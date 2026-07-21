package tunnelstate_test

import (
	"testing"

	"github.com/fjbender/mollie-cli/internal/tunnelstate"
)

func TestLoad_MissingFileReturnsEmptyState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	f, err := tunnelstate.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Environments == nil {
		t.Fatal("expected a non-nil, empty Environments map")
	}
	if len(f.Environments) != 0 {
		t.Errorf("expected no environments, got %d", len(f.Environments))
	}
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	f, err := tunnelstate.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env := f.Get("default")
	env.OwnedSubscriptionID = "wh_123"
	env.PendingRestore = &tunnelstate.SubscriptionSnapshot{
		ID:         "wh_456",
		Name:       "My backend",
		URL:        "https://example.com/webhooks",
		EventTypes: []string{"payment-link.paid"},
	}

	if err := tunnelstate.Save(f); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	reloaded, err := tunnelstate.Load()
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}
	got := reloaded.Get("default")
	if got.OwnedSubscriptionID != "wh_123" {
		t.Errorf("OwnedSubscriptionID = %q, want %q", got.OwnedSubscriptionID, "wh_123")
	}
	if got.PendingRestore == nil || got.PendingRestore.ID != "wh_456" {
		t.Errorf("PendingRestore = %+v, want ID wh_456", got.PendingRestore)
	}
}

func TestFile_Get_CreatesEmptyStateForUnknownEnv(t *testing.T) {
	f := &tunnelstate.File{}

	env := f.Get("staging")
	if env == nil {
		t.Fatal("expected a non-nil EnvState")
	}
	if env.OwnedSubscriptionID != "" || env.PendingRestore != nil {
		t.Errorf("expected zero-value EnvState, got %+v", env)
	}

	// Getting the same name again must return the same instance, so
	// mutations made through one Get() call are visible via another.
	env.OwnedSubscriptionID = "wh_1"
	if f.Get("staging").OwnedSubscriptionID != "wh_1" {
		t.Error("expected Get to return the same EnvState instance on repeat calls")
	}
}
