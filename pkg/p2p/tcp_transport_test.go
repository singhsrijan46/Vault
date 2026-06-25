package p2p

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTCPTransport(t *testing.T) {
	opts := TCPTransportOpts{
		ListenAddr:    ":3000",
		HandshakeFunc: NOPHandshakeFunc,
		Decoder:       DefaultDecoder{},
	}
	tr := NewTCPTransport(opts)
	// assertion checks that the ListenAddr field of the TCPTransport instance (tr) is set to ":3000"
	assert.Equal(t, tr.ListenAddr, ":3000")

	// checks that the ListenAndAccept method of the TCPTransport instance returns nil
	assert.Nil(t, tr.ListenAndAccept())
}
