package hash

import (
	"errors"
	"strings"
	"testing"

	"github.com/deltegui/owl/v3/core"
	"golang.org/x/crypto/bcrypt"
)

// Cheap parameters: the tests are about behavior, not about being slow.
func hashers() map[string]core.Hasher {
	return map[string]core.Hasher{
		"argon2": Argon2{Memory: 64, Time: 1, Threads: 1},
		"bcrypt": Bcrypt{Cost: bcrypt.MinCost},
	}
}

func TestHashAndCheck(t *testing.T) {
	for name, h := range hashers() {
		t.Run(name, func(t *testing.T) {
			for _, password := range []string{"correct horse", "", "ñandú ✓", "with\x00nul"} {
				encoded, err := h.Hash(password)
				if err != nil {
					t.Fatalf("%q: %v", password, err)
				}
				if ok, err := h.Check(encoded, password); !ok || err != nil {
					t.Errorf("%q: Check = %v, %v", password, ok, err)
				}
				if ok, err := h.Check(encoded, password+"x"); ok || err != nil {
					t.Errorf("%q: a wrong password is (false, nil), got %v, %v", password, ok, err)
				}
			}
			a, _ := h.Hash("same")
			b, _ := h.Hash("same")
			if a == b {
				t.Error("two hashes of the same password must differ (salt)")
			}
		})
	}
}

func TestMalformedHash(t *testing.T) {
	bad := []string{"", "garbage", "$2a$", "$argon2id$", "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$a2V5"}
	for name, h := range hashers() {
		for _, encoded := range bad {
			if ok, err := h.Check(encoded, "x"); ok || !errors.Is(err, ErrInvalidHash) {
				t.Errorf("%s %q: want ErrInvalidHash, got %v, %v", name, encoded, ok, err)
			}
			if !h.NeedsRehash(encoded) {
				t.Errorf("%s %q: what cannot be read needs a new hash", name, encoded)
			}
		}
	}
	argon := Argon2{Memory: 64, Time: 1, Threads: 1}
	for _, encoded := range []string{
		"$argon2id$v=18$m=64,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=0,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=64,t=1,p=0$c2FsdA$a2V5",
		"$argon2id$v=19$m=64,t=1,p=300$c2FsdA$a2V5",
		"$argon2id$v=19$m=64,t=1,p=1$$a2V5",
		"$argon2id$v=19$m=64,t=1,p=1$c2FsdA$",
		"$argon2id$v=19$m=64,t=1,p=1$***$a2V5",
	} {
		if _, err := argon.Check(encoded, "x"); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("%q: want ErrInvalidHash, got %v", encoded, err)
		}
	}
}

func TestNeedsRehash(t *testing.T) {
	for name, tc := range map[string]struct{ old, new core.Hasher }{
		"argon2": {Argon2{Memory: 64, Time: 1, Threads: 1}, Argon2{Memory: 128, Time: 1, Threads: 1}},
		"bcrypt": {Bcrypt{Cost: 4}, Bcrypt{Cost: 5}},
	} {
		encoded, err := tc.old.Hash("pw")
		if err != nil {
			t.Fatal(err)
		}
		if tc.old.NeedsRehash(encoded) {
			t.Errorf("%s: a hash made with the same parameters does not need it", name)
		}
		if !tc.new.NeedsRehash(encoded) {
			t.Errorf("%s: a hash with other parameters does", name)
		}
		if ok, err := tc.new.Check(encoded, "pw"); !ok || err != nil {
			t.Errorf("%s: it still checks with the other parameters: %v %v", name, ok, err)
		}
	}
	if !(Argon2{}).NeedsRehash("$2a$04$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ01234") {
		t.Error("a hash of another algorithm needs a new one")
	}
}

func TestArgon2Format(t *testing.T) {
	encoded, err := Argon2{Memory: 64, Time: 2, Threads: 1}.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=64,t=2,p=1$") || strings.Count(encoded, "$") != 5 {
		t.Fatalf("not the standard form: %q", encoded)
	}
}

func TestDefaults(t *testing.T) {
	d := NewArgon2()
	if d.Memory != DefaultArgon2Memory || d.Time != DefaultArgon2Time || d.Threads != DefaultArgon2Threads {
		t.Fatalf("defaults: %+v", d)
	}
	if (Argon2{}).NeedsRehash(mustHash(t, d)) {
		t.Fatal("the zero value must be the default")
	}
	if (Bcrypt{}).cost() != DefaultBcryptCost || NewBcrypt().Cost != DefaultBcryptCost {
		t.Fatal("bcrypt default cost")
	}
}

func mustHash(t *testing.T, h core.Hasher) string {
	t.Helper()
	encoded, err := h.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestBcryptCostIsUsed(t *testing.T) {
	encoded := mustHash(t, Bcrypt{Cost: 5})
	if cost, _ := bcrypt.Cost([]byte(encoded)); cost != 5 {
		t.Fatalf("Cost was ignored: got %d", cost)
	}
	for _, cost := range []int{-1, 3, 32} {
		if _, err := (Bcrypt{Cost: cost}).Hash("pw"); err == nil {
			t.Errorf("cost %d must be refused, not changed to another", cost)
		}
	}
}

func TestBcryptLongPassword(t *testing.T) {
	h := Bcrypt{Cost: bcrypt.MinCost}
	if _, err := h.Hash(strings.Repeat("a", 73)); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("want ErrPasswordTooLong, got %v", err)
	}
	encoded, err := h.Hash(strings.Repeat("a", 72))
	if err != nil {
		t.Fatalf("72 bytes is the limit and fits: %v", err)
	}
	if ok, err := h.Check(encoded, strings.Repeat("a", 73)); ok || err != nil {
		t.Errorf("a longer password is just a wrong one: %v %v", ok, err)
	}
	// Argon2 has no such limit.
	long := strings.Repeat("a", 500)
	a := Argon2{Memory: 64, Time: 1, Threads: 1}
	enc, err := a.Hash(long)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.Check(enc, long); !ok {
		t.Error("argon2 must take long passwords")
	}
	if ok, _ := a.Check(enc, long[:499]); ok {
		t.Error("and tell them apart to the last byte")
	}
}

func TestMimicDoesNotPanic(t *testing.T) {
	for _, h := range hashers() {
		h.Mimic("whatever")
		h.Mimic(strings.Repeat("a", 100))
	}
}
