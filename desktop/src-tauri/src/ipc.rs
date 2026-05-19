// §8.3 — §8.5: Rust shell ↔ Go server IPC channel for Steam features.
//
// Wire format (per design D8 + §4.4): newline-delimited JSON request/response.
// Each request has a stable `id` the response echoes back so the Go side can
// match async responses on a single shared socket.
//
//   →  {"id":"req-1","method":"local_player"}
//   ←  {"id":"req-1","result":{"steamId64":"7656...","personaName":"Acegamer"}}
//   ←  {"id":"req-1","error":{"code":"steam_unavailable","message":"..."}}
//
// One Go child connects per session; we accept once and run a per-connection
// dispatcher. When the connection closes we just stop the dispatcher — the
// Go process is exiting anyway.

#![cfg(feature = "steam")]

use std::io::{BufRead, BufReader, Write};
use std::sync::{Arc, Mutex, OnceLock};
use std::thread;

use interprocess::local_socket::{
    prelude::*, GenericFilePath, GenericNamespaced, ListenerOptions, Name,
};
use log::{info, warn};
use serde::{Deserialize, Serialize};

use crate::steam;
use crate::steam_net;

/// Returns the (bind_name, env_path) pair for one of the two IPC pipes.
/// `direction` is "c2s" (client→shell — Go writes, Rust reads) or
/// "s2c" (shell→client — Rust writes, Go reads).
///
/// Two pipes are required on Windows because `interprocess`'s
/// `ConcurrencyDetector` panics when read and write happen concurrently on
/// the same underlying named pipe (a hard constraint of Windows named
/// pipes' sync I/O API). By splitting traffic across two unidirectional
/// pipes the detector never fires, and we avoid the deadlock-prone design
/// of trying to share one duplex pipe between a blocking reader and any
/// writer.
fn socket_paths_for(direction: &str, runtime_dir: &std::path::Path) -> (String, String) {
    if cfg!(windows) {
        let name = format!("nomads-shell-{direction}-{}", std::process::id());
        let env_path = format!(r"\\.\pipe\{name}");
        (name, env_path)
    } else {
        let p = runtime_dir
            .join(format!("shell-{direction}.sock"))
            .to_string_lossy()
            .into_owned();
        (p.clone(), p)
    }
}

/// Resolves the socket path string into the platform-appropriate
/// interprocess Name. On Windows we treat it as a namespaced pipe; on Unix
/// it's a filesystem path.
fn name_from_path(path: &str) -> Result<Name<'_>, std::io::Error> {
    if cfg!(windows) {
        path.to_ns_name::<GenericNamespaced>()
    } else {
        path.to_fs_name::<GenericFilePath>()
    }
}

/// Spawn the IPC listener on a background thread. Returns the path string
/// that should be passed to the Go child via NOMADS_IPC_PATH. The listener
/// runs until process exit (no explicit shutdown — Go child closes when
/// stdin is closed, which closes its socket end and our dispatcher exits).
///
/// When `bridge` is present, the dispatcher also spins up a Steam Networking
/// Sockets worker (steam_net::SteamNet) that handles peer-transport requests
/// (open_listener, connect_to, send_peer_message, close_peer) and emits
/// peer notifications (new_peer_transport, peer_message, peer_disconnected)
/// through the same SharedWriter. See §12 in tasks.md for the wire format.
pub fn start(
    runtime_dir: &std::path::Path,
    bridge: Option<Arc<steam::Bridge>>,
) -> Result<String, std::io::Error> {
    let (c2s_bind, c2s_env) = socket_paths_for("c2s", runtime_dir);
    let (s2c_bind, s2c_env) = socket_paths_for("s2c", runtime_dir);

    // Unix: clean up any stale socket files from a previous unclean exit.
    if !cfg!(windows) {
        let _ = std::fs::remove_file(&c2s_bind);
        let _ = std::fs::remove_file(&s2c_bind);
    }

    let c2s_name = name_from_path(&c2s_bind)?;
    let s2c_name = name_from_path(&s2c_bind)?;
    let c2s_listener = ListenerOptions::new().name(c2s_name).create_sync()?;
    let s2c_listener = ListenerOptions::new().name(s2c_name).create_sync()?;
    info!("ipc: listening c2s={c2s_env} s2c={s2c_env}");

    // Combined env var: Go side parses `c2s=<path>|s2c=<path>` and dials
    // each pipe to the right end. Pipe character chosen because Windows
    // pipe paths never contain it.
    let env_value = format!("c2s={c2s_env}|s2c={s2c_env}");

    thread::spawn(move || {
        // Two accepts: Go dials both ends in parallel; the order doesn't
        // matter for our purposes. Each accept blocks independently.
        let c2s_conn = match c2s_listener.accept() {
            Ok(c) => c,
            Err(e) => {
                warn!("ipc: c2s accept failed: {e}");
                return;
            }
        };
        let s2c_conn = match s2c_listener.accept() {
            Ok(c) => c,
            Err(e) => {
                warn!("ipc: s2c accept failed: {e}");
                return;
            }
        };
        info!("ipc: Go child connected on both pipes");
        // Each pipe is used in one direction only — that's the whole
        // point. c2s for reads, s2c for writes. We do NOT call split()
        // because interprocess's ConcurrencyDetector would still panic
        // if anyone tried to use the other direction.
        run_dispatcher(c2s_conn, s2c_conn, bridge);
        info!("ipc: dispatcher exited (connection closed)");
    });

    Ok(env_value)
}

