package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Field key constants — used when reading/writing via Viper.
const (
	KeyAPIKey    = "api_key"
	KeyProfileID = "profile_id"
	KeyLiveMode  = "live_mode"
	KeyOutput    = "output"

	// Defaults for commonly reused create-command parameters.
	KeyDefaultDescription = "default_description"
	KeyDefaultAmount      = "default_amount"
	KeyDefaultCurrency    = "default_currency"
	KeyDefaultRedirectURL = "default_redirect_url"
	KeyDefaultWebhookURL  = "default_webhook_url"
)

// Config holds all persistent CLI configuration.
type Config struct {
	APIKey    string `mapstructure:"api_key"`
	ProfileID string `mapstructure:"profile_id"`
	LiveMode  bool   `mapstructure:"live_mode"`
	Output    string `mapstructure:"output"`

	// Reusable defaults applied to create commands when the flag is not set.
	DefaultDescription string `mapstructure:"default_description"`
	DefaultAmount      string `mapstructure:"default_amount"`
	DefaultCurrency    string `mapstructure:"default_currency"`
	DefaultRedirectURL string `mapstructure:"default_redirect_url"`
	DefaultWebhookURL  string `mapstructure:"default_webhook_url"`
}

// IsConfigured returns true when an API key is present.
func (c *Config) IsConfigured() bool {
	return c.APIKey != ""
}

// Dir returns the directory that holds the config file, respecting XDG.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mollie-cli"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".config", "mollie-cli"), nil
}

// Path returns the full path to config.toml.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load reads the config file (if present) and returns the resolved Config.
// Missing file is not an error — it simply yields an empty Config.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigType("toml")
	v.SetConfigName("config")

	// Defaults
	v.SetDefault(KeyOutput, "table")
	v.SetDefault(KeyLiveMode, false)

	// Environment variable overrides
	v.SetEnvPrefix("MOLLIE")
	v.AutomaticEnv()
	v.BindEnv(KeyAPIKey, "MOLLIE_API_KEY")     //nolint:errcheck
	v.BindEnv(KeyLiveMode, "MOLLIE_LIVE_MODE") //nolint:errcheck

	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	v.AddConfigPath(dir)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to disk at the canonical path with mode 0600.
func Save(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	path, err := Path()
	if err != nil {
		return err
	}

	v := viper.New()
	v.SetConfigType("toml")
	v.Set(KeyAPIKey, cfg.APIKey)
	v.Set(KeyProfileID, cfg.ProfileID)
	v.Set(KeyLiveMode, cfg.LiveMode)
	v.Set(KeyOutput, cfg.Output)
	v.Set(KeyDefaultDescription, cfg.DefaultDescription)
	v.Set(KeyDefaultAmount, cfg.DefaultAmount)
	v.Set(KeyDefaultCurrency, cfg.DefaultCurrency)
	v.Set(KeyDefaultRedirectURL, cfg.DefaultRedirectURL)
	v.Set(KeyDefaultWebhookURL, cfg.DefaultWebhookURL)

	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	// Restrict permissions to owner read/write only.
	return os.Chmod(path, 0600)
}
