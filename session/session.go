package session

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/cypher"
)

type Id string

type User struct {
	Id    int64
	Name  string
	Roles []core.Role
	Image string
}

type Entry struct {
	Id      Id
	User    User
	Timeout time.Time
}

func (entry Entry) IsValid() bool {
	return time.Now().Before(entry.Timeout)
}

type SessionStore interface {
	Save(entry Entry)
	Get(id Id) (Entry, error)
	Delete(id Id)
	Invalidate(userId int64)
	RecollectGarbage()
}

type MemoryStore struct {
	values map[Id]Entry
	mutex  sync.Mutex
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		values: make(map[Id]Entry),
		mutex:  sync.Mutex{},
	}
}

func (store *MemoryStore) Save(entry Entry) {
	store.mutex.Lock()
	store.values[entry.Id] = entry
	store.mutex.Unlock()
}

func (store *MemoryStore) RecollectGarbage() {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for key, entry := range store.values {
		if !entry.IsValid() {
			delete(store.values, key)
		}
	}
}

func (store *MemoryStore) Invalidate(userId int64) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for key, entry := range store.values {
		if entry.User.Id == userId {
			delete(store.values, key)
		}
	}
}

func (store *MemoryStore) Get(id Id) (Entry, error) {
	store.mutex.Lock()
	entry, ok := store.values[id]
	store.mutex.Unlock()

	if !ok {
		return Entry{}, fmt.Errorf("no session entry for id '%s'", id)
	}
	return entry, nil
}

func (store *MemoryStore) Delete(id Id) {
	store.mutex.Lock()
	delete(store.values, id)
	store.mutex.Unlock()
}

type ManagerConfiguration struct {
	// Secure controls cookie "secure" parameter.
	// True means only use it with HTTPS, false use both
	// HTTP and HTTPS.
	Secure bool

	// Invlidate enabled session renew feature. If is enabled
	// invalidates old sessions every time user logins. If it
	// is not enabled old sessions remains valid util expires.
	Invalidate bool
}

type Manager struct {
	store           SessionStore
	timeoutDuration time.Duration
	cypher          core.Cypher
	configuration   ManagerConfiguration
}

func NewManager(store SessionStore, duration time.Duration, cypher core.Cypher, configuration ManagerConfiguration) *Manager {
	return &Manager{
		store:           store,
		timeoutDuration: duration,
		cypher:          cypher,
		configuration:   configuration,
	}
}

func NewManagerWithDefaults(store SessionStore, duration time.Duration, cypher core.Cypher) *Manager {
	config := ManagerConfiguration{
		Secure:     true,
		Invalidate: true,
	}
	return &Manager{
		store:           store,
		timeoutDuration: duration,
		cypher:          cypher,
		configuration:   config,
	}
}

func NewInMemoryManager(duration time.Duration, cypher core.Cypher, configuration ManagerConfiguration) *Manager {
	return NewManager(
		NewMemoryStore(),
		duration,
		cypher,
		configuration)
}

func (manager *Manager) Add(user User) Entry {
	id := manager.createSessionId()
	entry := Entry{
		Id:      id,
		User:    user,
		Timeout: time.Now().Add(manager.timeoutDuration),
	}
	manager.invalidateEntries(user.Id)
	manager.store.Save(entry)
	manager.store.RecollectGarbage()
	return entry
}

func (manager *Manager) createSessionId() Id {
	return Id(core.GenerateTokenDefaultLength())
}

func (manager *Manager) invalidateEntries(userId int64) {
	if manager.configuration.Invalidate {
		manager.store.Invalidate(userId)
	}
}

func (manager *Manager) Get(id Id) (Entry, error) {
	manager.store.RecollectGarbage()
	return manager.store.Get(id)
}

func (manager *Manager) Delete(id Id) {
	manager.store.Delete(id)
}

func (manager *Manager) GetUserIfValid(id Id) (User, error) {
	entry, err := manager.Get(id)
	if err != nil {
		return User{}, err
	}
	if entry.IsValid() {
		return entry.User, nil
	}
	manager.store.Delete(id)
	return User{}, errors.New("expired session")
}

const cookieKey string = "phx_session"

func (manager *Manager) CreateSessionCookie(w http.ResponseWriter, user User) {
	entry := manager.Add(user)
	age := core.OneDayDuration
	encoded, err := cypher.EncodeCookie(manager.cypher, string(entry.Id))
	if err != nil {
		log.Println("Cannot encrypt session cookie:", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieKey,
		Value:    encoded,
		Expires:  time.Now().Add(age),
		MaxAge:   int(age.Seconds()),
		Path:     "/",
		SameSite: http.SameSiteDefaultMode,
		HttpOnly: true,
		Secure:   manager.configuration.Secure,
	})
}

func readSessionId(req *http.Request, cy core.Cypher) (Id, *http.Cookie, error) {
	cookie, err := req.Cookie(cookieKey)
	if err != nil {
		return Id(""), nil, errors.New("no session cookie is present in the request")
	}
	id, err := cypher.DecodeCookie(cy, cookie.Value)
	if err != nil {
		return Id(""), nil, err
	}
	return Id(id), cookie, nil
}

func (manager *Manager) ReadSessionCookie(req *http.Request) (User, error) {
	sessionId, cookie, err := readSessionId(req, manager.cypher)
	if err != nil {
		return User{}, err
	}
	if cookie.Expires.After(time.Now()) {
		return User{}, errors.New("expired session cookie")
	}
	user, err := manager.GetUserIfValid(sessionId)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (manager *Manager) DestroySession(w http.ResponseWriter, req *http.Request) error {
	session, _, err := readSessionId(req, manager.cypher)
	if err != nil {
		return err
	}
	manager.store.Delete(session)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieKey,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   manager.configuration.Secure,
	})
	return nil
}