/// Process-wide slot for the IPC writer, set once the Go child connects.
/// Tauri commands (which run on tokio threads with no direct access to the
/// dispatcher loop) read this to push notifications to Go after handling
/// an SPA-initiated action — e.g. the SPA's `create_lobby` Tauri command
/// triggers a `lobby_hosted` notification so the Go server knows to open
/// the steam-net listener.
///
/// Holds None until the dispatcher accepts a connection. None means "no Go
/// child is connected yet"; callers either drop the notification or queue
/// it themselves. In practice the SPA can't trigger any of these flows
/// until the SPA itself has loaded, which is after the Go child + IPC
/// channel are up.
static GO_WRITER_SLOT: OnceLock<Mutex<Option<SharedWriter>>> = OnceLock::new();

fn writer_slot() -> &'static Mutex<Option<SharedWriter>> {
    GO_WRITER_SLOT.get_or_init(|| Mutex::new(None))
}

/// Returns a clone of the current Go-bound IPC writer if the dispatcher
/// has accepted a Go child connection, or None otherwise. Cheap — only
/// clones an Arc + a Box pointer.
pub fn current_go_writer() -> Option<SharedWriter> {
    writer_slot().lock().unwrap().clone()
}

fn run_dispatcher<R, S>(recv: R, send: S, bridge: Option<Arc<steam::Bridge>>)
where
    R: std::io::Read + Send + 'static,
    S: std::io::Write + Send + 'static,
{
    // Send half goes behind a Mutex so concurrent writers (the dispatcher
    // itself, async Steam callbacks, steam_net peer notifications, and
    // Tauri commands that push notifications) serialise. The recv half
    // is moved into the reader and never touches this mutex — which
    // means a parked read no longer blocks any write.
    let writer: SharedWriter = Arc::new(Mutex::new(Box::new(send)));
    *writer_slot().lock().unwrap() = Some(writer.clone());
    let reader = BufReader::new(recv);

    // Start the Steam Networking Sockets worker now that the writer exists.
    // The worker pushes peer notifications through the same writer so they
    // multiplex with the existing response/notification frames. Construction
    // is cheap (one mpsc + one thread); skipping it when bridge is None
    // keeps non-Steam dev/test paths free of an idle steam thread.
    let net: Option<steam_net::SteamNet> = bridge
        .as_ref()
        .map(|b| steam_net::SteamNet::start(b.client.clone(), writer.clone()));

    for line in reader.lines() {
        let line = match line {
            Ok(l) => l,
            Err(e) => {
                warn!("ipc: read error: {e}");
                return;
            }
        };
        if line.is_empty() {
            continue;
        }
        let request: Request = match serde_json::from_str(&line) {
            Ok(r) => r,
            Err(e) => {
                warn!("ipc: invalid request JSON ({e}): {line}");
                continue;
            }
        };
        // Sync handlers return Some(Response); async handlers return None
        // after registering a Steam callback that writes via the shared
        // writer when the callback fires.
        if let Some(response) = handle(&request, bridge.as_ref(), net.as_ref(), &writer) {
            write_response(&writer, &response);
        }
    }
}

