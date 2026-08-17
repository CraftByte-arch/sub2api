package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

const (
	defaultLoginChallengeTTL = 5 * time.Minute
	maxLoginChallenges       = 256
	loginChallengeRandomSize = 16
)

var (
	errLoginChallengeUnavailable = errors.New("login challenge unavailable")
	errLoginChallengeCapacity    = errors.New("too many active login challenges")
)

type loginChallengeScope struct {
	AdminID       int64
	UpstreamID    string
	IdentityID    string
	ManagementURL string
}

type loginChallengeAttempt struct {
	Scope     loginChallengeScope
	CaptchaID string
	Cookie    string
	Provider  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type loginChallengeHandle struct {
	ID        string
	Provider  string
	ExpiresAt time.Time
}

type loginChallengeStore struct {
	mu       sync.Mutex
	attempts map[string]loginChallengeAttempt
	ttl      time.Duration
	now      func() time.Time
	random   io.Reader
}

func newLoginChallengeStore(ttl time.Duration) *loginChallengeStore {
	if ttl <= 0 || ttl > defaultLoginChallengeTTL {
		ttl = defaultLoginChallengeTTL
	}
	return &loginChallengeStore{
		attempts: map[string]loginChallengeAttempt{},
		ttl:      ttl,
		now:      time.Now,
		random:   rand.Reader,
	}
}

func (s *loginChallengeStore) Create(scope loginChallengeScope, challenge upstream.LoginChallenge) (loginChallengeHandle, error) {
	if s == nil || !validLoginChallengeScope(scope) || !challenge.Required || strings.TrimSpace(challenge.CaptchaID) == "" {
		return loginChallengeHandle{}, errLoginChallengeUnavailable
	}
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if len(s.attempts) >= maxLoginChallenges {
		return loginChallengeHandle{}, errLoginChallengeCapacity
	}
	id, err := s.randomIDLocked()
	if err != nil {
		return loginChallengeHandle{}, err
	}
	expiresAt := now.Add(s.ttl)
	s.attempts[id] = loginChallengeAttempt{
		Scope:     normalizedLoginChallengeScope(scope),
		CaptchaID: challenge.CaptchaID,
		Cookie:    challenge.Cookie,
		Provider:  strings.TrimSpace(challenge.Provider),
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	return loginChallengeHandle{ID: id, Provider: strings.TrimSpace(challenge.Provider), ExpiresAt: expiresAt}, nil
}

func (s *loginChallengeStore) Consume(id string, scope loginChallengeScope) (loginChallengeAttempt, error) {
	if s == nil || !validLoginChallengeScope(scope) {
		return loginChallengeAttempt{}, errLoginChallengeUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n\x00") {
		return loginChallengeAttempt{}, errLoginChallengeUnavailable
	}
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	attempt, ok := s.attempts[id]
	if !ok || !sameLoginChallengeScope(attempt.Scope, scope) {
		return loginChallengeAttempt{}, errLoginChallengeUnavailable
	}
	delete(s.attempts, id)
	return attempt, nil
}

func (s *loginChallengeStore) pruneLocked(now time.Time) {
	for id, attempt := range s.attempts {
		if !attempt.ExpiresAt.After(now) {
			delete(s.attempts, id)
		}
	}
}

func (s *loginChallengeStore) randomIDLocked() (string, error) {
	for attempts := 0; attempts < 4; attempts++ {
		raw := make([]byte, loginChallengeRandomSize)
		if _, err := io.ReadFull(s.random, raw); err != nil {
			return "", err
		}
		id := "login_challenge_" + hex.EncodeToString(raw)
		if _, exists := s.attempts[id]; !exists {
			return id, nil
		}
	}
	return "", errors.New("failed to allocate login challenge id")
}

func validLoginChallengeScope(scope loginChallengeScope) bool {
	return scope.AdminID > 0 &&
		strings.TrimSpace(scope.UpstreamID) != "" &&
		strings.TrimSpace(scope.ManagementURL) != ""
}

func normalizedLoginChallengeScope(scope loginChallengeScope) loginChallengeScope {
	scope.UpstreamID = strings.TrimSpace(scope.UpstreamID)
	scope.IdentityID = strings.TrimSpace(scope.IdentityID)
	scope.ManagementURL = strings.TrimSpace(scope.ManagementURL)
	return scope
}

func sameLoginChallengeScope(left, right loginChallengeScope) bool {
	left = normalizedLoginChallengeScope(left)
	right = normalizedLoginChallengeScope(right)
	return left.AdminID == right.AdminID &&
		left.UpstreamID == right.UpstreamID &&
		left.IdentityID == right.IdentityID &&
		left.ManagementURL == right.ManagementURL
}
