// §12 — Steam Networking Sockets transport bridge (Rust side).
//
// Lifecycle:
//   1. `SteamNet::start(client, writer)` spawns a single worker thread that
//      owns the ListenSocket and every NetConnection. Why a single thread:
//      steamworks-rs's ListenSocket / NetConnection are `Send` but not `Sync`
//      (their internal mpsc Receivers move handles across threads but cannot
//      be referenced from two threads at once). Centralising them on one
//      worker means we never have to take a global mutex on the steam handle
//      itself — the worker is the sole owner.
//   2. IPC request handlers (ipc.rs) send commands over a `mpsc::Sender` and
//      block on a `oneshot::Receiver` for the reply. Each command is short
//      (microseconds; Steam queues internally) so the IPC dispatcher's
//      synchronous-handler model is preserved.
//   3. The same worker loop polls `ListenSocket::try_receive_event` and each
//      open `NetConnection::receive_messages_with`, pushing IPC notifications
//      for new peers, peer bytes, and peer disconnects.
//
// Wire format (notifications on the shared IPC channel, per §12 header note):
//   →  { "event": "new_peer_transport", "params": { "peerId": "<u64>", "steamId64": "<u64>", "role": "host"|"joiner" } }
//   →  { "event": "peer_message",       "params": { "peerId": "<u64>", "payload": "<base64>" } }
//   →  { "event": "peer_disconnected",  "params": { "peerId": "<u64>", "reason": "<i32>" } }
//
// Request methods handled by ipc.rs and forwarded to this module:
//   open_listener       { "virtualPort": <i32> }
//   connect_to          { "steamId64": "<u64>", "virtualPort": <i32> }
//   send_peer_message   { "peerId": "<u64>", "payload": "<base64>" }
//   close_peer          { "peerId": "<u64>" }
//
// §12.0 invariant: every SendMessageToConnection call SHALL use the
// `SendFlags::RELIABLE` flag — reliable + ordered. The marker test
// `reliable_send_flag_is_the_only_one_used` greps this file for the literal
// `SendFlags::RELIABLE` and asserts no other `SendFlags::` constant appears
// in the send path.

#![cfg(feature = "steam")]

use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::mpsc::{self, Receiver, RecvTimeoutError, Sender};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use base64::Engine as _;
use log::{info, warn};
use serde_json::json;
use steamworks::networking_sockets::NetConnection;
use steamworks::networking_types::{
    AppNetConnectionEnd, ListenSocketEvent, NetConnectionEnd, NetworkingIdentity, SendFlags,
};
use steamworks::{Client, SteamId};

use crate::ipc::{push_notification, SharedWriter};

/// Default virtual port for nomads game traffic. Picked at random in the
/// small-int range (<1000) recommended by the Steamworks docs; values that
/// already have meaning to common games are avoided. Both host and joiner
/// agree on this constant — the lobby flow doesn't currently transmit it.
pub const NOMADS_VIRTUAL_PORT: i32 = 27;

/// Public handle to the Steam Sockets worker. Cheap to clone — internally
/// only the Sender side of the command channel is shared.
#[derive(Clone)]
pub struct SteamNet {
    cmd_tx: Sender<Command>,
    quit: Arc<AtomicBool>,
}

impl SteamNet {
    /// Start the worker thread. The thread runs until `shutdown` is called or
    /// the SteamNet handle is dropped along with all clones.
    pub fn start(client: Client, writer: SharedWriter) -> Self {
        let (cmd_tx, cmd_rx) = mpsc::channel::<Command>();
        let quit = Arc::new(AtomicBool::new(false));
        let quit_for_worker = quit.clone();
        thread::Builder::new()
            .name("nomads-steam-net".into())
            .spawn(move || Worker::new(client, writer, cmd_rx, quit_for_worker).run())
            .expect("spawn nomads-steam-net thread");
        SteamNet { cmd_tx, quit }
    }

