package cipher

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

// forge makes what Seal would make, but with the time and the bytes we choose.
func forge(t *testing.T, c *AES256, purpose string, stamp time.Time, data string) string {
	t.Helper()
	plain := make([]byte, 8, 8+len(data))
	binary.BigEndian.PutUint64(plain, uint64(stamp.Unix()))
	plain = append(plain, data...)
	sealed, err := c.Encrypt(plain, []byte(purpose))
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(sealed)
}

func TestOpenReturnsWhatWasSealedAndNothingOfTheStamp(t *testing.T) {
	c := newCipher(t, newKey(t))
	token := forge(t, c, "p", time.Now(), "hello")
	if got, err := Open(c, "p", token, time.Hour); err != nil || got != "hello" {
		t.Fatalf("got %q, %v: the stamp is 8 bytes and the rest is the value", got, err)
	}
}

func TestSealStartsWithTheTimeInEightBytes(t *testing.T) {
	c := newCipher(t, newKey(t))
	before := time.Now().Unix()
	token, err := Seal(c, "p", "hello")
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := base64.RawURLEncoding.DecodeString(token)
	plain, err := c.Decrypt(sealed, []byte("p"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 8+len("hello") || string(plain[8:]) != "hello" {
		t.Fatalf("plain = %q", plain)
	}
	if at := int64(binary.BigEndian.Uint64(plain)); at < before || at > time.Now().Unix() {
		t.Fatalf("stamp %d is not the time of sealing (%d)", at, before)
	}
}

func TestOpenRefusesWhatWasSealedInTheFuture(t *testing.T) {
	c := newCipher(t, newKey(t))
	soon := forge(t, c, "p", time.Now().Add(clockSkew/2), "x")
	if got, err := Open(c, "p", soon, 0); err != nil || got != "x" {
		t.Fatalf("a little skew between servers is fine: %q, %v", got, err)
	}
	future := forge(t, c, "p", time.Now().Add(2*clockSkew), "x")
	if _, err := Open(c, "p", future, 0); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("want a 'sealed in the future' error, got %v", err)
	}
}

func TestOpenRefusesAValueTooShortToHoldTheTime(t *testing.T) {
	c := newCipher(t, newKey(t))
	sealed, err := c.Encrypt([]byte("1234567"), []byte("p")) // 7 bytes: one short
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(sealed)
	if _, err := Open(c, "p", token, 0); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("want a malformed error, got %v", err)
	}
	exact, _ := c.Encrypt(make([]byte, 8), []byte("p")) // only the time: the value is empty
	zero := base64.RawURLEncoding.EncodeToString(exact)
	if _, err := Open(c, "p", zero, 0); errors.Is(err, ErrExpired) || (err != nil && strings.Contains(err.Error(), "malformed")) {
		t.Fatalf("eight bytes are enough: %v", err)
	}
}

func TestOpenExpiryIsRelativeToWhenItWasSealed(t *testing.T) {
	c := newCipher(t, newKey(t))
	old := forge(t, c, "p", time.Now().Add(-time.Hour), "x")
	if _, err := Open(c, "p", old, 30*time.Minute); !errors.Is(err, ErrExpired) {
		t.Fatalf("an hour old with 30 minutes: want ErrExpired, got %v", err)
	}
	if _, err := Open(c, "p", old, time.Nanosecond); !errors.Is(err, ErrExpired) {
		t.Fatalf("any positive max age expires an old value: got %v", err)
	}
	if got, err := Open(c, "p", old, 2*time.Hour); err != nil || got != "x" {
		t.Fatalf("an hour old with two hours: %q, %v", got, err)
	}
	if got, err := Open(c, "p", old, 0); err != nil || got != "x" {
		t.Fatalf("zero never expires: %q, %v", got, err)
	}
}

func TestOpenErrorsSayWhy(t *testing.T) {
	c := newCipher(t, newKey(t))
	token, _ := Seal(c, "p", "x")
	if _, err := Open(c, "p", "***", 0); err == nil || !strings.Contains(err.Error(), "cannot open") {
		t.Errorf("not base64: %v", err)
	}
	if _, err := Open(c, "other", token, 0); err == nil || !strings.Contains(err.Error(), "cannot open") {
		t.Errorf("other purpose: %v", err)
	}
}

