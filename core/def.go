package core

import "time"

const (
	Size64 int = 64
	Size32 int = 32
	Size16 int = 16
	Size8  int = 8
)

const (
	IntBase10 int = 10
)

const OneDayDuration time.Duration = 24 * time.Hour

type Hasher interface {
	Hash(value string) string
	Check(a, b string) bool
}

type Cypher interface {
	Encrypt(data []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

type Role int64

const (
	RoleAdmin Role = 1
	RoleUser  Role = 2
)

type ValidationError interface {
	Error() string
	Format(f string) string
	GetName() string
}

type Validator func(interface{}) map[string][]ValidationError
