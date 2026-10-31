package outbox

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Status is the state of an email in a Store.
type Status int

const (
	Pending Status = iota
	InProgress
	Sent
	Failed
)

func (s Status) String() string {
	return [...]string{"Pending", "InProgress", "Sent", "Failed"}[s]
}

// Record is an email in a MemoryStore, with what the store knows about it.
type Record struct {
	Item
	Status        Status
	NextAttemptAt time.Time
	ClaimedAt     time.Time
	ClaimedBy     string
	LastError     string
}

// MemoryStore is a Store in memory, for tests and development: nothing
// survives the process. It is safe for concurrent use.
type MemoryStore struct {
	mu      sync.Mutex
	records []*Record
	now     func() time.Time
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore creates a MemoryStore. now is the clock that tells when an
// email was saved, time.Now if nil.
func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{now: now}
}

func (m *MemoryStore) Save(_ context.Context, e Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, &Record{
		Item:          Item{ID: int64(len(m.records) + 1), Entry: cloneEntry(e)},
		NextAttemptAt: m.now(),
	})
	return nil
}

func (m *MemoryStore) ResetStale(_ context.Context, before time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.Status == InProgress && r.ClaimedAt.Before(before) {
			r.Status, r.ClaimedAt, r.ClaimedBy = Pending, time.Time{}, ""
		}
	}
	return nil
}

func (m *MemoryStore) Claim(_ context.Context, worker string, limit int, now time.Time) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var due []*Record
	for _, r := range m.records {
		if r.Status == Pending && !r.NextAttemptAt.After(now) {
			due = append(due, r)
		}
	}
	slices.SortStableFunc(due, func(a, b *Record) int { return a.NextAttemptAt.Compare(b.NextAttemptAt) })
	if len(due) > limit {
		due = due[:limit]
	}
	items := make([]Item, len(due))
	for i, r := range due {
		r.Status, r.ClaimedAt, r.ClaimedBy = InProgress, now, worker
		items[i] = r.Item
		items[i].Entry = cloneEntry(r.Entry)
	}
	return items, nil
}

func (m *MemoryStore) MarkSent(_ context.Context, id int64) error {
	return m.update(id, func(r *Record) { r.Status = Sent })
}

func (m *MemoryStore) MarkFailed(_ context.Context, id int64, cause string, next *time.Time) error {
	return m.update(id, func(r *Record) {
		r.Attempts++
		r.LastError = cause
		r.ClaimedAt, r.ClaimedBy = time.Time{}, ""
		if next == nil {
			r.Status = Failed
			return
		}
		r.Status, r.NextAttemptAt = Pending, *next
	})
}

func (m *MemoryStore) update(id int64, f func(*Record)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id < 1 || id > int64(len(m.records)) {
		return fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	f(m.records[id-1])
	return nil
}

// Snapshot is a copy of every email, in the order they were saved. For tests.
func (m *MemoryStore) Snapshot() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, len(m.records))
	for i, r := range m.records {
		out[i] = *r
		out[i].Entry = cloneEntry(r.Entry)
	}
	return out
}

func cloneEntry(e Entry) Entry {
	e.Payload, e.Envelope = slices.Clone(e.Payload), slices.Clone(e.Envelope)
	return e
}
