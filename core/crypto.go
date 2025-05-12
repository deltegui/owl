package core

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"math/big"
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

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+"

func GenerateRandomPassword(length int) string {
	password := make([]byte, length)
	charsetLength := big.NewInt(int64(len(charset)))
	for i := range password {
		index, err := rand.Int(rand.Reader, charsetLength)
		if err != nil {
			log.Panicf("Error generating random password: cannot generate random index: %v", err)
		}
		password[i] = charset[index.Int64()]
	}

	return string(password)
}
