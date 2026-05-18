//go:build windows

package steam

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// dialIPC connects to the Tauri shell's named pipe on Windows. The path is
// the fully-qualified `\\.\pipe\<name>` form the Rust shell publishes via
// NOMADS_IPC_PATH.
func dialIPC(path string) (net.Conn, error) {
	timeout := 5 * time.Second
	return winio.DialPipe(path, &timeout)
}
