package hash

import (
	"errors"
	"fmt"

	"github.com/deltegui/owl/v3/core"
	"golang.org/x/crypto/bcrypt"
)

// DefaultBcryptCost is the cost of a Bcrypt that does not set one: 2^12 rounds.
const DefaultBcryptCost = 12

// maxBcryptPassword is how many bytes of a password bcrypt reads.
const maxBcryptPassword = 72

// Bcrypt is a core.Hasher with bcrypt. Its zero value uses DefaultBcryptCost.
// Passwords are limited to 72 bytes.
type Bcrypt struct {
	// Cost is the work factor (bcrypt.MinCost to bcrypt.MaxCost); each step doubles the time.
	Cost int
}

var _ core.Hasher = Bcrypt{}

// NewBcrypt creates a Bcrypt with the default cost.
func NewBcrypt() Bcrypt { return Bcrypt{Cost: DefaultBcryptCost} }

func (b Bcrypt) cost() int {
	if b.Cost == 0 {
		return DefaultBcryptCost
	}
	return b.Cost
}

// Hash hashes a password with the cost of b.
func (b Bcrypt) Hash(password string) (string, error) {
	if len(password) > maxBcryptPassword {
		return "", ErrPasswordTooLong
	}
	cost := b.cost()
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return "", fmt.Errorf("bcrypt cost %d is out of range (%d to %d)", cost, bcrypt.MinCost, bcrypt.MaxCost)
	}
	raw, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", fmt.Errorf("cannot hash the password: %w", err)
	}
	return string(raw), nil
}

// Check tells if password made encoded. A password over 72 bytes cannot have made it.
func (b Bcrypt) Check(encoded, password string) (bool, error) {
	if len(password) > maxBcryptPassword {
		return false, nil // bcrypt would compare only the first 72 bytes and accept it
	}
	err := bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return false, nil
	}
	return false, fmt.Errorf("%w: %w", ErrInvalidHash, err)
}

// NeedsRehash tells if encoded has another cost than b, or is not a bcrypt hash.
func (b Bcrypt) NeedsRehash(encoded string) bool {
	cost, err := bcrypt.Cost([]byte(encoded))
	return err != nil || cost != b.cost()
}

// Mimic does the work of Check, with no hash.
func (b Bcrypt) Mimic(password string) { _, _ = b.Hash(password) }
