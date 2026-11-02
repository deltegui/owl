package hash

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestBcryptDefaultCostIsTwelve(t *testing.T) {
	if DefaultBcryptCost != 12 || NewBcrypt().Cost != 12 {
		t.Fatalf("the default cost is part of what is stored: %d / %d", DefaultBcryptCost, NewBcrypt().Cost)
	}
	encoded := mustHash(t, Bcrypt{})
	if cost, _ := bcrypt.Cost([]byte(encoded)); cost != 12 {
		t.Fatalf("a zero Cost takes the default: got %d", cost)
	}
}

func TestBcryptCostOutOfRangeSaysSo(t *testing.T) {
	for _, cost := range []int{-1, bcrypt.MinCost - 1, bcrypt.MaxCost + 1} {
		_, err := (Bcrypt{Cost: cost}).Hash("pw")
		if err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("cost %d: %v", cost, err)
		}
	}
	if encoded := mustHash(t, Bcrypt{Cost: bcrypt.MinCost}); encoded == "" {
		t.Error("the minimum cost is valid")
	}
}

func TestBcryptTakesExactly72Bytes(t *testing.T) {
	b := Bcrypt{Cost: bcrypt.MinCost}
	exact := strings.Repeat("a", 72)
	encoded, err := b.Hash(exact)
	if err != nil {
		t.Fatalf("72 bytes fit: %v", err)
	}
	if ok, err := b.Check(encoded, exact); !ok || err != nil {
		t.Fatalf("72 bytes check: %v %v", ok, err)
	}
	if _, err := b.Hash(exact + "a"); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("73 bytes: %v", err)
	}
	// bcrypt would compare only the first 72 bytes and accept this.
	if ok, err := b.Check(encoded, exact+"a"); ok || err != nil {
		t.Fatalf("73 bytes must not match the 72 byte one: %v %v", ok, err)
	}
}

// Mimic is there to take the time of a Check when the user does not exist.
func TestBcryptMimicSpendsTime(t *testing.T) {
	b := Bcrypt{Cost: 10} // tens of milliseconds
	start := time.Now()
	b.Mimic("whatever")
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Fatalf("Mimic took %v: it does not hash", elapsed)
	}
}

func TestArgon2MimicSpendsTime(t *testing.T) {
	a := Argon2{Memory: 32 * 1024, Time: 2, Threads: 1}
	start := time.Now()
	a.Mimic("whatever")
	if elapsed := time.Since(start); elapsed < 5*time.Millisecond {
		t.Fatalf("Mimic took %v: it does not hash", elapsed)
	}
}
