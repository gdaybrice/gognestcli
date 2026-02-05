package cmd

import (
	"fmt"
	"strings"

	"github.com/brice/gognestcli/internal/auth"
	"github.com/brice/gognestcli/internal/config"
	"github.com/brice/gognestcli/internal/sdm"
	"github.com/brice/gognestcli/internal/secrets"
)

type DevicesCmd struct{}

func (d *DevicesCmd) Run() error {
	client, _, err := newSDMClient()
	if err != nil {
		return err
	}

	devices, err := client.ListDevices()
	if err != nil {
		return fmt.Errorf("listing devices: %w", err)
	}

	if len(devices) == 0 {
		fmt.Println("No devices found.")
		return nil
	}

	for _, dev := range devices {
		displayName := deviceDisplayName(dev)
		deviceType := shortType(dev.Type)
		fmt.Printf("%-40s  %-20s  %s\n", displayName, deviceType, dev.Name)
	}
	return nil
}

// newSDMClient creates an authenticated SDM client from stored config and secrets.
func newSDMClient() (*sdm.Client, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}

	store, err := secrets.NewStore()
	if err != nil {
		return nil, nil, fmt.Errorf("opening keyring: %w", err)
	}

	refreshToken, err := store.LoadRefreshToken()
	if err != nil {
		return nil, nil, err
	}

	tm := auth.NewTokenManager(cfg.ClientID, cfg.ClientSecret)
	tokenFn := func() (string, error) {
		return tm.AccessToken(refreshToken)
	}

	return sdm.NewClient(cfg.ProjectID, tokenFn), cfg, nil
}

func deviceDisplayName(dev sdm.Device) string {
	for _, rel := range dev.ParentRelations {
		if rel.DisplayName != "" {
			return rel.DisplayName
		}
	}
	parts := strings.Split(dev.Name, "/")
	return parts[len(parts)-1]
}

func shortType(t string) string {
	// e.g. "sdm.devices.types.CAMERA" → "CAMERA"
	parts := strings.Split(t, ".")
	return parts[len(parts)-1]
}
