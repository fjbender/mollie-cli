package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Field key constants — kept for reference by other packages.
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

	// DefaultEnvironmentName is the name of the auto-created first environment.
	DefaultEnvironmentName = "default"
)

// currentEnvOverride optionally overrides which environment Load() and Save()
// operate on for the lifetime of the process. Set via SetCurrentEnv().
var currentEnvOverride string

// SetCurrentEnv makes Load() and Save() target the named environment instead
// of the file's active_environment pointer. Pass "" to clear the override.
func SetCurrentEnv(name string) {
	currentEnvOverride = name
}

// Config holds all persistent configuration for a single named environment.
type Config struct {
	APIKey    string `toml:"api_key"    mapstructure:"api_key"`
	ProfileID string `toml:"profile_id" mapstructure:"profile_id"`
	LiveMode  bool   `toml:"live_mode"  mapstructure:"live_mode"`
	Output    string `toml:"output"     mapstructure:"output"`

	// Reusable defaults applied to create commands when the flag is not set.
	DefaultDescription string `toml:"default_description,omitempty" mapstructure:"default_description"`
	DefaultAmount      string `toml:"default_amount,omitempty"      mapstructure:"default_amount"`
	DefaultCurrency    string `toml:"default_currency,omitempty"    mapstructure:"default_currency"`
	DefaultRedirectURL string `toml:"default_redirect_url,omitempty" mapstructure:"default_redirect_url"`
	DefaultWebhookURL  string `toml:"default_webhook_url,omitempty"  mapstructure:"default_webhook_url"`
}

// IsConfigured returns true when an API key is present.
func (c *Config) IsConfigured() bool {
	return c.APIKey != ""
}

// IsAPIKey reports whether key is a profile-scoped API key (prefixed with
// "test_" or "live_") rather than an Organization Access Token ("access_").
func IsAPIKey(key string) bool {
	return strings.HasPrefix(key, "test_") || strings.HasPrefix(key, "live_")
}

// IsLiveAPIKey reports whether key is a live-mode API key (prefixed with "live_").
func IsLiveAPIKey(key string) bool {
	return strings.HasPrefix(key, "live_")
}

// ConfigFile is the complete on-disk representation of the config file,
// containing all named environments and the active-environment pointer.
type ConfigFile struct {
	ActiveEnvironment string             `toml:"active_environment"`
	Environments      map[string]*Config `toml:"environments"`
}

// ActiveEnvName returns the effective environment name: the process-level
// override set via SetCurrentEnv(), or the file's active_environment field.
func (f *ConfigFile) ActiveEnvName() string {
	if currentEnvOverride != "" {
		return currentEnvOverride
	}
	return f.ActiveEnvironment
}

// ActiveConfig returns the Config for the currently active environment.
// Never returns nil; falls back to a zero Config with sensible defaults.
func (f *ConfigFile) ActiveConfig() *Config {
	name := f.ActiveEnvName()
	if f.Environments != nil {
		if env, ok := f.Environments[name]; ok {
			return env
		}
	}
	return &Config{Output: "table"}
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

// LoadFile reads the complete config file and returns a *ConfigFile.
//
// Three cases are handled transparently:
//   - File not found       → brand-new install; returns a ConfigFile with an empty "default" env.
//   - New format           → active_environment key present; parsed directly.
//   - Legacy flat format   → no active_environment key; top-level keys are wrapped into a
//     "default" environment and the file is silently re-written in the new format.
func LoadFile() (*ConfigFile, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newDefaultConfigFile(), nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Probe the raw map to detect which format the file uses.
	var probe map[string]any
	if err := toml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if _, isNew := probe["active_environment"]; isNew {
		// ── New multi-environment format ─────────────────────────────────────
		var f ConfigFile
		if err := toml.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing config file: %w", err)
		}
		if f.Environments == nil {
			f.Environments = map[string]*Config{}
		}
		return &f, nil
	}

	// ── Legacy flat format — migrate automatically ───────────────────────────
	var legacy Config
	if err := toml.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	f := &ConfigFile{
		ActiveEnvironment: DefaultEnvironmentName,
		Environments: map[string]*Config{
			DefaultEnvironmentName: &legacy,
		},
	}
	// Silently persist the migrated format; a failure here is non-fatal.
	_ = SaveFile(f)
	return f, nil
}

// SaveFile writes the complete ConfigFile to disk with mode 0600.
func SaveFile(f *ConfigFile) error {
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

	data, err := toml.Marshal(f)
	if err != nil {
		return fmt.Errorf("serializing config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}

// Load returns the Config for the currently active environment (or the override
// set via SetCurrentEnv), with environment-variable overrides applied.
// This is the primary read function used by most commands.
func Load() (*Config, error) {
	f, err := LoadFile()
	if err != nil {
		return nil, err
	}

	cfg := f.ActiveConfig()

	// Apply environment-variable overrides (higher priority than file).
	if key := os.Getenv("MOLLIE_API_KEY"); key != "" {
		cfg.APIKey = key
	}
	if os.Getenv("MOLLIE_LIVE_MODE") == "true" {
		cfg.LiveMode = true
	}
	if cfg.Output == "" {
		cfg.Output = "table"
	}

	return cfg, nil
}

// Save persists cfg as the configuration for the currently active environment
// (or the override set via SetCurrentEnv).
// This is the primary write function used by most commands.
func Save(cfg *Config) error {
	f, err := LoadFile()
	if err != nil {
		// If the file couldn't be read at all, start from a clean slate.
		f = newDefaultConfigFile()
	}

	name := f.ActiveEnvName()
	if f.Environments == nil {
		f.Environments = map[string]*Config{}
	}
	f.Environments[name] = cfg

	return SaveFile(f)
}

// newDefaultConfigFile returns a ConfigFile with a single, empty "default"
// environment pre-created, ready to be populated by auth setup.
func newDefaultConfigFile() *ConfigFile {
	return &ConfigFile{
		ActiveEnvironment: DefaultEnvironmentName,
		Environments: map[string]*Config{
			DefaultEnvironmentName: {Output: "table"},
		},
	}
}
