// Package tunnelstate persists cross-invocation state for `mollie
// webhook-tunnel`: which webhook subscription (if any) this tool currently
// owns, and — when an existing foreign subscription was temporarily
// repointed — what to restore it to if the process is killed before it can
// clean up after itself.
package tunnelstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fjbender/mollie-cli/internal/config"
)

// SubscriptionSnapshot captures a webhook subscription's identity and
// configuration before webhook-tunnel repoints it, so it can be restored.
type SubscriptionSnapshot struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
}

// EnvState is the webhook-tunnel state for a single config environment.
type EnvState struct {
	// OwnedSubscriptionID is the ID of a webhook subscription this tool
	// created in a previous run, so a later run can recognize and safely
	// replace it without asking the user.
	OwnedSubscriptionID string `json:"owned_subscription_id,omitempty"`
	// PendingRestore holds the pre-repoint state of a foreign subscription
	// that was patched to point at a tunnel. It's cleared once restored;
	// if it's non-nil on startup, the previous run didn't shut down cleanly.
	PendingRestore *SubscriptionSnapshot `json:"pending_restore,omitempty"`
}

// File is the on-disk representation of webhook-tunnel state, keyed per
// config environment (mirrors config.File).
type File struct {
	Environments map[string]*EnvState `json:"environments"`
}

// Get returns the EnvState for name, creating and registering an empty one
// if none exists yet. Never returns nil.
func (f *File) Get(name string) *EnvState {
	if f.Environments == nil {
		f.Environments = map[string]*EnvState{}
	}
	if s, ok := f.Environments[name]; ok {
		return s
	}
	s := &EnvState{}
	f.Environments[name] = s
	return s
}

const fileName = "webhook-tunnel-state.json"

func statePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the state file, returning an empty File if it doesn't exist yet.
func Load() (*File, error) {
	p, err := statePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Environments: map[string]*EnvState{}}, nil
		}
		return nil, fmt.Errorf("reading tunnel state file: %w", err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing tunnel state file: %w", err)
	}
	if f.Environments == nil {
		f.Environments = map[string]*EnvState{}
	}
	return &f, nil
}

// Save writes f to the state file, creating the config directory if needed.
func Save(f *File) error {
	p, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing tunnel state: %w", err)
	}
	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("writing tunnel state file: %w", err)
	}
	return nil
}
