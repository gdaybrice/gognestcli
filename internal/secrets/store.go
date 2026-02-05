package secrets

import (
	"errors"

	"github.com/99designs/keyring"
)

const (
	serviceName     = "gognestcli"
	refreshTokenKey = "refresh_token"
)

// Store provides access to the OS keyring for secure token storage.
type Store struct {
	ring keyring.Keyring
}

// NewStore creates a new keyring-backed secret store.
func NewStore() (*Store, error) {
	ring, err := keyring.Open(keyring.Config{
		ServiceName: serviceName,
		// macOS Keychain is used automatically on Darwin.
		// On Linux, SecretService or encrypted file fallback.
		KeychainTrustApplication: true,
	})
	if err != nil {
		return nil, err
	}
	return &Store{ring: ring}, nil
}

// SaveRefreshToken stores the refresh token in the OS keyring.
func (s *Store) SaveRefreshToken(token string) error {
	return s.ring.Set(keyring.Item{
		Key:  refreshTokenKey,
		Data: []byte(token),
	})
}

// LoadRefreshToken retrieves the refresh token from the OS keyring.
func (s *Store) LoadRefreshToken() (string, error) {
	item, err := s.ring.Get(refreshTokenKey)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", errors.New("no refresh token found (run: gognestcli auth)")
		}
		return "", err
	}
	return string(item.Data), nil
}

// DeleteRefreshToken removes the refresh token from the OS keyring.
func (s *Store) DeleteRefreshToken() error {
	return s.ring.Remove(refreshTokenKey)
}