    /// Open the host-side listen socket on the given virtual port. Idempotent
    /// — calling twice with the same port is a no-op. The same nomads
    /// process can both listen (as a host) and connect (as a joiner) in
    /// principle, but our MP model is single-role per session.
    pub fn open_listener(&self, virtual_port: i32) -> Result<(), String> {
        self.dispatch(|reply| Command::OpenListener { virtual_port, reply })
    }

    /// Connect to a remote Steam user. Returns the opaque peer-id the Go
    /// server uses to route subsequent send/close requests for this peer.
    pub fn connect_to(&self, steam_id_64: u64, virtual_port: i32) -> Result<u64, String> {
        self.dispatch(|reply| Command::Connect {
            steam_id_64,
            virtual_port,
            reply,
        })
    }

    /// Send `payload` to the peer with id `peer_id`. When `reliable` is true
    /// the message goes out as RELIABLE+ordered; when false it goes out as
    /// UNRELIABLE_NO_DELAY (drop-on-saturation, no Nagle). See D22 for the
    /// per-message-type policy — snapshots use unreliable, everything else
    /// uses reliable.
    pub fn send(&self, peer_id: u64, payload: Vec<u8>, reliable: bool) -> Result<(), String> {
        self.dispatch(|reply| Command::Send {
            peer_id,
            payload,
            reliable,
            reply,
        })
    }

    /// Close the connection associated with `peer_id`. Returns true when the
    /// peer existed and was closed, false when no such peer was known.
    pub fn close(&self, peer_id: u64) -> Result<bool, String> {
        self.dispatch(|reply| Command::Close { peer_id, reply })
    }

    /// Stop the worker thread. Idempotent.
    #[allow(dead_code)]
    pub fn shutdown(&self) {
        self.quit.store(true, Ordering::SeqCst);
    }

    fn dispatch<T: Send + 'static, F>(&self, build: F) -> Result<T, String>
    where
        F: FnOnce(Sender<Result<T, String>>) -> Command,
    {
        let (tx, rx) = mpsc::channel::<Result<T, String>>();
        let cmd = build(tx);
        self.cmd_tx
            .send(cmd)
            .map_err(|_| "steam_net worker is dead".to_string())?;
        rx.recv()
            .map_err(|_| "steam_net worker dropped the reply".to_string())?
    }
}

// ----- Worker ---------------------------------------------------------------

enum Command {
    OpenListener {
        virtual_port: i32,
        reply: Sender<Result<(), String>>,
    },
    Connect {
        steam_id_64: u64,
        virtual_port: i32,
        reply: Sender<Result<u64, String>>,
    },
    Send {
        peer_id: u64,
        payload: Vec<u8>,
        // D22: true → SendFlags::RELIABLE; false → SendFlags::UNRELIABLE_NO_DELAY
        reliable: bool,
        reply: Sender<Result<(), String>>,
    },
    Close {
        peer_id: u64,
        reply: Sender<Result<bool, String>>,
    },
}

/// Peer state held by the worker. `role` is recorded for future reconnect
/// handling and diagnostic logging; the inbound-traffic path doesn't read it
/// (the role is encoded in the `new_peer_transport` notification at the
/// moment of emission).
struct PeerEntry {
    conn: NetConnection,
    steam_id_64: u64,
    #[allow(dead_code)]
    role: PeerRole,
}

#[derive(Clone, Copy)]
enum PeerRole {
    Host,
    Joiner,
}

impl PeerRole {
    fn as_str(self) -> &'static str {
        match self {
            PeerRole::Host => "host",
            PeerRole::Joiner => "joiner",
        }
    }
}

