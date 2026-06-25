package p2p

// create custom handshake logic
// If the handshake succeeds, it returns nil
// If it fails, it returns an error
type HandshakeFunc func(Peer) error

// ==== For testing or development ====
// It accepts a Peer but performs no checks.
// It always returns nil (no error), meaning the handshake is automatically successful.
func NOPHandshakeFunc(Peer) error {
	return nil
}
