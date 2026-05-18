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
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::thread;

use interprocess::local_socket::{
    prelude::*, GenericFilePath, GenericNamespaced, ListenerOptions, Name,
};
use log::{info, warn};
use serde::{Deserialize, Serialize};

use crate::steam;

/// Returns (bind_name, env_path) for the local socket. `bind_name` is what
/// interprocess uses (namespaced name on Windows, file path on Unix).
/// `env_path` is what we put in NOMADS_IPC_PATH for the Go child to dial —
/// fully qualified so the Go side can dial without OS-specific path
/// reconstruction:
///   Windows: \\.\pipe\nomads-shell-<pid>
///   Unix:    <runtime_dir>/shell.sock
fn socket_paths(runtime_dir: &std::path::Path) -> (String, String) {
    if cfg!(windows) {
        let name = format!("nomads-shell-{}", std::process::id());
        let env_path = format!(r"\\.\pipe\{name}");
        (name, env_path)
    } else {
        let p = runtime_dir.join("shell.sock").to_string_lossy().into_owned();
        (p.clone(), p)
    }
}

#[allow(dead_code)]
pub fn socket_path(runtime_dir: &std::path::Path) -> Result<PathBuf, std::io::Error> {
    let (_, env) = socket_paths(runtime_dir);
    Ok(PathBuf::from(env))
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
pub fn start(
    runtime_dir: &std::path::Path,
    bridge: Option<Arc<steam::Bridge>>,
) -> Result<String, std::io::Error> {
    let (bind_str, env_path) = socket_paths(runtime_dir);

    // Unix: clean up any stale socket file from a previous unclean exit so
    // bind doesn't fail with EADDRINUSE.
    if !cfg!(windows) {
        let _ = std::fs::remove_file(&bind_str);
    }

    let bind_name = name_from_path(&bind_str)?;
    let listener = ListenerOptions::new().name(bind_name).create_sync()?;
    info!("ipc: listening on {env_path}");

    thread::spawn(move || {
        // Single Go-child connects per session. accept() blocks until it does.
        match listener.accept() {
            Ok(conn) => {
                info!("ipc: Go child connected");
                run_dispatcher(conn, bridge);
                info!("ipc: dispatcher exited (connection closed)");
            }
            Err(e) => warn!("ipc: accept failed: {e}"),
        }
    });

    Ok(env_path)
}

fn run_dispatcher<C: std::io::Read + std::io::Write + Send + 'static>(
    conn: C,
    bridge: Option<Arc<steam::Bridge>>,
) {
    let (read_half, write_half) = split_duplex(conn);
    // Type-erase the writer so async closures (which capture it for
    // callback-driven responses) don't need a per-conn generic parameter.
    let writer: SharedWriter = Arc::new(Mutex::new(Box::new(write_half)));
    let reader = BufReader::new(read_half);

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
        if let Some(response) = handle(&request, bridge.as_ref(), &writer) {
            write_response(&writer, &response);
        }
    }
}

/// Boxed-trait writer so async handlers (which `move` the writer into
/// 'static closures) don't need to know the underlying transport type.
type SharedWriter = Arc<Mutex<Box<dyn std::io::Write + Send>>>;

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

// interprocess's LocalSocketStream is duplex but not `Clone`. To allow the
// reader thread and (future) async response writers to share it, wrap it
// in an Arc<Mutex<_>>. For now we serialise reads and writes through this
// helper; if we ever need true concurrent reads + writes we can swap to
// the tokio variant.
fn split_duplex<S: std::io::Read + std::io::Write + Send + 'static>(
    s: S,
) -> (DuplexReadHalf<S>, DuplexWriteHalf<S>) {
    let shared = Arc::new(Mutex::new(s));
    (
        DuplexReadHalf {
            inner: shared.clone(),
        },
        DuplexWriteHalf { inner: shared },
    )
}

pub struct DuplexReadHalf<S> {
    inner: Arc<Mutex<S>>,
}

pub struct DuplexWriteHalf<S> {
    inner: Arc<Mutex<S>>,
}

impl<S: std::io::Read> std::io::Read for DuplexReadHalf<S> {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        self.inner.lock().unwrap().read(buf)
    }
}

impl<S: std::io::Write> std::io::Write for DuplexWriteHalf<S> {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        self.inner.lock().unwrap().write(buf)
    }
    fn flush(&mut self) -> std::io::Result<()> {
        self.inner.lock().unwrap().flush()
    }
}

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
                let id = req.id.clone();
                let writer = writer.clone();
                info!("ipc: create_lobby(maxPlayers={max_members})");
                b.client.matchmaking().create_lobby(
                    steamworks::LobbyType::FriendsOnly,
                    max_members,
                    move |result| {
                        let response = match result {
                            Ok(lobby_id) => Response::ok(
                                &id,
                                serde_json::json!({"lobbyId": lobby_id.raw().to_string()}),
                            ),
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
                info!("ipc: join_lobby({raw})");
                b.client.matchmaking().join_lobby(
                    steamworks::LobbyId::from_raw(raw),
                    move |result| {
                        let response = match result {
                            Ok(lobby_id) => Response::ok(
                                &id,
                                serde_json::json!({"lobbyId": lobby_id.raw().to_string()}),
                            ),
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
        let resp = handle(&req, None, &null_writer()).expect("sync handler returns Some");
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
        let resp = handle(&req, None, &null_writer()).expect("sync handler returns Some");
        assert_eq!(resp.error.unwrap().code, "unknown_method");
    }
}