struct Worker {
    client: Client,
    writer: SharedWriter,
    cmd_rx: Receiver<Command>,
    quit: Arc<AtomicBool>,
    listen_socket: Option<steamworks::networking_sockets::ListenSocket>,
    listen_port: Option<i32>,
    /// peer_id → entry. peer_id is a monotonically increasing u64 assigned
    /// when a connection becomes Connected (host side) or when ConnectP2P
    /// returns (joiner side). Opaque to Go; never reused.
    peers: HashMap<u64, PeerEntry>,
    next_peer_id: AtomicU64,
    /// Pending connecting-side connections: connection -> peer_id assigned
    /// at ConnectP2P time so the joiner-side IPC reply can include it.
    /// On Connected we drain into `peers` and emit new_peer_transport.
    pending_outbound: HashMap<u64, NetConnection>,
    /// Throughput counters for the periodic stats log line. Reset every
    /// STATS_INTERVAL. Helps diagnose backpressure (high send count but
    /// game-side feels slow → bandwidth/relay issue), starvation (low
    /// counts despite active gameplay → worker loop is starved), etc.
    stats_sent_msgs: u64,
    stats_sent_bytes: u64,
    stats_recv_msgs: u64,
    stats_recv_bytes: u64,
    stats_last_log: std::time::Instant,
}

impl Worker {
    fn new(
        client: Client,
        writer: SharedWriter,
        cmd_rx: Receiver<Command>,
        quit: Arc<AtomicBool>,
    ) -> Self {
        Worker {
            client,
            writer,
            cmd_rx,
            quit,
            listen_socket: None,
            listen_port: None,
            peers: HashMap::new(),
            next_peer_id: AtomicU64::new(1),
            pending_outbound: HashMap::new(),
            stats_sent_msgs: 0,
            stats_sent_bytes: 0,
            stats_recv_msgs: 0,
            stats_recv_bytes: 0,
            stats_last_log: std::time::Instant::now(),
        }
    }

    fn run(mut self) {
        // 100ms is the steamworks callback-pump cadence; matching it keeps
        // event-to-notification latency close to the SDK's own granularity
        // without busy-waiting.
        const POLL_INTERVAL: Duration = Duration::from_millis(20);
        loop {
            if self.quit.load(Ordering::SeqCst) {
                break;
            }
            // Drain pending commands without blocking past the poll interval.
            match self.cmd_rx.recv_timeout(POLL_INTERVAL) {
                Ok(cmd) => self.handle_command(cmd),
                Err(RecvTimeoutError::Timeout) => {}
                Err(RecvTimeoutError::Disconnected) => break,
            }
            while let Ok(cmd) = self.cmd_rx.try_recv() {
                self.handle_command(cmd);
            }
            self.pump_listener_events();
            self.pump_connection_messages();
            self.maybe_log_stats();
        }
        info!("steam_net: worker exiting");
    }

    /// Logs sent/recv throughput once every STATS_INTERVAL. Logs only
    /// when peers are connected (avoids cluttering the shell log during
    /// idle pre-match time) but always emits a line in that case so we
    /// can see "0 sent, 0 recv" as a problem signal too. Writes via
    /// `current_shell_log()` because env_logger's stderr output is
    /// detached in windowed release builds.
    fn maybe_log_stats(&mut self) {
        const STATS_INTERVAL: Duration = Duration::from_secs(5);
        if self.stats_last_log.elapsed() < STATS_INTERVAL {
            return;
        }
        let active_peers = self.peers.len();
        let elapsed = self.stats_last_log.elapsed();
        if active_peers > 0 {
            if let Some(sl) = crate::logs::current_shell_log() {
                sl.write_line(
                    "INFO",
                    &format!(
                        "steam_net stats over {:?}: peers={} sent={} msgs ({} bytes) recv={} msgs ({} bytes)",
                        elapsed,
                        active_peers,
                        self.stats_sent_msgs,
                        self.stats_sent_bytes,
                        self.stats_recv_msgs,
                        self.stats_recv_bytes,
                    ),
                );
            }
            info!(
                "steam_net stats over {:?}: peers={} sent={} ({} bytes) recv={} ({} bytes)",
                elapsed,
                active_peers,
                self.stats_sent_msgs,
                self.stats_sent_bytes,
                self.stats_recv_msgs,
                self.stats_recv_bytes,
            );
        }
        self.stats_sent_msgs = 0;
        self.stats_sent_bytes = 0;
        self.stats_recv_msgs = 0;
        self.stats_recv_bytes = 0;
        self.stats_last_log = std::time::Instant::now();
    }

