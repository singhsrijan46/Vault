package p2p

const (
	IncomingMessage = 0x1
	IncomingStream  = 0x2
)

// RPC (Remote Procedure Call) to encapsulate messages and streams sent over the network.
type RPC struct {
	From    string
	Payload []byte
	Stream  bool
}

// example : rpc := RPC{
//     From:    "192.168.1.1:3000", // Source address
//     Payload: []byte("Hello, world!"), // Data to send
//     Stream:  false, // Not a stream
// }
