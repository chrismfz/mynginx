package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type Session struct {
	Token   string
	UserID  int64
	Username string
	Role    string
	Expires time.Time
}

type SessionStore struct {
	mu   sync.Mutex
	data map[string]Session
	ttl  time.Duration
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		data: map[string]Session{},
		ttl:  ttl,
	}
}

func (s *SessionStore) New(userID int64, username, role string) (Session, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return Session{}, err
	}
	tok := hex.EncodeToString(b)
	sess := Session{
		Token:   tok,
		UserID:  userID,
		Username: username,
		Role:    role,
		Expires: time.Now().Add(s.ttl),
	}
	s.mu.Lock()
	s.data[tok] = sess
	s.mu.Unlock()
	return sess, nil
}

func (s *SessionStore) Get(token string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.data[token]
	if !ok {
		return Session{}, false
	}
	if time.Now().After(sess.Expires) {
		delete(s.data, token)
		return Session{}, false
	}
	return sess, true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.data, token)
	s.mu.Unlock()
}