    fn handle_command(&mut self, cmd: Command) {
        match cmd {
            Command::OpenListener { virtual_port, reply } => {
                let result = self.open_listener_locked(virtual_port);
                let _ = reply.send(result);
            }
            Command::Connect {
                steam_id_64,
                virtual_port,
                reply,
            } => {
                let result = self.connect_locked(steam_id_64, virtual_port);
                let _ = reply.send(result);
            }
            Command::Send {
                peer_id,
                payload,
                reliable,
                reply,
            } => {
                let result = self.send_locked(peer_id, &payload, reliable);
                let _ = reply.send(result);
            }
            Command::Close { peer_id, reply } => {
                let result = self.close_locked(peer_id);
                let _ = reply.send(Ok(result));
            }
        }
    }

    fn open_listener_locked(&mut self, virtual_port: i32) -> Result<(), String> {
        if let Some(existing) = self.listen_port {
            if existing == virtual_port {
                return Ok(());
            }
            return Err(format!(
                "listener already open on virtual port {existing}; close it before reopening"
            ));
        }
        // Per the steamworks docs, P2P listen requires the relay network so
        // the matchmaking lobby's friends can reach us through Steam's relay
        // infrastructure. init_relay_network_access is idempotent.
        self.client.networking_utils().init_relay_network_access();
        let sockets = self.client.networking_sockets();
        let socket = sockets
            .create_listen_socket_p2p(virtual_port, std::iter::empty())
            .map_err(|e| format!("create_listen_socket_p2p: {e}"))?;
        self.listen_socket = Some(socket);
        self.listen_port = Some(virtual_port);
        info!("steam_net: listening on virtual port {virtual_port}");
        Ok(())
    }

    fn connect_locked(
        &mut self,
        steam_id_64: u64,
        virtual_port: i32,
    ) -> Result<u64, String> {
        self.client.networking_utils().init_relay_network_access();
        let identity = NetworkingIdentity::new_steam_id(SteamId::from_raw(steam_id_64));
        let conn = self
            .client
            .networking_sockets()
            .connect_p2p(identity, virtual_port, std::iter::empty())
            .map_err(|e| format!("connect_p2p({steam_id_64}): {e}"))?;
        let peer_id = self.next_peer_id.fetch_add(1, Ordering::Relaxed);
        self.pending_outbound.insert(peer_id, conn);
        info!(
            "steam_net: joiner connect_p2p steamId={steam_id_64} → peerId={peer_id} (pending)"
        );
        Ok(peer_id)
    }