func TestSealFailsWhenTheCipherFails(t *testing.T) {
	boom := errors.New("boom")
	if _, err := Seal(failing{boom}, "p", "x"); !errors.Is(err, boom) {
		t.Fatalf("want the error of the cipher, got %v", err)
	}
}

type failing struct{ err error }

func (f failing) Encrypt(_, _ []byte) ([]byte, error) { return nil, f.err }
func (f failing) Decrypt(_, _ []byte) ([]byte, error) { return nil, f.err }

func TestAESFirstByteIsTheKeyID(t *testing.T) {
	k := newKey(t)
	c := newCipher(t, k)
	sealed, _ := c.Encrypt([]byte("x"), nil)
	if sum := sha256.Sum256(k); sealed[0] != sum[0] {
		t.Fatalf("first byte %d, want the first byte of the SHA-256 of the key (%d)", sealed[0], sum[0])
	}
}

func TestAESDecryptErrorsSayWhy(t *testing.T) {
	c := newCipher(t, newKey(t))
	sealed, _ := c.Encrypt([]byte("hello"), []byte("a"))
	if _, err := c.Decrypt(sealed, []byte("b")); err == nil || !strings.Contains(err.Error(), "cannot decrypt") || strings.Contains(err.Error(), "unknown key") {
		t.Errorf("a wrong aad is an authentication failure, not an unknown key: %v", err)
	}
	other := newCipher(t, newKey(t))
	if _, err := other.Decrypt(sealed, []byte("a")); err == nil {
		t.Error("another key must not open it")
	}
	// A nonce and nothing else: there is a key, it is not "malformed", it just fails to authenticate.
	nonceOnly := sealed[:1+c.keys[0].aead.NonceSize()]
	if _, err := c.Decrypt(nonceOnly, []byte("a")); err == nil || !strings.Contains(err.Error(), "cannot decrypt") {
		t.Errorf("a nonce with no ciphertext: %v", err)
	}
	if _, err := c.Decrypt(sealed[:5], []byte("a")); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Errorf("shorter than a nonce: %v", err)
	}
}

func TestNewFromStringsSaysWhichKeyIsNotBase64(t *testing.T) {
	good, _ := GenerateKeyString()
	if _, err := NewFromStrings(good, "not base64!"); err == nil || !strings.Contains(err.Error(), "key 1 is not base64") {
		t.Fatalf("got %v", err)
	}
}

func TestOpenSaysWhatWentWrong(t *testing.T) {
	c := newCipher(t, newKey(t))
	token, _ := Seal(c, "p", "x")
	if _, err := Open(c, "p", "***", 0); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Errorf("a value that is not base64 says so: %v", err)
	}
	if _, err := Open(c, "other", token, 0); err == nil || strings.Contains(err.Error(), "malformed") {
		t.Errorf("a value for another purpose fails to authenticate, it is not malformed: %v", err)
	}
}

// Keys whose ids (first byte of their SHA-256) differ, so the tests do not depend on luck.
func twoKeys(t *testing.T) (a, b []byte) {
	t.Helper()
	a, b = make([]byte, 32), make([]byte, 32)
	for i := range a {
		a[i], b[i] = 1, 2
	}
	if sha256.Sum256(a)[0] == sha256.Sum256(b)[0] {
		t.Skip("the two test keys have the same id")
	}
	return a, b
}

func TestAESDecryptWithAnUnknownKeyID(t *testing.T) {
	ka, kb := twoKeys(t)
	sealedByA, _ := newCipher(t, ka).Encrypt([]byte("hello"), nil)
	onlyB := newCipher(t, kb)
	if _, err := onlyB.Decrypt(sealedByA, nil); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("data of a key we do not have: %v", err)
	}
	// Just the id: nothing else to read, but the id is already not ours.
	if _, err := onlyB.Decrypt(sealedByA[:1], nil); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("only an unknown id: %v", err)
	}
	// With the right id and nothing after it, the nonce is missing.
	own, _ := onlyB.Encrypt([]byte("x"), nil)
	if _, err := onlyB.Decrypt(own[:1], nil); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Errorf("only our id: %v", err)
	}
	both := newCipher(t, kb, ka)
	if plain, err := both.Decrypt(sealedByA, nil); err != nil || string(plain) != "hello" {
		t.Errorf("the second key opens what it sealed: %q, %v", plain, err)
	}
}
