package cipher

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func newCipher(t *testing.T, keys ...[]byte) *AES256 {
	t.Helper()
	c, err := NewWithKeys(keys...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newKey(t *testing.T) []byte {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRoundTrip(t *testing.T) {
	c := newCipher(t, newKey(t))
	sealed, err := c.Encrypt([]byte("hello"), []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("hello")) {
		t.Fatal("the data is in the clear")
	}
	plain, err := c.Decrypt(sealed, []byte("a"))
	if err != nil || string(plain) != "hello" {
		t.Fatalf("got %q, %v", plain, err)
	}
	other, _ := c.Encrypt([]byte("hello"), []byte("a"))
	if bytes.Equal(sealed, other) {
		t.Fatal("two encryptions of the same data must differ (nonce)")
	}
}

func TestDecryptFails(t *testing.T) {
	c := newCipher(t, newKey(t))
	sealed, _ := c.Encrypt([]byte("hello"), []byte("a"))
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	cases := map[string]struct{ data, aad []byte }{
		"other aad":  {sealed, []byte("b")},
		"no aad":     {sealed, nil},
		"tampered":   {tampered, []byte("a")},
		"empty":      {nil, []byte("a")},
		"short":      {sealed[:5], []byte("a")},
		"only a key": {sealed[:1], []byte("a")},
	}
	for name, tc := range cases {
		if _, err := c.Decrypt(tc.data, tc.aad); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := newCipher(t, newKey(t)).Decrypt(sealed, []byte("a")); err == nil {
		t.Error("another key must not open it")
	}
}

func TestKeyRotation(t *testing.T) {
	oldKey, freshKey := newKey(t), newKey(t)
	old := newCipher(t, oldKey)
	sealedOld, _ := old.Encrypt([]byte("x"), nil)

	rotated := newCipher(t, freshKey, oldKey)
	if plain, err := rotated.Decrypt(sealedOld, nil); err != nil || string(plain) != "x" {
		t.Fatalf("the old key must still open: %q %v", plain, err)
	}
	sealedNew, _ := rotated.Encrypt([]byte("y"), nil)
	if _, err := old.Decrypt(sealedNew, nil); err == nil {
		t.Fatal("new data is sealed with the first key only")
	}
	if _, err := newCipher(t, freshKey).Decrypt(sealedOld, nil); err == nil {
		t.Fatal("without the old key, old data does not open")
	}
}

func TestConstructors(t *testing.T) {
	if _, err := NewWithKeys(); err == nil {
		t.Error("no keys must fail")
	}
	if _, err := NewWithKeys(make([]byte, 16)); err == nil || !strings.Contains(err.Error(), "16 bytes") {
		t.Errorf("a short key must fail, got %v", err)
	}
	if _, err := NewFromStrings("not base64!"); err == nil {
		t.Error("bad base64 must fail")
	}
	s, err := GenerateKeyString()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewFromStrings(s); err != nil {
		t.Errorf("a generated key must be accepted: %v", err)
	}
	if _, err := New(); err != nil {
		t.Error(err)
	}
}

func TestSealOpen(t *testing.T) {
	c := newCipher(t, newKey(t))
	token, err := Seal(c, "session", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Open(c, "session", token, time.Hour); err != nil || got != "abc" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := Open(c, "session", token, 0); err != nil || got != "abc" {
		t.Fatalf("no max age: %q, %v", got, err)
	}
	empty, _ := Seal(c, "p", "")
	if got, err := Open(c, "p", empty, 0); err != nil || got != "" {
		t.Fatalf("an empty value: %q, %v", got, err)
	}
}

func TestOpenFails(t *testing.T) {
	c := newCipher(t, newKey(t))
	token, _ := Seal(c, "session", "abc")
	if _, err := Open(c, "language", token, 0); err == nil {
		t.Error("a value sealed for another purpose must not open")
	}
	if _, err := Open(c, "session", token+"x", 0); err == nil {
		t.Error("a manipulated value must not open")
	}
	if _, err := Open(c, "session", "***", 0); err == nil {
		t.Error("a value that is not base64 must not open")
	}
	// What a user chose to put in a cookie cannot be turned into a session.
	user, _ := Seal(c, "cookie:theme", "abc")
	if _, err := Open(c, "session", user, 0); err == nil {
		t.Error("a cookie of the user opened as a session")
	}
}

func TestOpenExpires(t *testing.T) {
	c := newCipher(t, newKey(t))
	token, _ := Seal(c, "p", "abc")
	time.Sleep(1100 * time.Millisecond)
	if _, err := Open(c, "p", token, time.Second); !errors.Is(err, ErrExpired) {
		t.Fatalf("want ErrExpired, got %v", err)
	}
}
