package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webrts/server/internal/game"
	httpserver "webrts/server/internal/http"
	"webrts/server/internal/profile"
	"webrts/server/internal/steam"
	"webrts/server/internal/ws"
)

// version is overridden at build time via `-ldflags "-X main.version=..."`.
// The Tauri shell's ready-line parser reads it to detect SPA/server build
// mismatches; the SPA's first WS hello includes the same value.
var version = "dev"

func main() {
	var portFlag string
	flag.StringVar(&portFlag, "port", "", "port to bind (0 = kernel-assigned). Overrides WEBRTS_PORT.")
	flag.Parse()

	port := portFlag
	if port == "" {
		port = os.Getenv("WEBRTS_PORT")
	}
	if port == "" {
		port = "8080"
	}

	corsOrigin := os.Getenv("CORS_ALLOWED_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "http://localhost:5173"
	}

	// Overlay any editor-saved / joiner-received maps from the writable map dir
	// on top of the embedded catalog so edits survive a restart. Without this,
	// an edit only lives in the non-persistent in-memory overlay of the process
	// that made it, and the server reverts to the embedded map on the next
	// launch. Best-effort: never fatal.
	game.LoadPersistedMapsIntoOverlay()

	manager := game.NewMatchManager()
	lobbyManager := game.NewLobbyManager()
	hub := ws.NewHub(manager, lobbyManager)
	hub.SetVersion(version) // §17.1 build-mismatch handshake
	profileManager := profile.NewManager("")
	manager.SetDominionPointCommitter(profileManager)
	manager.SetRecipeRecorder(profileManager)

	spaHandler, err := newSPAHandler()
	if err != nil {
		log.Fatalf("embedded SPA init: %v", err)
	}

	// SteamBridge selection: when the Tauri shell launches the server it sets
	// NOMADS_IPC_PATH to the shell-owned named-pipe / Unix-socket path. If
	// set, we connect to the IPCBridge so the Go server can reach Steam
	// (player identity, achievements, lobby create/join). If unset we fall
	// back to NoopBridge (browser dev loop, server run bare, etc.).
	var bridge steam.SteamBridge = steam.NewNoopBridge()
	if path := os.Getenv("NOMADS_IPC_PATH"); path != "" {
		if b, err := steam.NewIPCBridge(path); err != nil {
			log.Printf("steam: IPCBridge dial failed (%v) — falling back to NoopBridge", err)
		} else {
			log.Printf("steam: IPCBridge connected to %s", path)
			bridge = b
		}
	}

	// §12 / §14 — Steam Networking Sockets transport bridge wiring. When
	// the bridge is the live IPCBridge:
	//
	//   - With NOMADS_SELFTEST set, the smoke-test handler replaces the
	//     default and fires OpenListener/ConnectTo immediately. Used for
	//     two-machine CLI verification before §14 SPA wiring landed.
	//   - Without NOMADS_SELFTEST, lobby state drives the wiring: the
	//     shell pushes `lobby_hosted` after CreateLobby and `lobby_joined`
	//     after JoinLobby; the handlers (see steam_lobby.go) install the
	//     right peer handler and fire OpenListener/ConnectTo at that point.
	//     Until one of those events arrives the bridge holds no peer
	//     handler and any premature new_peer_transport notification is
	//     dropped by the bridge with a log.
	if ipcBridge, ok := bridge.(*steam.IPCBridge); ok {
		if selftest := os.Getenv("NOMADS_SELFTEST"); selftest != "" {
			if err := installSteamNetSelftest(ipcBridge, selftest); err != nil {
				log.Printf("[SELFTEST] install failed: %v", err)
			}
		} else {
			wireSteamLobbyHandlers(ipcBridge, hub)
		}
	}
	_ = bridge // achievements §16, lobby state sync §14.5/§14.6 land later

	router := httpserver.NewRouter(hub, corsOrigin, profileManager, spaHandler)

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen on port %q: %v", port, err)
	}
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		log.Fatalf("unexpected listener address type %T", listener.Addr())
	}
	actualPort := tcpAddr.Port

	server := &http.Server{
		Handler: router,
	}

	// Shut down cleanly on SIGINT / SIGTERM or on stdin EOF (the latter is how
	// the Tauri shell tells the Go child "parent is closing, please exit").
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// The stdin-EOF shutdown signal (design D7) is ONLY meaningful when the
	// Tauri shell is our parent: it pipes our stdin, holds the write end for
	// the app's lifetime, and drops it on exit. The shell sets
	// NOMADS_SHELL_MANAGED=1 to opt in. When run standalone (start.bat → air,
	// `go run`, bare binary) stdin is non-interactive and reads EOF
	// immediately, so an unconditional watchStdin would kill the server one
	// second after it binds. Gate on the explicit shell signal.
	if os.Getenv("NOMADS_SHELL_MANAGED") != "" {
		go watchStdin(stop)
	}

	go func() {
		log.Printf("server listening on %s", listener.Addr())
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// NOMADS_READY is the single line the Tauri shell scrapes from stdout to
	// learn (a) the actual port (when port=0) and (b) the server's compiled
	// version for SPA/server build-mismatch detection. It is printed exactly
	// once, after the listener is bound and Serve is dispatched.
	fmt.Printf("NOMADS_READY url=http://127.0.0.1:%d version=%s\n", actualPort, version)

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}

	hub.Close()
	log.Println("server stopped")
}

// watchStdin reads and discards stdin until EOF or another read error, then
// triggers shutdown. The Tauri shell closes the Go child's stdin to request a
// graceful exit (design D7: "pipes already give us a cross-platform 'parent
// died, child should die' semantic with no protocol overhead").
func watchStdin(stop context.CancelFunc) {
	buf := make([]byte, 256)
	for {
		_, err := os.Stdin.Read(buf)
		if err != nil {
			if err == io.EOF {
				log.Println("stdin closed; initiating shutdown")
			} else {
				log.Printf("stdin read error: %v; initiating shutdown", err)
			}
			stop()
			return
		}
	}
}
