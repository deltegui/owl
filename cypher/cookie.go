package cypher

import (
	"encoding/base64"

	"github.com/deltegui/owl/core"
)

// EncodeCookie is a function that encodes a cookie data using a Cypher implementation.
// Returns a base64 encoded cookie. If cannot encode data will return an error.
func EncodeCookie(cypher core.Cypher, data string) (string, error) {
	bytes, err := cypher.Encrypt([]byte(data))
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// DecodeCookie is a function that decodes a cookie data using a Cypher implementation.
// Returns plain cookie data. If cannot decode or decrypt data will return an error.
func DecodeCookie(cypher core.Cypher, data string) (string, error) {
	bytes, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}
	plaintext, err := cypher.Decrypt(bytes)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
