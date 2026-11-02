package hash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/deltegui/owl/v3/core"
	"golang.org/x/crypto/argon2"
)

// What an Argon2 that does not set a parameter uses: 64 MiB, 3 passes, 2 threads, a
// salt of 16 bytes and a key of 32.
const (
	DefaultArgon2Memory  uint32 = 64 * 1024
	DefaultArgon2Time    uint32 = 3
	DefaultArgon2Threads uint8  = 2
	defaultSaltBytes     uint32 = 16
	defaultKeyBytes      uint32 = 32
)

// Check uses the parameters the hash says it was made with, so a hash that was
// written by someone else could ask for terabytes of RAM or hours of CPU. A hash that
// asks for more than this is not read. They are well above what anyone uses (RFC 9106
// recommends at most 2 GiB; 64 MiB and 3 passes are the defaults here).
const (
	maxArgon2Memory uint32 = 1 << 20 // KiB: 1 GiB
	maxArgon2Time   uint32 = 32
)

// Argon2 is a core.Hasher with Argon2id. A zero field takes its default, so the zero
// value works. Every hash made takes Memory of RAM while it runs: if many logins can
// be at once, limit them, or a flood of requests uses up the memory.
//
// The hash is the standard text form: $argon2id$v=19$m=65536,t=3,p=2$salt$key.
type Argon2 struct {
	Memory  uint32 // in KiB
	Time    uint32 // passes
	Threads uint8
	// SaltBytes and KeyBytes are the sizes of the salt and of the key.
	SaltBytes uint32
	KeyBytes  uint32
}

var _ core.Hasher = Argon2{}

// NewArgon2 creates an Argon2 with the default parameters.
func NewArgon2() Argon2 { return Argon2{}.withDefaults() }

func (a Argon2) withDefaults() Argon2 {
	if a.Memory == 0 {
		a.Memory = DefaultArgon2Memory
	}
	if a.Time == 0 {
		a.Time = DefaultArgon2Time
	}
	if a.Threads == 0 {
		a.Threads = DefaultArgon2Threads
	}
	if a.SaltBytes == 0 {
		a.SaltBytes = defaultSaltBytes
	}
	if a.KeyBytes == 0 {
		a.KeyBytes = defaultKeyBytes
	}
	return a
}

var b64 = base64.RawStdEncoding

// Hash hashes a password with a new random salt.
func (a Argon2) Hash(password string) (string, error) {
	a = a.withDefaults()
	salt := make([]byte, a.SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("cannot make a salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, a.Time, a.Memory, a.Threads, a.KeyBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, a.Memory, a.Time, a.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// parsed is what a hash says about itself.
type parsed struct {
	params    Argon2
	salt, key []byte
}

func parse(encoded string) (parsed, error) {
	invalid := func(why string) (parsed, error) { return parsed{}, fmt.Errorf("%w: %s", ErrInvalidHash, why) }
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return invalid("not an argon2id hash")
	}
	var version int
	if n, err := fmt.Sscanf(parts[2], "v=%d", &version); n != 1 || err != nil || version != argon2.Version {
		return invalid("unknown version")
	}
	var p parsed
	var threads uint32
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.params.Memory, &p.params.Time, &threads); n != 3 || err != nil {
		return invalid("bad parameters")
	}
	if p.params.Memory == 0 || p.params.Time == 0 || threads == 0 || threads > 255 {
		return invalid("bad parameters")
	}
	if p.params.Memory > maxArgon2Memory || p.params.Time > maxArgon2Time {
		return invalid("parameters too large")
	}
	p.params.Threads = uint8(threads)
	var err error
	if p.salt, err = b64.DecodeString(parts[4]); err != nil || len(p.salt) == 0 {
		return invalid("bad salt")
	}
	if p.key, err = b64.DecodeString(parts[5]); err != nil || len(p.key) == 0 {
		return invalid("bad key")
	}
	// Solo desbordaría con más de 4 GiB de sal o de clave, que salen de un hash guardado.
	p.params.SaltBytes, p.params.KeyBytes = uint32(len(p.salt)), uint32(len(p.key)) //nolint:gosec // G115: ver arriba
	return p, nil
}

// Check tells if password made encoded, whatever parameters it was made with.
func (a Argon2) Check(encoded, password string) (bool, error) {
	p, err := parse(encoded)
	if err != nil {
		return false, err
	}
	key := argon2.IDKey([]byte(password), p.salt, p.params.Time, p.params.Memory, p.params.Threads, p.params.KeyBytes)
	return subtle.ConstantTimeCompare(key, p.key) == 1, nil
}

// NeedsRehash tells if encoded was made with other parameters than a, or cannot be read.
func (a Argon2) NeedsRehash(encoded string) bool {
	p, err := parse(encoded)
	return err != nil || p.params != a.withDefaults()
}

// Mimic does the work of Check, with no hash.
func (a Argon2) Mimic(password string) { _, _ = a.Hash(password) }