    fn send_locked(
        &mut self,
        peer_id: u64,
        payload: &[u8],
        reliable: bool,
    ) -> Result<(), String> {
        // §12.0 / D22: every send_message call uses one of exactly two flag
        // constants — SendFlags::RELIABLE (reliable + ordered) for commands
        // and SendFlags::UNRELIABLE_NO_DELAY (drop-on-link-full, no Nagle)
        // for snapshots. The marker test below grep-asserts only these two
        // constants appear in this file.
        //
        // For reliable sends we also flush messages on the connection after
        // each send. The default Steam Sockets behaviour batches small writes
        // for up to ~200ms (Nagle), which compounds across multiple hops to
        // multi-second perceived latency for RTS inputs. Flushing forces the
        // SDK to dispatch what's queued right now. UnreliableNoDelay already
        // skips the Nagle timer at the SDK layer, so a separate flush is
        // unnecessary (but harmless if called) — we omit it on the unreliable
        // path to save a syscall.
        let flags = if reliable {
            SendFlags::RELIABLE
        } else {
            SendFlags::UNRELIABLE_NO_DELAY
        };
        let payload_len = payload.len() as u64;
        if let Some(entry) = self.peers.get_mut(&peer_id) {
            let result = entry
                .conn
                .send_message(payload, flags)
                .map(|_| ())
                .map_err(|e| format!("send_message({peer_id}): {e:?}"));
            if result.is_ok() {
                if reliable {
                    let _ = entry.conn.flush_messages();
                }
                self.stats_sent_msgs += 1;
                self.stats_sent_bytes += payload_len;
            }
            return result;
        }
        if let Some(conn) = self.pending_outbound.get_mut(&peer_id) {
            // Sending before Connected is permitted by Steamworks; messages
            // are queued and flushed once the connection completes.
            let result = conn
                .send_message(payload, flags)
                .map(|_| ())
                .map_err(|e| format!("send_message({peer_id} pending): {e:?}"));
            if result.is_ok() {
                if reliable {
                    let _ = conn.flush_messages();
                }
                self.stats_sent_msgs += 1;
                self.stats_sent_bytes += payload_len;
            }
            return result;
        }
        Err(format!("unknown peer_id {peer_id}"))
    }

    fn close_locked(&mut self, peer_id: u64) -> bool {
        if let Some(entry) = self.peers.remove(&peer_id) {
            entry
                .conn
                .close(NetConnectionEnd::App(AppNetConnectionEnd::generic_normal()), Some("close_peer ipc"), false);
            return true;
        }
        if let Some(conn) = self.pending_outbound.remove(&peer_id) {
            conn.close(NetConnectionEnd::App(AppNetConnectionEnd::generic_normal()), Some("close_peer ipc"), false);
            return true;
        }
        false
    }

    fn pump_listener_events(&mut self) {
        let Some(socket) = self.listen_socket.as_ref() else {
            return;
        };
        // Drain all pending events in one tick; the channel is unbounded so
        // unprocessed events otherwise accumulate forever.
        while let Some(event) = socket.try_receive_event() {
            match event {
                ListenSocketEvent::Connecting(req) => {
                    // Spec: respond promptly so the client doesn't time out.
                    // We accept everyone — friend authorisation lives at the
                    // Steam lobby layer (we only listen on the lobby's
                    // virtual port and only friends in the lobby learn our
                    // SteamID via lobby metadata).
                    if let Err(e) = req.accept() {
                        warn!("steam_net: accept incoming connection failed: {e:?}");
                    }
                }
                ListenSocketEvent::Connected(connected) => {
                    let steam_id_64 = connected
                        .remote()
                        .steam_id()
                        .map(|s| s.raw())
                        .unwrap_or(0);
                    let conn = connected.take_connection();
                    let peer_id = self.next_peer_id.fetch_add(1, Ordering::Relaxed);
                    self.peers.insert(
                        peer_id,
                        PeerEntry {
                            conn,
                            steam_id_64,
                            role: PeerRole::Host,
                        },
                    );
                    info!(
                        "steam_net: peer connected steamId={steam_id_64} → peerId={peer_id}"
                    );
                    if let Some(sl) = crate::logs::current_shell_log() {
                        sl.write_line(
                            "INFO",
                            &format!(
                                "steam_net: peer connected (host-side) steamId={steam_id_64} peerId={peer_id}"
                            ),
                        );
                    }
                    push_notification(
                        &self.writer,
                        "new_peer_transport",
                        json!({
                            "peerId": peer_id.to_string(),
                            "steamId64": steam_id_64.to_string(),
                            "role": PeerRole::Host.as_str(),
                        }),
                    );
                }
                ListenSocketEvent::Disconnected(disc) => {
                    let steam_id_64 = disc.remote().steam_id().map(|s| s.raw()).unwrap_or(0);
                    let reason: i32 = disc.end_reason().into();
                    // Find the peer_id matching this steam_id (host-side
                    // disconnect events lose the connection handle by this
                    // point; matching by steam_id is the documented escape).
                    let dropped: Vec<u64> = self
                        .peers
                        .iter()
                        .filter(|(_, e)| e.steam_id_64 == steam_id_64)
                        .map(|(k, _)| *k)
                        .collect();
                    for peer_id in dropped {
                        self.peers.remove(&peer_id);
                        push_notification(
                            &self.writer,
                            "peer_disconnected",
                            json!({
                                "peerId": peer_id.to_string(),
                                "reason": reason,
                            }),
                        );
                    }
                }
            }
        }
    }

