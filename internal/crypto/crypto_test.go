package crypto

import (
	"bytes"
	"fmt"
	"testing"
)

// Steps:
// Define the Payload
// Generate an Encryption Key
// Encrypt the Data
// Print Debug Information
// Decrypt the Encrypted Data
// Check the Number of Bytes Written
// Validate Decryption

func TestCopyEncryptDecrypt(t *testing.T) {
	payload := "Foo not bar"
	src := bytes.NewReader([]byte(payload))
	dst := new(bytes.Buffer)

	// generates a random 32-byte AES key.
	key := NewEncryptionKey()

	_, err := CopyEncrypt(key, src, dst)
	if err != nil {
		t.Error(err)
	}

	fmt.Println(len(payload))
	fmt.Println(len(dst.String()))

	out := new(bytes.Buffer)
	nw, err := CopyDecrypt(key, dst, out)
	if err != nil {
		t.Error(err)
	}

	// copyDecrypt should return the number of decrypted bytes written (not including IV)
	if nw != len(payload) {
		t.Errorf("Expected %d decrypted bytes, got %d", len(payload), nw)
	}

	if out.String() != payload {
		t.Errorf("decryption failed!!!")
	}
}
