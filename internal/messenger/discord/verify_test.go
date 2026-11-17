package discord

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func TestVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ts, body := "1700000000", []byte(`{"type":1}`)
	sig := hex.EncodeToString(ed25519.Sign(priv, append([]byte(ts), body...)))

	if !verify(pub, sig, ts, body) {
		t.Fatal("valid signature rejected")
	}
	for name, ok := range map[string]bool{
		"changed body":      verify(pub, sig, ts, []byte(`{"type":2}`)),
		"changed timestamp": verify(pub, sig, "1700000001", body),
		"non-hex signature": verify(pub, "zz"+sig[2:], ts, body),
		"empty signature":   verify(pub, "", ts, body),
		"short signature":   verify(pub, sig[:10], ts, body),
	} {
		if ok {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestParsePublicKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	if k, err := parsePublicKey(hex.EncodeToString(pub)); err != nil || !k.Equal(pub) {
		t.Fatalf("valid key: %v", err)
	}
	for _, bad := range []string{hex.EncodeToString(pub)[:63], strings.Repeat("z", 64), ""} {
		if _, err := parsePublicKey(bad); err == nil || !strings.Contains(err.Error(), "messengers.discord.public_key") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}
