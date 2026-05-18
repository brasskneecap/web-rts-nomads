package transportbridge

// MessageStream is the minimal byte-channel surface ProxyStreams needs to
// pipe messages between a joiner's SPA and an upstream host. Both
// `*websocket.Conn` (via WSConnStream below) and `ws.Transport` (which
// already has matching method signatures) satisfy this interface, so the
// Direct-connect and Steam-Sockets joiner paths share the same byte-forwarding
// loop.
//
// Defined here rather than in ws/ to keep transportbridge cycle-free —
// `ws` imports `transportbridge`, so transportbridge cannot import `ws`.
// Go's structural interface satisfaction means we don't need to know about
// ws.Transport here; any type whose methods match is accepted.
type MessageStream interface {
	ReadMessage() ([]byte, error)
	WriteMessage(payload []byte) error
	Close() error
}

// ProxyStreams runs two goroutines that pipe messages between `a` and `b`.
// Bytes are forwarded verbatim. Either side closing or erroring tears down
// the other. Blocks until both directions have terminated — the caller is
// typically a hub HandleWS goroutine and the return releases the WS
// upgrade.
//
// Replacement for the original Proxy(*websocket.Conn, *websocket.Conn): the
// existing function is preserved as a thin wrapper so direct-connect tests
// keep passing unchanged.
func ProxyStreams(a, b MessageStream) {
	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		for {
			data, err := a.ReadMessage()
			if err != nil {
				return
			}
			if err := b.WriteMessage(data); err != nil {
				return
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		for {
			data, err := b.ReadMessage()
			if err != nil {
				return
			}
			if err := a.WriteMessage(data); err != nil {
				return
			}
		}
	}()

	// First direction to terminate closes both sides so the other goroutine
	// unblocks; then we wait for both halves to fully drain before returning.
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}
