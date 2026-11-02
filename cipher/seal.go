package cipher

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/deltegui/owl/v3/core"
)

// ErrExpired is what Open returns for a value older than its maxAge.
var ErrExpired = errors.New("sealed value expired")

const (
	stampBytes = 8
	// clockSkew is how far in the future the time of a sealed value can be (other server's clock).
	clockSkew = time.Minute
)

// Seal encrypts data for a purpose and encodes it in base64 to put it in a cookie or a
// form. The time is sealed with it, so Open can refuse it when it is too old. purpose
// is anything that tells apart the uses of the key ("session", "cookie:theme"): Open
// only opens it with the same one.
func Seal(c core.Cipher, purpose, data string) (string, error) {
	plain := make([]byte, stampBytes, stampBytes+len(data))
	binary.BigEndian.PutUint64(plain, uint64(time.Now().Unix())) //nolint:gosec // G115: Unix() solo es negativo con un reloj anterior a 1970
	plain = append(plain, data...)
	sealed, err := c.Encrypt(plain, []byte(purpose))
	if err != nil {
		return "", fmt.Errorf("cannot seal: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open is the reverse of Seal. It fails if the value was sealed for another purpose, was
// manipulated, or is older than maxAge (zero means it does not expire).
func Open(c core.Cipher, purpose, token string, maxAge time.Duration) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("cannot open: %w", err)
	}
	plain, err := c.Decrypt(sealed, []byte(purpose))
	if err != nil {
		return "", fmt.Errorf("cannot open: %w", err)
	}
	if len(plain) < stampBytes {
		return "", errors.New("cannot open: malformed value")
	}
	// El valor sale de descifrar con AES-GCM: solo pudo escribirlo Seal con una hora real.
	sealedAt := time.Unix(int64(binary.BigEndian.Uint64(plain)), 0) //nolint:gosec // G115: autenticado; si diera negativo, solo caduca
	now := time.Now()
	if sealedAt.After(now.Add(clockSkew)) {
		return "", errors.New("cannot open: sealed in the future")
	}
	if maxAge > 0 && now.Sub(sealedAt) > maxAge {
		return "", ErrExpired
	}
	return string(plain[stampBytes:]), nil
}
