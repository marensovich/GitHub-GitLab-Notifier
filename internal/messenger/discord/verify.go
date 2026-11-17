package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
)

// verify checks an interaction request: Discord signs timestamp+body with
// the application's key and sends the signature as hex.
func verify(key ed25519.PublicKey, signature, timestamp string, body []byte) bool {
	sig, err := hex.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize || len(key) != ed25519.PublicKeySize {
		return false
	}
	msg := make([]byte, 0, len(timestamp)+len(body))
	msg = append(append(msg, timestamp...), body...)
	return ed25519.Verify(key, msg, sig)
}

// parsePublicKey reads the application's public key from the config.
func parsePublicKey(hexKey string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("messengers.discord.public_key must be the 64 hex characters from the Discord developer portal")
	}
	return ed25519.PublicKey(raw), nil
}