    fn pump_connection_messages(&mut self) {
        // For pending-outbound (joiner) connections, also poll connection
        // events to detect transition into Connected so we can emit
        // new_peer_transport on the joiner side too.
        let pending_keys: Vec<u64> = self.pending_outbound.keys().copied().collect();
        for peer_id in pending_keys {
            let connected = {
                let Some(conn) = self.pending_outbound.get_mut(&peer_id) else {
                    continue;
                };
                use steamworks::networking_types::NetworkingConnectionState;
                let info = match conn.info() {
                    Ok(i) => i,
                    Err(_) => continue,
                };
                match info.state() {
                    Ok(NetworkingConnectionState::Connected) => Some(
                        info.identity_remote()
                            .and_then(|id| id.steam_id())
                            .map(|s| s.raw())
                            .unwrap_or(0),
                    ),
                    Ok(NetworkingConnectionState::ClosedByPeer)
                    | Ok(NetworkingConnectionState::ProblemDetectedLocally) => {
                        // Connection failed before reaching Connected; emit
                        // disconnect so the Go side can clean up.
                        Some(0u64)
                    }
                    _ => None,
                }
            };
            if let Some(steam_id_64) = connected {
                // Move the connection from pending_outbound into peers.
                let conn = self.pending_outbound.remove(&peer_id).unwrap();
                // Determine if this is the connected case (steam_id_64 != 0)
                // vs the closed case (steam_id_64 == 0). For closed we emit
                // disconnect and drop the connection.
                if steam_id_64 == 0 {
                    push_notification(
                        &self.writer,
                        "peer_disconnected",
                        json!({
                            "peerId": peer_id.to_string(),
                            "reason": -1,
                        }),
                    );
                    conn.close(NetConnectionEnd::App(AppNetConnectionEnd::generic_normal()), Some("pre-connect close"), false);
                    continue;
                }
                self.peers.insert(
                    peer_id,
                    PeerEntry {
                        conn,
                        steam_id_64,
                        role: PeerRole::Joiner,
                    },
                );
                info!(
                    "steam_net: outbound connect completed steamId={steam_id_64} peerId={peer_id}"
                );
                if let Some(sl) = crate::logs::current_shell_log() {
                    sl.write_line(
                        "INFO",
                        &format!(
                            "steam_net: outbound connect completed (joiner-side) steamId={steam_id_64} peerId={peer_id}"
                        ),
                    );
                }
                push_notification(
                    &self.writer,
                    "new_peer_transport",
                    json!({
                        "peerId": peer_id.to_string(),
                        "steamId64": steam_id_64.to_string(),
                        "role": PeerRole::Joiner.as_str(),
                    }),
                );
            }
        }

        // Pump per-connection inbound messages on every active peer.
        let peer_keys: Vec<u64> = self.peers.keys().copied().collect();
        for peer_id in peer_keys {
            let mut to_emit: Vec<Vec<u8>> = Vec::new();
            let mut closed = false;
            if let Some(entry) = self.peers.get_mut(&peer_id) {
                entry.conn.receive_messages_with(|msg| {
                    to_emit.push(msg.data().to_vec());
                });
                // Check whether the connection is still in a usable state.
                if let Ok(info) = entry.conn.info() {
                    use steamworks::networking_types::NetworkingConnectionState;
                    if !matches!(
                        info.state(),
                        Ok(NetworkingConnectionState::Connected)
                            | Ok(NetworkingConnectionState::FindingRoute)
                    ) {
                        closed = true;
                    }
                }
            }
            for payload in to_emit {
                self.stats_recv_msgs += 1;
                self.stats_recv_bytes += payload.len() as u64;
                let b64 = base64::engine::general_purpose::STANDARD.encode(&payload);
                push_notification(
                    &self.writer,
                    "peer_message",
                    json!({
                        "peerId": peer_id.to_string(),
                        "payload": b64,
                    }),
                );
            }
            if closed {
                if let Some(entry) = self.peers.remove(&peer_id) {
                    info!("steam_net: peer {peer_id} (steamId={}) closed", entry.steam_id_64);
                    entry
                        .conn
                        .close(NetConnectionEnd::App(AppNetConnectionEnd::generic_normal()), Some("state degraded"), false);
                    push_notification(
                        &self.writer,
                        "peer_disconnected",
                        json!({
                            "peerId": peer_id.to_string(),
                            "reason": 0,
                        }),
                    );
                }
            }
        }
    }
}

