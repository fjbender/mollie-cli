package mollieclient

import (
	"fmt"

	mollieapi "github.com/mollie/mollie-api-golang"
	"github.com/mollie/mollie-api-golang/models/components"

	"github.com/fjbender/mollie-cli/internal/config"
)

// New initialises the Mollie SDK client from the resolved configuration and any
// invocation-level overrides supplied via global flags.
//
//   - apiKeyOverride — set from --api-key flag; takes precedence over cfg.APIKey
//   - liveMode       — set from --live flag; when true test mode is disabled
//     (ignored when key is an API key — mode is determined by key prefix)
//   - profileID      — set from --profile flag; takes precedence over cfg.ProfileID
//     (ignored when key is an API key — profile is baked into the key)
func New(cfg *config.Config, apiKeyOverride string, liveMode bool, profileID string) (*mollieapi.Client, error) {
	apiKey := cfg.APIKey
	if apiKeyOverride != "" {
		apiKey = apiKeyOverride
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured — run `mollie auth setup` to get started")
	}

	// API keys (test_/live_) are profile- and mode-scoped, so they use the http
	// bearer security scheme and must NOT have testmode or profileId injected.
	// Organization Access Tokens use the OAuth security scheme and require both.
	var sec components.Security
	if config.IsAPIKey(apiKey) {
		sec = components.Security{APIKey: &apiKey}
	} else {
		sec = components.Security{OAuth: &apiKey}
	}

	opts := []mollieapi.SDKOption{
		mollieapi.WithSecurity(sec),
		// Identify the Mollie CLI through User Agent
		mollieapi.WithCustomUserAgent("Mollie-CLI/1.0.0"),
	}

	// testmode and profileId are only meaningful for Organization Access Tokens.
	// API keys are already scoped to a specific mode and profile.
	if !config.IsAPIKey(apiKey) {
		// Test mode is the safe default; live mode is opt-in via --live or config.
		opts = append(opts, mollieapi.WithTestmode(!liveMode))

		// Profile ID: flag override > config value.
		resolvedProfile := cfg.ProfileID
		if profileID != "" {
			resolvedProfile = profileID
		}
		if resolvedProfile != "" {
			opts = append(opts, mollieapi.WithProfileID(resolvedProfile))
		}
	}

	return mollieapi.New(opts...), nil
}

// NewOrganizationClient builds a minimal client suitable for endpoints that
// only accept an organization access token and do not support profileId or
// testmode (e.g. Invoices). It deliberately omits both WithProfileID and
// WithTestmode, and must not delegate to New (which falls back to cfg.ProfileID).
func NewOrganizationClient(cfg *config.Config, apiKeyOverride string) (*mollieapi.Client, error) {
	apiKey := cfg.APIKey
	if apiKeyOverride != "" {
		apiKey = apiKeyOverride
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured — run `mollie auth setup` to get started")
	}

	opts := []mollieapi.SDKOption{
		mollieapi.WithSecurity(components.Security{
			OAuth: &apiKey,
		}),
		// testmode and profileId are deliberately omitted — not supported by this endpoint.
	}

	return mollieapi.New(opts...), nil
}
