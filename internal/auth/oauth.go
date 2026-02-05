package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

const (
	googleAuthURL    = "https://nestservices.google.com/partnerconnections"
	googleTokenURL   = "https://oauth2.googleapis.com/token"
	sdmScope         = "https://www.googleapis.com/auth/sdm.service https://www.googleapis.com/auth/pubsub"
	DefaultPort      = 9004
	DefaultRedirect  = "http://localhost:9004/callback"
)

// AuthCodeResult is returned from the OAuth callback.
type AuthCodeResult struct {
	Code string
	Err  error
}

// BuildAuthURL constructs the Google OAuth authorization URL.
func BuildAuthURL(clientID, redirectURI, projectID string) string {
	params := url.Values{
		"redirect_uri":  {redirectURI},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"client_id":     {clientID},
		"response_type": {"code"},
		"scope":         {sdmScope},
	}
	return fmt.Sprintf("%s/%s/auth?%s", googleAuthURL, projectID, params.Encode())
}

// BrowserFlow starts a local HTTP server on a fixed port, opens the browser
// for OAuth, and waits for the callback with the auth code.
//
// The redirect URI http://localhost:9004/callback must be registered in your
// Google Cloud Console under APIs & Services → Credentials → OAuth 2.0 Client.
func BrowserFlow(ctx context.Context, clientID, projectID string) (code string, redirectURI string, err error) {
	addr := fmt.Sprintf("localhost:%d", DefaultPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", "", fmt.Errorf("failed to listen on %s (is another instance running?): %w", addr, err)
	}
	defer listener.Close()

	redirectURI = DefaultRedirect
	authURL := BuildAuthURL(clientID, redirectURI, projectID)

	resultCh := make(chan AuthCodeResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no code in callback"
			}
			resultCh <- AuthCodeResult{Err: fmt.Errorf("oauth error: %s", errMsg)}
			fmt.Fprint(w, "<html><body><h1>Authentication failed</h1><p>You can close this window.</p></body></html>")
			return
		}
		resultCh <- AuthCodeResult{Code: code}
		fmt.Fprint(w, "<html><body><h1>Authentication successful!</h1><p>You can close this window.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer server.Shutdown(ctx)

	fmt.Printf("Opening browser for authentication...\n")
	if err := openBrowser(authURL); err != nil {
		fmt.Printf("Could not open browser. Please visit:\n%s\n", authURL)
	}

	select {
	case result := <-resultCh:
		return result.Code, redirectURI, result.Err
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

// ManualFlow prints the auth URL and prompts the user to paste the redirect URL.
func ManualFlow(clientID, projectID string) (code string, err error) {
	redirectURI := "https://www.google.com"
	authURL := BuildAuthURL(clientID, redirectURI, projectID)

	fmt.Printf("Visit this URL in your browser:\n\n%s\n\n", authURL)
	fmt.Printf("After authorizing, paste the full redirect URL here: ")

	var redirectURL string
	if _, err := fmt.Scanln(&redirectURL); err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	parsed, err := url.Parse(strings.TrimSpace(redirectURL))
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	code = parsed.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("no code parameter found in URL")
	}
	return code, nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
