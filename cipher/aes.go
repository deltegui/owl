// Package cipher seals small values (cookies, tokens) with AES-256-GCM.
//
// A value is sealed for a purpose, which GCM authenticates as additional data:
// what was sealed for one purpose (the session cookie) cannot be opened as
// another (the language cookie), even though they share the key. See Seal and Open.
package cipher

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/deltegui/owl/v3/core"
)

// keyBytes is the size of an AES-256 key.
const keyBytes = core.Size32

type key struct {
	id   byte // first byte of the SHA-256 of the key, to tell the keys apart
	aead cipher.AEAD
}

// AES256 is a core.Cipher with AES-256-GCM. It holds a list of keys: it always
// encrypts with the first one and opens with the one that sealed the data, so a
// key can be replaced without logging everybody out (put the new key first and
// keep the old one until the data sealed with it expires).
type AES256 struct {
	keys []key
}

// GenerateKey makes a random key to use with AES256.
func GenerateKey() ([]byte, error) {
	bytes := make([]byte, keyBytes)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("cannot generate a random key: %w", err)
	}
	return bytes, nil
}

// GenerateKeyString makes a random key and encodes it as NewFromStrings reads it.
func GenerateKeyString() (string, error) {
	bytes, err := GenerateKey()
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(bytes), nil
}

// New creates an AES256 with a random key. Everything sealed with it can only be
// opened by the same instance, so it does not survive a restart.
func New() (*AES256, error) {
	k, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	return NewWithKeys(k)
}

// NewWithKeys creates an AES256 from keys of 32 bytes. The first one encrypts;
// the rest only open data sealed before.
func NewWithKeys(keys ...[]byte) (*AES256, error) {
	if len(keys) == 0 {
		return nil, errors.New("cipher: at least one key is needed")
	}
	a := &AES256{}
	for i, raw := range keys {
		if len(raw) != keyBytes {
			return nil, fmt.Errorf("cipher: key %d is %d bytes long, it must be %d", i, len(raw), keyBytes)
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return nil, fmt.Errorf("cipher: key %d: %w", i, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("cipher: key %d: %w", i, err)
		}
		sum := sha256.Sum256(raw)
		a.keys = append(a.keys, key{id: sum[0], aead: aead})
	}
	return a, nil
}

// NewFromStrings is NewWithKeys with the keys in raw standard base64 (see GenerateKeyString).
func NewFromStrings(keys ...string) (*AES256, error) {
	raw := make([][]byte, len(keys))
	for i, s := range keys {
		b, err := base64.RawStdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("cipher: key %d is not base64: %w", i, err)
		}
		raw[i] = b
	}
	return NewWithKeys(raw...)
}

// Encrypt seals data with the first key. aad is authenticated but not stored:
// Decrypt needs the same one. The result is keyID, nonce and ciphertext.
func (a *AES256) Encrypt(data, aad []byte) ([]byte, error) {
	k := a.keys[0]
	out := make([]byte, 1, 1+k.aead.NonceSize()+len(data)+k.aead.Overhead())
	out[0] = k.id
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("cannot encrypt using AES256: %w", err)
	}
	out = append(out, nonce...)
	return k.aead.Seal(out, nonce, data, aad), nil
}

// Decrypt opens what Encrypt sealed with the same aad, with any of the keys.
func (a *AES256) Decrypt(data, aad []byte) ([]byte, error) {
	if len(data) < 1 {
		return nil, errors.New("malformed AES256 encrypted data")
	}
	id, data := data[0], data[1:]
	err := errors.New("cannot decrypt AES256: unknown key")
	for _, k := range a.keys {
		if k.id != id {
			continue
		}
		size := k.aead.NonceSize()
		if len(data) < size {
			return nil, errors.New("malformed AES256 encrypted data")
		}
		plain, openErr := k.aead.Open(nil, data[:size], data[size:], aad)
		if openErr == nil {
			return plain, nil
		}
		err = fmt.Errorf("cannot decrypt AES256: %w", openErr)
	}
	return nil, err
}
