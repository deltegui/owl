package hash

import (
	"golang.org/x/crypto/bcrypt"
)

// BcryptHasher is an implementation of a users Hasher that use Bcrypt.
type BcryptHasher struct {
	Cost int
}

const DefaultCost int = 12

func NewBcryptHasher() BcryptHasher {
	return BcryptHasher{
		Cost: DefaultCost,
	}
}

// Hash a password using bcrypt and returns the result.
func (hasher BcryptHasher) Hash(password string) (string, error) {
	rawResult, err := bcrypt.GenerateFromPassword([]byte(password), DefaultCost)
	if err != nil {
		return "", err
	}
	return string(rawResult), nil
}

// CheckHashPassword compares users hashed password and a raw password and returns if are the same or not.
func (hasher BcryptHasher) Check(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
