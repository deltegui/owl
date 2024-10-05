package hash

import (
	"log"

	"golang.org/x/crypto/bcrypt"
)

// BcryptHasher is an implementation of a users Hasher that use Bcrypt.
type BcryptHasher struct{}

// Hash a password using bcrypt and returns the result.
func (hasher BcryptHasher) Hash(password string) string {
	rawResult, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	return string(rawResult)
}

// CheckHashPassword compares users hashed password and a raw password and returns if are the same or not.
func (hasher BcryptHasher) Check(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
