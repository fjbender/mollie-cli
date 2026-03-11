package prompt

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
)

// ProfileOption is a display-label / value pair used to populate the profile
// selection list. The caller is responsible for mapping SDK types to this shape.
type ProfileOption struct {
	Label string // shown to the user (e.g. "My Shop — pfl_abc123")
	Value string // the profile ID stored in config
}

// APIKey prompts the user interactively for a Mollie API key or Organization
// Access Token. It accepts all three supported prefixes:
//   - access_ — Organization Access Token
//   - test_   — Test-mode API key (profile-scoped)
//   - live_   — Live-mode API key (profile-scoped)
func APIKey() (string, error) {
	var apiKey string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Mollie API Key / Organization Access Token").
				Description("Paste your key (format: access_*, test_*, or live_*)").
				EchoMode(huh.EchoModePassword).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if strings.HasPrefix(s, "access_") && len(s) >= 15 {
						return nil
					}
					if (strings.HasPrefix(s, "test_") || strings.HasPrefix(s, "live_")) && len(s) >= 10 {
						return nil
					}
					return fmt.Errorf("key must start with 'access_', 'test_', or 'live_'")
				}).
				Value(&apiKey),
		),
	)

	if err := form.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(apiKey), nil
}

// Confirm prompts the user with a yes/no question and returns the answer.
func Confirm(message string) (bool, error) {
	var confirmed bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(message).
				Affirmative("Yes").
				Negative("No").
				Value(&confirmed),
		),
	)

	if err := form.Run(); err != nil {
		return false, err
	}
	return confirmed, nil
}

// skipProfileValue is the sentinel value used when the user skips profile
// selection. The caller should treat this as an empty profile ID.
const skipProfileValue = "__skip__"

// ProfileSelect presents the user with a list of profiles to choose from.
// A "Skip" option is always appended so the user can proceed without selecting
// one. Returns an empty string if the user skips.
func ProfileSelect(profiles []ProfileOption) (string, error) {
	if len(profiles) == 0 {
		return "", nil
	}

	opts := make([]huh.Option[string], 0, len(profiles)+1)
	for _, p := range profiles {
		opts = append(opts, huh.NewOption(p.Label, p.Value))
	}
	opts = append(opts, huh.NewOption("Skip — configure later", skipProfileValue))

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Profile").
				Description("Profile IDs are required for payment operations with org tokens.").
				Options(opts...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return "", err
	}
	if selected == skipProfileValue {
		return "", nil
	}
	return strings.TrimSpace(selected), nil
}
