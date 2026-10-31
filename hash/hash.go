// Package hash has the password Hashers: Argon2 (the one to use) and Bcrypt.
package hash

import "errors"

// ErrInvalidHash is what Check returns for something that is not a hash this Hasher can read.
var ErrInvalidHash = errors.New("invalid hash")

// ErrPasswordTooLong is what Bcrypt.Hash returns for a password over 72 bytes, which is
// all that bcrypt reads. Use Argon2 if long passphrases are allowed.
var ErrPasswordTooLong = errors.New("password is longer than 72 bytes")