// ----- Tests ---------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    /// §12.0 / D22 marker: this file must use `SendFlags::RELIABLE` AND
    /// `SendFlags::UNRELIABLE_NO_DELAY` and ONLY those two send flags in the
    /// send_message path. The reliable flag is used for commands and every
    /// non-snapshot message; the unreliable+no-delay flag is used for
    /// per-tick snapshot broadcasts (D22 amended after a 5s observed lag in
    /// two-machine playtests traced to reliable-queue compounding latency
    /// on bandwidth-saturated Steam relays). Reads its own source via
    /// include_str! so the grep is portable (works in CI without a
    /// `grep` binary). If the assertion fires, audit every `SendFlags::` use
    /// and confirm the change is intentional.
    #[test]
    fn send_flags_are_restricted_to_the_two_allowed_constants() {
        let src = include_str!("steam_net.rs");
        let mut occurrences: Vec<&str> = Vec::new();
        // Find every SendFlags-double-colon use.
        for (idx, _) in src.match_indices("SendFlags::") {
            let after = &src[idx + "SendFlags::".len()..];
            let end = after
                .find(|c: char| !c.is_ascii_alphanumeric() && c != '_')
                .unwrap_or(after.len());
            let ident = &after[..end];
            if !ident.is_empty() {
                occurrences.push(ident);
            }
        }
        // Allowed set per D22.
        let allowed = ["RELIABLE", "UNRELIABLE_NO_DELAY"];
        let unexpected: Vec<&&str> = occurrences
            .iter()
            .filter(|i| !allowed.contains(*i))
            .collect();
        assert!(
            unexpected.is_empty(),
            "steam_net.rs may only use SendFlags::{{RELIABLE, UNRELIABLE_NO_DELAY}} per D22; unexpected idents: {unexpected:?}"
        );
        // Both constants MUST appear in the file — RELIABLE for commands,
        // UNRELIABLE_NO_DELAY for snapshots. Missing either means a refactor
        // dropped one of the two paths and invalidated the per-message-type
        // policy.
        assert!(
            occurrences.contains(&"RELIABLE"),
            "steam_net.rs no longer references SendFlags::RELIABLE — every non-snapshot send needs the reliable path"
        );
        assert!(
            occurrences.contains(&"UNRELIABLE_NO_DELAY"),
            "steam_net.rs no longer references SendFlags::UNRELIABLE_NO_DELAY — snapshot broadcasts need the drop-on-saturation path (D22)"
        );
    }

    #[test]
    fn peer_role_strings_are_stable() {
        assert_eq!(PeerRole::Host.as_str(), "host");
        assert_eq!(PeerRole::Joiner.as_str(), "joiner");
    }
}
