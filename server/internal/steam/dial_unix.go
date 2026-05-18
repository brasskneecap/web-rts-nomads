//go:build !windows

package steam

import "net"

// dialIPC connects to the Tauri shell's Unix socket. The path is the
// fully-qualified filesystem path under <userdata>/runtime/shell.sock.
func dialIPC(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}
