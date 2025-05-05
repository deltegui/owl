package core

import (
	"crypto/rand"
	"encoding/base64"
	"log"
)

const DefaultTokenBytes int = Size32

func GenerateToken(numberBytes int) string {
	b := make([]byte, numberBytes)
	if _, err := rand.Read(b); err != nil {
		log.Panicln("Error while generating random token: ", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func GenerateTokenDefaultLength() string {
	return GenerateToken(DefaultTokenBytes)
}
