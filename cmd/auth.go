package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/fjbender/mollie-cli/internal/prompt"
	mollieapi "github.com/mollie/mollie-api-golang"
	"github.com/spf13/cobra"
)

// authCmd is the parent for all credential-management subcommands.
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Mollie API credentials and configuration",
}

// authSetupCmd guides the user through first-run credential setup.
var authSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive first-run setup: configure your API key",
	Long: `Prompt for a Mollie Organization Access Token, validate it against the
Mollie API, and save it to the local config file.`,
	RunE: runAuthSetup,
}

// authStatusCmd displays the current configuration.
var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current configuration and authentication status",
	RunE:  runAuthStatus,
}

// authClearCmd removes stored credentials.
var authClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove stored credentials from the config file",
	RunE:  runAuthClear,
}

func init() {
	authCmd.AddCommand(authSetupCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authClearCmd)
	rootCmd.AddCommand(authCmd)
}

// runAuthSetup implements the interactive first-run setup flow.
func runAuthSetup(_ *cobra.Command, _ []string) error {
	apiKey, err := prompt.APIKey()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Setup cancelled.")
			return nil
		}
		return fmt.Errorf("prompt failed: %w", err)
	}

	fmt.Println("Validating token …")

	// Validate the token by hitting GET /v2/organizations/me.
	// We always use test mode here so live data is never accidentally touched.
	tmpCfg := &config.Config{APIKey: apiKey}
	client, err := mollieclient.New(tmpCfg, "", false, "")
	if err != nil {
		return err
	}

	if _, err := client.Organizations.GetCurrent(context.Background(), nil); err != nil {
		return fmt.Errorf("token validation failed — the key has NOT been saved\n  %w", err)
	}

	// Fetch available profiles so the user can select one up-front.
	profileID, err := selectProfileInteractively(client)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Setup cancelled.")
			return nil
		}
		return err
	}

	// Load existing config so we don't clobber other settings.
	existing, loadErr := config.Load()
	if loadErr != nil {
		existing = &config.Config{Output: "table"}
	}
	if existing.Output == "" {
		existing.Output = "table"
	}
	existing.APIKey = apiKey
	existing.ProfileID = profileID

	if err := config.Save(existing); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	path, _ := config.Path()
	fmt.Printf("✓ Token validated and saved to %s\n", path)
	if profileID != "" {
		fmt.Printf("✓ Default profile set to %s\n", profileID)
	}
	return nil
}

// selectProfileInteractively fetches available profiles from the Mollie API and
// presents an interactive selection to the user. Returns "" if the user skips,
// no profiles exist, or the fetch fails (which is treated as a soft warning so
// setup is not blocked).
func selectProfileInteractively(client *mollieapi.Client) (string, error) {
	fmt.Println("Fetching profiles …")

	resp, err := client.Profiles.List(context.Background(), nil, nil, nil)
	if err != nil {
		fmt.Printf("Warning: could not fetch profiles (%v)\n  You can set a profile later with --profile or by re-running setup.\n", err)
		return "", nil
	}
	if resp.Object == nil {
		return "", nil
	}

	embedded := resp.Object.GetEmbedded()
	profiles := embedded.GetProfiles()
	if len(profiles) == 0 {
		fmt.Println("No profiles found — you can configure one later with --profile.")
		return "", nil
	}

	opts := make([]prompt.ProfileOption, 0, len(profiles))
	for _, p := range profiles {
		label := fmt.Sprintf("%s — %s", p.GetName(), p.GetID())
		if p.GetWebsite() != "" {
			label = fmt.Sprintf("%s (%s) — %s", p.GetName(), p.GetWebsite(), p.GetID())
		}
		opts = append(opts, prompt.ProfileOption{Label: label, Value: p.GetID()})
	}

	return prompt.ProfileSelect(opts)
}

// runAuthStatus displays the current auth / config state.
func runAuthStatus(_ *cobra.Command, _ []string) error {
	c, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if !c.IsConfigured() {
		fmt.Fprintln(os.Stderr, "No API key configured. Run `mollie auth setup` to get started.")
		os.Exit(1)
	}

	liveModeStr := "false (test mode is active)"
	if c.LiveMode {
		liveModeStr = "true"
	}

	output.PrintTable(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"API Key", maskAPIKey(c.APIKey)},
			{"Profile ID", dash(c.ProfileID)},
			{"Live Mode", liveModeStr},
			{"Default Output", dash(c.Output)},
		},
		false, // status is informational; don't add a [TEST] banner
	)
	return nil
}

// runAuthClear removes the config file after an interactive confirmation.
func runAuthClear(_ *cobra.Command, _ []string) error {
	cfgPath, err := config.Path()
	if err != nil {
		return err
	}

	confirmed, err := prompt.Confirm(fmt.Sprintf("Remove stored credentials from %s?", cfgPath))
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Cancelled.")
			return nil
		}
		return err
	}
	if !confirmed {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing config file: %w", err)
	}

	fmt.Printf("✓ Credentials cleared (%s removed)\n", cfgPath)
	return nil
}

// maskAPIKey returns a masked representation: access_****xyz
func maskAPIKey(key string) string {
	if len(key) <= 12 {
		return "access_****"
	}
	return key[:7] + "****" + key[len(key)-3:]
}

// dash returns "—" for empty strings, otherwise the value as-is.
func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
