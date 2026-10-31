package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNotFound is what a Store returns for a key it does not have.
var ErrNotFound = errors.New("session not found")

// Entry is what a Store keeps about a session.
type Entry struct {
	Key     string // hash of the session id; the id itself is never stored
	User    User
	Created time.Time
	Expires time.Time
}

// Store keeps the sessions. The Manager checks the expiration, so a store may keep
// an expired entry for a while, and it may drop it whenever it wants.
type Store interface {
	// Save creates the entry or replaces the one with the same Key.
	Save(ctx context.Context, entry Entry) error
	// Get returns ErrNotFound if there is no entry.
	Get(ctx context.Context, key string) (Entry, error)
	// Delete removes an entry, and it is not an error if there is none.
	Delete(ctx context.Context, key string) error
	// DeleteUser removes every entry of the user.
	DeleteUser(ctx context.Context, userId int64) error
}

// sweepEvery is how often a MemoryStore looks for expired entries (when it is written).
const sweepEvery = time.Minute

// MemoryStore keeps the sessions in memory. It is safe for concurrent use.
type MemoryStore struct {
	mutex     sync.Mutex
	values    map[string]Entry
	lastSweep time.Time
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{values: make(map[string]Entry), lastSweep: time.Now()}
}

func (s *MemoryStore) Save(_ context.Context, entry Entry) error {
	entry.User = entry.User.clone()
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.values[entry.Key] = entry
	if now := time.Now(); now.Sub(s.lastSweep) > sweepEvery {
		s.lastSweep = now
		for k, e := range s.values {
			if !now.Before(e.Expires) {
				delete(s.values, k)
			}
		}
	}
	return nil
}

func (s *MemoryStore) Get(_ context.Context, key string) (Entry, error) {
	s.mutex.Lock()
	entry, ok := s.values[key]
	s.mutex.Unlock()
	if !ok {
		return Entry{}, ErrNotFound
	}
	entry.User = entry.User.clone()
	return entry, nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mutex.Lock()
	delete(s.values, key)
	s.mutex.Unlock()
	return nil
}

func (s *MemoryStore) DeleteUser(_ context.Context, userId int64) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for k, e := range s.values {
		if e.User.Id == userId {
			delete(s.values, k)
		}
	}
	return nil
}
