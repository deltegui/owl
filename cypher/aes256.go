package cypher

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/deltegui/owl/core"
)

// AES256 is an implementation of the interface core.Cypher using
// the AES256 symmetric algorithm.
type AES256 struct {
	cipher cipher.AEAD
}

// Generates a random password to use with AES256.
func GenerateRandomPass() ([]byte, error) {
	bytes := make([]byte, core.Size32) // generate a random 32 byte key for AES-256
	if _, err := rand.Read(bytes); err != nil {
		return bytes, fmt.Errorf("failed to generate random key for AES: %w", err)
	}
	return bytes, nil
}

// Generates a random password as string to use with AES256.
func GenerateRandomPassAsString() (string, error) {
	bytes, err := GenerateRandomPass()
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(bytes), nil
}

func generateCipher(pass []byte) cipher.AEAD {
	if len(pass) != core.Size32 {
		log.Panicln("The AES256 encrypt password must be 32 bit long")
	}
	aes, err := aes.NewCipher(pass)
	if err != nil {
		log.Panicln("Cannot create cipher for AES256", err)
	}
	gcm, err := cipher.NewGCM(aes)
	if err != nil {
		log.Panicln("Cannot create CGM:", err)
	}
	return gcm
}

// Creates a Cypher with a random password. Everytime you create a cypher
// will generate a new random password. Keep in mind that anything encrypted
// with other instances will not be decrypted because they dont share passwords.
// Will panic if it cannot generate a random password.
func New() core.Cypher {
	bytes, err := GenerateRandomPass()
	if err != nil {
		log.Panicln("Cannot create cypher: ", err)
	}
	return AES256{
		cipher: generateCipher(bytes),
	}
}

// Creates a Cypher with a provided password as bytes
func NewWithPassword(password []byte) core.Cypher {
	return AES256{
		cipher: generateCipher(password),
	}
}

// Creates a Cypher with a provided password as string
func NewWithPasswordAsString(password string) core.Cypher {
	bytes, err := base64.RawStdEncoding.DecodeString(password)
	if err != nil {
		log.Panicln("Cannot decode password for cypher:", err)
	}
	return NewWithPassword(bytes)
}

// Encrypt bytes using AES256. Will return an error if cannot encrypt.
func (aes AES256) Encrypt(data []byte) ([]byte, error) {
	nonce := make([]byte, aes.cipher.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("cannot encrypt using AES256: %w", err)
	}
	dst := aes.cipher.Seal(nonce, nonce, data, nil)
	return dst, nil
}

// Decrypt bytes using AES256. Will return an error if cannot decrypt information.
func (aes AES256) Decrypt(data []byte) ([]byte, error) {
	nonceSize := aes.cipher.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("malformed AES256 encryted data")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aes.cipher.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot decrypt AES256: %w", err)
	}
	return plaintext, nil
}