/// Boxed-trait writer so async handlers (which `move` the writer into
/// 'static closures) don't need to know the underlying transport type.
/// Exposed at crate visibility so steam_net::SteamNet can push peer
/// notifications through the same writer the IPC dispatcher uses.
pub(crate) type SharedWriter = Arc<Mutex<Box<dyn std::io::Write + Send>>>;

fn write_response(writer: &SharedWriter, response: &Response) {
    write_json_line(writer, response);
}

/// Push a one-way notification to Go. Used for Steam Sockets transport
/// events (new_peer_transport, peer_message, peer_disconnected).
pub fn push_notification(writer: &SharedWriter, event: &str, params: serde_json::Value) {
    let notif = Notification {
        event: event.to_string(),
        params,
    };
    write_json_line(writer, &notif);
}

fn write_json_line<T: serde::Serialize>(writer: &SharedWriter, value: &T) {
    let mut buf = match serde_json::to_vec(value) {
        Ok(v) => v,
        Err(e) => {
            warn!("ipc: serialise frame: {e}");
            return;
        }
    };
    buf.push(b'\n');
    if let Ok(mut w) = writer.lock() {
        if let Err(e) = w.write_all(&buf) {
            warn!("ipc: write error: {e}");
            return;
        }
        let _ = w.flush();
    }
}

// Note: an earlier version of this module wrapped a single duplex stream
// in Arc<Mutex<_>> for both reads and writes. That deadlocked: the
// reader thread held the mutex while parked in a blocking read, so any
// concurrent write (e.g. push_notification from a Tauri command) blocked
// indefinitely. The fix is to split the stream into independent
// RecvHalf + SendHalf via Stream::split() at accept time so the reader
// and writers never contend on a shared lock.

// ----- Wire types -----------------------------------------------------------
//
// Three frame kinds share the channel, distinguished by which fields are
// present:
//   Request       — has `id` + `method`. Go → Rust. Expects a Response.
//   Response      — has `id` + (`result` | `error`). Rust → Go. Matched by id.
//   Notification  — has `event` + `params`. Rust → Go, no response. Used for
//                   pushed events like new_peer_transport / peer_message.

#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct Request {
    pub id: String,
    pub method: String,
    #[serde(default)]
    pub params: serde_json::Value,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Response {
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<ErrorBody>,
}

/// One-way notification from Rust to Go. No id, no response expected.
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Notification {
    pub event: String,
    pub params: serde_json::Value,
}

#[derive(Debug, Serialize)]
pub struct ErrorBody {
    pub code: String,
    pub message: String,
}

impl Response {
    fn ok(id: &str, result: serde_json::Value) -> Self {
        Self {
            id: id.to_string(),
            result: Some(result),
            error: None,
        }
    }
    fn err(id: &str, code: &str, message: impl Into<String>) -> Self {
        Self {
            id: id.to_string(),
            result: None,
            error: Some(ErrorBody {
                code: code.to_string(),
                message: message.into(),
            }),
        }
    }
}

// ----- Handler --------------------------------------------------------------

