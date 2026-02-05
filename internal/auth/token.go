package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenResponse is the response from the Google OAuth token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// TokenManager handles token caching and refresh.
type TokenManager struct {
	clientID     string
	clientSecret string

	mu          sync.Mutex
	accessToken string
	expiry      time.Time
}

// NewTokenManager creates a new token manager.
func NewTokenManager(clientID, clientSecret string) *TokenManager {
	return &TokenManager{
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

// ExchangeCode exchanges an authorization code for tokens.
func (tm *TokenManager) ExchangeCode(code, redirectURI string) (*TokenResponse, error) {
	return tm.tokenRequest(url.Values{
		"client_id":     {tm.clientID},
		"client_secret": {tm.clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	})
}

// AccessToken returns a valid access token, refreshing if needed.
func (tm *TokenManager) AccessToken(refreshToken string) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.accessToken != "" && time.Now().Before(tm.expiry.Add(-60*time.Second)) {
		return tm.accessToken, nil
	}

	resp, err := tm.refresh(refreshToken)
	if err != nil {
		return "", err
	}

	tm.accessToken = resp.AccessToken
	tm.expiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	return tm.accessToken, nil
}

func (tm *TokenManager) refresh(refreshToken string) (*TokenResponse, error) {
	return tm.tokenRequest(url.Values{
		"client_id":     {tm.clientID},
		"client_secret": {tm.clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	})
}

func (tm *TokenManager) tokenRequest(params url.Values) (*TokenResponse, error) {
	resp, err := http.Post(googleTokenURL, "application/x-www-form-urlencoded", strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}
	return &tok, nil
}
