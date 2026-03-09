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
//   - profileID      — set from --profile flag; takes precedence over cfg.ProfileID
func New(cfg *config.Config, apiKeyOverride string, liveMode bool, profileID string) (*mollieapi.Client, error) {
	apiKey := cfg.APIKey
	if apiKeyOverride != "" {
		apiKey = apiKeyOverride
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured — run `mollie auth setup` to get started")
	}

	opts := []mollieapi.SDKOption{
		// Organization Access Tokens must be provided via the OAuth security field.
		// This activates the SDK's BeforeRequest hook, which automatically injects
		// testmode and profileId into all POST/PATCH/DELETE request bodies — a
		// requirement when using access tokens instead of per-environment API keys.
		mollieapi.WithSecurity(components.Security{
			OAuth: &apiKey,
		}),
		// Test mode is the safe default; live mode is opt-in via --live or config.
		mollieapi.WithTestmode(!liveMode),
		// Identify the Mollie CLI through User Agent
		mollieapi.WithCustomUserAgent("Mollie-CLI/1.0.0"),
	}

	// Profile ID: flag override > config value.
	resolvedProfile := cfg.ProfileID
	if profileID != "" {
		resolvedProfile = profileID
	}
	if resolvedProfile != "" {
		opts = append(opts, mollieapi.WithProfileID(resolvedProfile))
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