/// Returns Some(response) for sync handlers, None for async handlers that
/// will write their own response via the SharedWriter when the Steam
/// callback fires.
fn handle(
    req: &Request,
    bridge: Option<&Arc<steam::Bridge>>,
    net: Option<&steam_net::SteamNet>,
    writer: &SharedWriter,
) -> Option<Response> {
    match req.method.as_str() {
        "local_player" => Some(match bridge {
            Some(b) => {
                let p = b.local_player();
                Response::ok(
                    &req.id,
                    serde_json::json!({
                        "steamId64": p.steam_id_64.to_string(),
                        "personaName": p.persona_name,
                    }),
                )
            }
            None => Response::err(&req.id, "steam_unavailable", "Steamworks not initialised"),
        }),

        "report_achievement" => Some(match bridge {
            Some(_b) => {
                // §16 wiring lands later; the IPC plumbing is here.
                let id = req.params.get("id").and_then(|v| v.as_str()).unwrap_or("");
                info!("ipc: report_achievement(id={id}) — Steam dispatch deferred to §16");
                Response::ok(&req.id, serde_json::Value::Null)
            }
            None => Response::err(&req.id, "steam_unavailable", "Steamworks not initialised"),
        }),

        "open_invite_overlay" => Some(match bridge {
            Some(b) => {
                let lobby_str = req
                    .params
                    .get("lobbyId")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");
                match lobby_str.parse::<u64>() {
                    Ok(raw) => {
                        b.client
                            .friends()
                            .activate_invite_dialog(steamworks::LobbyId::from_raw(raw));
                        info!("ipc: opened invite dialog for lobby {raw}");
                        Response::ok(&req.id, serde_json::Value::Null)
                    }
                    Err(e) => Response::err(
                        &req.id,
                        "bad_lobby_id",
                        format!("not a u64 lobby id: {e}"),
                    ),
                }
            }
            None => Response::err(&req.id, "steam_unavailable", "Steamworks not initialised"),
        }),

        "create_lobby" => match bridge {
            Some(b) => {
                let max_members = req
                    .params
                    .get("maxPlayers")
                    .and_then(|v| v.as_u64())
                    .unwrap_or(4) as u32;
                // §14R-A: optional extra metadata stamped into the Steam
                // lobby so /find-game listings can show map + host info
                // without each joiner having to dial in first. All three
                // fields are best-effort — older /create-game callers may
                // omit them and the lobby still works.
                let map_id = req
                    .params
                    .get("mapId")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string();
                let local_lobby_id = req
                    .params
                    .get("localLobbyId")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string();
                let id = req.id.clone();
                let writer = writer.clone();
                let bridge_for_cb: Arc<steam::Bridge> = b.clone();
                let host_steam_id = b.client.user().steam_id().raw();
                let host_persona = b.client.friends().name();
                info!("ipc: create_lobby(maxPlayers={max_members}, mapId={map_id:?}, localLobbyId={local_lobby_id:?})");
                b.client.matchmaking().create_lobby(
                    steamworks::LobbyType::FriendsOnly,
                    max_members,
                    move |result| {
                        let response = match &result {
                            Ok(lobby_id) => {
                                // §14.2 / §14R-A: stamp the lobby metadata
                                // joiners need both for the steam-sockets
                                // handoff (host_steam_id) and for the
                                // /find-game listing (host_persona, map_id,
                                // local_lobby_id, status).
                                let mm = bridge_for_cb.client.matchmaking();
                                mm.set_lobby_data(
                                    *lobby_id,
                                    "host_steam_id",
                                    &host_steam_id.to_string(),
                                );
                                mm.set_lobby_data(*lobby_id, "host_persona", &host_persona);
                                mm.set_lobby_data(*lobby_id, "status", "waiting");
                                if !map_id.is_empty() {
                                    mm.set_lobby_data(*lobby_id, "map_id", &map_id);
                                }
                                if !local_lobby_id.is_empty() {
                                    mm.set_lobby_data(
                                        *lobby_id,
                                        "local_lobby_id",
                                        &local_lobby_id,
                                    );
                                }
                                push_notification(
                                    &writer,
                                    "lobby_hosted",
                                    serde_json::json!({
                                        "lobbyId": lobby_id.raw().to_string(),
                                        "hostSteamId64": host_steam_id.to_string(),
                                        "localLobbyId": local_lobby_id,
                                    }),
                                );
                                Response::ok(
                                    &id,
                                    serde_json::json!({
                                        "lobbyId": lobby_id.raw().to_string(),
                                    }),
                                )
                            }
                            Err(e) => Response::err(
                                &id,
                                "steam_error",
                                format!("create_lobby failed: {e:?}"),
                            ),
                        };
                        write_response(&writer, &response);
                    },
                );
                None
            }
            None => Some(Response::err(
                &req.id,
                "steam_unavailable",
                "Steamworks not initialised",
            )),
        },

        "join_lobby" => match bridge {
            Some(b) => {
                let lobby_str = req
                    .params
                    .get("lobbyId")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");
                let raw = match lobby_str.parse::<u64>() {
                    Ok(v) => v,
                    Err(e) => {
                        return Some(Response::err(
                            &req.id,
                            "bad_lobby_id",
                            format!("not a u64 lobby id: {e}"),
                        ));
                    }
                };
                let id = req.id.clone();
                let writer = writer.clone();
                let bridge_for_cb: Arc<steam::Bridge> = b.clone();
                info!("ipc: join_lobby({raw})");
                b.client.matchmaking().join_lobby(
                    steamworks::LobbyId::from_raw(raw),
                    move |result| {
                        let response = match &result {
                            Ok(lobby_id) => {
                                // §14.3: read host's SteamID from the lobby
                                // metadata so we can open a Steam Sockets
                                // connection to them. Empty/missing data
                                // means the host hasn't stamped it yet
                                // (race during create_lobby) — surface as
                                // a soft error so the SPA can retry.
                                let mm = bridge_for_cb.client.matchmaking();
                                let host_id_str = mm
                                    .lobby_data(*lobby_id, "host_steam_id")
                                    .unwrap_or_default();
                                let local_lobby_id =
                                    mm.lobby_data(*lobby_id, "local_lobby_id").unwrap_or_default();
                                let map_id =
                                    mm.lobby_data(*lobby_id, "map_id").unwrap_or_default();
                                match host_id_str.parse::<u64>() {
                                    Ok(host_id) => {
                                        push_notification(
                                            &writer,
                                            "lobby_joined",
                                            serde_json::json!({
                                                "lobbyId": lobby_id.raw().to_string(),
                                                "hostSteamId64": host_id.to_string(),
                                                "localLobbyId": local_lobby_id,
                                            }),
                                        );
                                        Response::ok(
                                            &id,
                                            serde_json::json!({
                                                "lobbyId": lobby_id.raw().to_string(),
                                                "hostSteamId64": host_id.to_string(),
                                                "localLobbyId": local_lobby_id,
                                                "mapId": map_id,
                                            }),
                                        )
                                    }
                                    Err(_) => Response::err(
                                        &id,
                                        "lobby_missing_host_id",
                                        "host_steam_id metadata absent or malformed",
                                    ),
                                }
                            }
                            Err(_) => Response::err(
                                &id,
                                "steam_error",
                                "join_lobby failed (unauthorized / lobby full / lobby gone)",
                            ),
                        };
                        write_response(&writer, &response);
                    },
                );
                None
            }
            None => Some(Response::err(
                &req.id,
                "steam_unavailable",
                "Steamworks not initialised",
            )),
        },

        // §12.1 — Steam Networking Sockets transport bridge.
        "open_listener" => Some(match net {
            Some(n) => {
                let vp = req
                    .params
                    .get("virtualPort")
                    .and_then(|v| v.as_i64())
                    .map(|v| v as i32)
                    .unwrap_or(steam_net::NOMADS_VIRTUAL_PORT);
                match n.open_listener(vp) {
                    Ok(()) => Response::ok(&req.id, serde_json::json!({"virtualPort": vp})),
                    Err(e) => Response::err(&req.id, "steam_net_error", e),
                }
            }
            None => Response::err(
                &req.id,
                "steam_unavailable",
                "Steamworks not initialised; cannot open listener",
            ),
        }),

        "connect_to" => Some(match net {
            Some(n) => {
                let steam_id_str = req
                    .params
                    .get("steamId64")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");
                let vp = req
                    .params
                    .get("virtualPort")
                    .and_then(|v| v.as_i64())
                    .map(|v| v as i32)
                    .unwrap_or(steam_net::NOMADS_VIRTUAL_PORT);
                match steam_id_str.parse::<u64>() {
                    Ok(raw) => match n.connect_to(raw, vp) {
                        Ok(peer_id) => Response::ok(
                            &req.id,
                            serde_json::json!({"peerId": peer_id.to_string()}),
                        ),
                        Err(e) => Response::err(&req.id, "steam_net_error", e),
                    },
                    Err(e) => Response::err(
                        &req.id,
                        "bad_steam_id",
                        format!("not a u64 steamId64: {e}"),
                    ),
                }
            }
            None => Response::err(
                &req.id,
                "steam_unavailable",
                "Steamworks not initialised; cannot connect",
            ),
        }),

        "send_peer_message" => Some(match net {
            Some(n) => {
                let peer_str = req
                    .params
                    .get("peerId")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");
                let payload_b64 = req
                    .params
                    .get("payload")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");
                let peer_id = match peer_str.parse::<u64>() {
                    Ok(v) => v,
                    Err(e) => {
                        return Some(Response::err(
                            &req.id,
                            "bad_peer_id",
                            format!("not a u64 peerId: {e}"),
                        ));
                    }
                };
                let payload = match base64::Engine::decode(
                    &base64::engine::general_purpose::STANDARD,
                    payload_b64,
                ) {
                    Ok(p) => p,
                    Err(e) => {
                        return Some(Response::err(
                            &req.id,
                            "bad_payload",
                            format!("payload not base64: {e}"),
                        ));
                    }
                };
                match n.send(peer_id, payload) {
                    Ok(()) => Response::ok(&req.id, serde_json::Value::Null),
                    Err(e) => Response::err(&req.id, "steam_net_error", e),
                }
            }
            None => Response::err(
                &req.id,
                "steam_unavailable",
                "Steamworks not initialised; cannot send",
            ),
        }),

        "close_peer" => Some(match net {
            Some(n) => {
                let peer_str = req
                    .params
                    .get("peerId")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");
                match peer_str.parse::<u64>() {
                    Ok(peer_id) => match n.close(peer_id) {
                        Ok(was_open) => {
                            Response::ok(&req.id, serde_json::json!({"closed": was_open}))
                        }
                        Err(e) => Response::err(&req.id, "steam_net_error", e),
                    },
                    Err(e) => Response::err(
                        &req.id,
                        "bad_peer_id",
                        format!("not a u64 peerId: {e}"),
                    ),
                }
            }
            None => Response::err(
                &req.id,
                "steam_unavailable",
                "Steamworks not initialised; cannot close",
            ),
        }),

        other => Some(Response::err(
            &req.id,
            "unknown_method",
            format!("unknown method {other}"),
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn request_parses_with_and_without_params() {
        let no_params: Request = serde_json::from_str(r#"{"id":"1","method":"local_player"}"#).unwrap();
        assert_eq!(no_params.id, "1");
        assert_eq!(no_params.method, "local_player");
        assert!(no_params.params.is_null());

        let with_params: Request = serde_json::from_str(
            r#"{"id":"2","method":"report_achievement","params":{"id":"ACH_X"}}"#,
        )
        .unwrap();
        assert_eq!(with_params.params.get("id").unwrap().as_str(), Some("ACH_X"));
    }

    #[test]
    fn response_ok_skips_error_field() {
        let r = Response::ok("req-1", serde_json::json!({"x":1}));
        let s = serde_json::to_string(&r).unwrap();
        assert!(s.contains(r#""id":"req-1""#));
        assert!(s.contains(r#""result":{"x":1}"#));
        assert!(!s.contains("error"));
    }

    #[test]
    fn response_err_skips_result_field() {
        let r = Response::err("req-1", "steam_unavailable", "Steam not running");
        let s = serde_json::to_string(&r).unwrap();
        assert!(s.contains(r#""error""#));
        assert!(s.contains(r#""code":"steam_unavailable""#));
        assert!(!s.contains("result"));
    }

    /// Test-only writer that never actually writes — sync handlers in
    /// these tests return Some(Response) directly and never touch it.
    fn null_writer() -> SharedWriter {
        Arc::new(Mutex::new(Box::new(Vec::<u8>::new())))
    }

    #[test]
    fn handle_local_player_without_bridge_returns_unavailable() {
        let req = Request {
            id: "x".into(),
            method: "local_player".into(),
            params: serde_json::Value::Null,
        };
        let resp = handle(&req, None, None, &null_writer()).expect("sync handler returns Some");
        let err = resp.error.expect("error should be present");
        assert_eq!(err.code, "steam_unavailable");
    }

    #[test]
    fn handle_unknown_method_returns_unknown_method() {
        let req = Request {
            id: "x".into(),
            method: "totally_made_up".into(),
            params: serde_json::Value::Null,
        };
        let resp = handle(&req, None, None, &null_writer()).expect("sync handler returns Some");
        assert_eq!(resp.error.unwrap().code, "unknown_method");
    }
}
