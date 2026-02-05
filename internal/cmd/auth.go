package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/brice/gognestcli/internal/auth"
	"github.com/brice/gognestcli/internal/config"
	"github.com/brice/gognestcli/internal/secrets"
)

type AuthCmd struct {
	Manual bool `help:"Use manual paste flow instead of browser callback" default:"false"`
}

func (a *AuthCmd) Run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	if cfg.ClientID == "" {
		cfg.ClientID, err = prompt(reader, "Client ID")
		if err != nil {
			return err
		}
	}
	if cfg.ClientSecret == "" {
		cfg.ClientSecret, err = prompt(reader, "Client Secret")
		if err != nil {
			return err
		}
	}
	if cfg.ProjectID == "" {
		cfg.ProjectID, err = prompt(reader, "SDM Project ID")
		if err != nil {
			return err
		}
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Println("Config saved.")

	var code string
	var redirectURI string

	if !a.Manual {
		fmt.Printf("\nMake sure this redirect URI is registered in Google Cloud Console:\n")
		fmt.Printf("  %s\n", auth.DefaultRedirect)
		fmt.Printf("  (APIs & Services → Credentials → OAuth 2.0 Client → Authorized redirect URIs)\n\n")
	}

	if a.Manual {
		redirectURI = "https://www.google.com"
		code, err = auth.ManualFlow(cfg.ClientID, cfg.ProjectID)
		if err != nil {
			return fmt.Errorf("manual auth flow: %w", err)
		}
	} else {
		ctx := context.Background()
		code, redirectURI, err = auth.BrowserFlow(ctx, cfg.ClientID, cfg.ProjectID)
		if err != nil {
			return fmt.Errorf("browser auth flow: %w", err)
		}
	}

	tm := auth.NewTokenManager(cfg.ClientID, cfg.ClientSecret)
	tok, err := tm.ExchangeCode(code, redirectURI)
	if err != nil {
		return fmt.Errorf("exchanging auth code: %w", err)
	}

	store, err := secrets.NewStore()
	if err != nil {
		return fmt.Errorf("opening keyring: %w", err)
	}

	if tok.RefreshToken != "" {
		if err := store.SaveRefreshToken(tok.RefreshToken); err != nil {
			return fmt.Errorf("saving refresh token: %w", err)
		}
		fmt.Println("Refresh token saved to OS keyring.")
	}

	fmt.Println("Authentication successful!")
	return nil
}

func prompt(reader *bufio.Reader, label string) (string, error) {
	fmt.Printf("%s: ", label)
	val, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return "", fmt.Errorf("%s cannot be empty", label)
	}
	return val, nil
}
