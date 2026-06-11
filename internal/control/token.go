package control

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTokenExpired = errors.New("token expired")
	ErrTokenUsed    = errors.New("token exhausted")
)

func NewRandomToken(prefix string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func (s *Store) CreateJoinToken(ttl time.Duration, uses int) (*JoinToken, error) {
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	if uses <= 0 {
		uses = 1
	}
	tokenValue, err := NewRandomToken("join")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	token := &JoinToken{
		Token:     tokenValue,
		UsesLeft:  uses,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
		UsedBy:    []string{},
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Tokens[tokenValue] = token
	s.addEventLocked("token.created", "join token created", "", "", "", map[string]string{"uses": fmt.Sprint(uses)}, now)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	copyValue := *token
	return &copyValue, nil
}

func (s *Store) consumeJoinTokenLocked(tokenValue, nodeID string, now time.Time) error {
	token, ok := s.state.Tokens[tokenValue]
	if !ok {
		return ErrInvalidToken
	}
	if !now.Before(token.ExpiresAt) {
		delete(s.state.Tokens, tokenValue)
		return ErrTokenExpired
	}
	if token.UsesLeft <= 0 {
		return ErrTokenUsed
	}
	token.UsesLeft--
	token.UsedBy = append(token.UsedBy, nodeID)
	return nil
}
