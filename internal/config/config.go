package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const configFile = "config.json"

// Config holds the application configuration persisted to disk.
type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	ProjectID    string `json:"project_id"`
	DeviceID     string `json:"device_id,omitempty"`
	PubSubSub    string `json:"pubsub_subscription,omitempty"`
}

// Load reads the config from the config directory. Returns an empty config if
// the file doesn't exist.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save writes the config to the config directory.
func (c *Config) Save() error {
	dir, err := EnsureDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, configFile), data, 0600)
}

// Validate checks that required fields are present.
func (c *Config) Validate() error {
	if c.ClientID == "" {
		return errors.New("client_id not configured (run: gognestcli auth)")
	}
	if c.ClientSecret == "" {
		return errors.New("client_secret not configured (run: gognestcli auth)")
	}
	if c.ProjectID == "" {
		return errors.New("project_id not configured (run: gognestcli auth)")
	}
	return nil
}
