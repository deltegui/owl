package hash

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// What a zero Argon2 writes is stored in databases: the numbers must not drift.
func TestArgon2DefaultsAreTheDocumentedNumbers(t *testing.T) {
	encoded := mustHash(t, Argon2{})
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("defaults changed: %s", encoded)
	}
	parts := strings.Split(encoded, "$")
	salt, _ := base64.RawStdEncoding.DecodeString(parts[4])
	key, _ := base64.RawStdEncoding.DecodeString(parts[5])
	if len(salt) != 16 || len(key) != 32 {
		t.Fatalf("salt %d bytes, key %d bytes", len(salt), len(key))
	}
	if DefaultArgon2Memory != 65536 || DefaultArgon2Time != 3 || DefaultArgon2Threads != 2 {
		t.Fatal("the exported defaults changed")
	}
}

func TestArgon2ParseSaysWhatIsWrong(t *testing.T) {
	const salt, key = "c2FsdHNhbHRzYWx0c2FsdA", "a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5"
	hash := func(params, s, k string) string { return "$argon2id$v=19$" + params + "$" + s + "$" + k }
	good := hash("m=64,t=1,p=1", salt, key)
	if _, err := parse(good); err != nil {
		t.Fatalf("the reference hash must parse: %v", err)
	}
	cases := map[string]struct{ encoded, why string }{
		"leading garbage": {"x" + good, "not an argon2id hash"},
		"other algorithm": {strings.Replace(good, "argon2id", "argon2i", 1), "not an argon2id hash"},
		"too few parts":   {"$argon2id$v=19$m=64,t=1,p=1$" + salt, "not an argon2id hash"},
		"too many parts":  {good + "$extra", "not an argon2id hash"},
		"other version":   {strings.Replace(good, "v=19", "v=16", 1), "unknown version"},
		"no version":      {strings.Replace(good, "v=19", "x", 1), "unknown version"},
		"parameters text": {hash("m=a,t=1,p=1", salt, key), "bad parameters"},
		"missing p":       {hash("m=64,t=1", salt, key), "bad parameters"},
		"zero memory":     {hash("m=0,t=1,p=1", salt, key), "bad parameters"},
		"zero passes":     {hash("m=64,t=0,p=1", salt, key), "bad parameters"},
		"zero threads":    {hash("m=64,t=1,p=0", salt, key), "bad parameters"},
		"256 threads":     {hash("m=64,t=1,p=256", salt, key), "bad parameters"},
		"salt not base64": {hash("m=64,t=1,p=1", "***", key), "bad salt"},
		"empty salt":      {hash("m=64,t=1,p=1", "", key), "bad salt"},
		"key not base64":  {hash("m=64,t=1,p=1", salt, "***"), "bad key"},
		"empty key":       {hash("m=64,t=1,p=1", salt, ""), "bad key"},
		"too much memory": {hash("m=1048577,t=1,p=1", salt, key), "too large"},
		"too many passes": {hash("m=64,t=33,p=1", salt, key), "too large"},
	}
	for name, tc := range cases {
		_, err := parse(tc.encoded)
		if !errors.Is(err, ErrInvalidHash) || !strings.Contains(err.Error(), tc.why) {
			t.Errorf("%s: want ErrInvalidHash (%s), got %v", name, tc.why, err)
		}
	}
	if p, err := parse(hash("m=64,t=1,p=255", salt, key)); err != nil || p.params.Threads != 255 {
		t.Errorf("255 threads is the most a hash can ask for: %v", err)
	}
}
