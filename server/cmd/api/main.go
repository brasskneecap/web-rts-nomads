package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webrts/server/internal/game"
	httpserver "webrts/server/internal/http"
	"webrts/server/internal/ws"
)

func main() {
	corsOrigin := os.Getenv("CORS_ALLOWED_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "http://localhost:5173"
	}

	manager := game.NewMatchManager()
	lobbyManager := game.NewLobbyManager()
	hub := ws.NewHub(manager, lobbyManager)
	router := httpserver.NewRouter(hub, corsOrigin)

	addr := ":8080"
	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Shut down cleanly on SIGINT / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

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
