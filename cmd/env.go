package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/spf13/cobra"
)

// ── command tree ──────────────────────────────────────────────────────────────

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage configuration environments",
	Long: `Create, list, copy, switch between, and delete named configuration environments.

Each environment stores its own API key, profile ID, mode setting, output
format, and payment defaults. The active environment is used by all commands
unless you pass --env <name> to override it for a single invocation.`,
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration environments",
	RunE:  runEnvList,
}

var envCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new, empty configuration environment",
	Long: `Create a new configuration environment and optionally run interactive setup.

If [name] is omitted you will be prompted to enter one.

The new environment starts empty (no API key). You will be offered the option
to configure it immediately. You can also configure it later with:

  mollie --env <name> auth setup

or by switching to it first:

  mollie env switch <name>
  mollie auth setup`,
	Args: cobra.MaximumNArgs(1),
	RunE: runEnvCreate,
}

var envCopyCmd = &cobra.Command{
	Use:   "copy <source> [destination]",
	Short: "Copy an existing environment to a new one",
	Long: `Duplicate a configuration environment under a new name.

All settings (including the API key) are copied verbatim. A common use case
is to copy an environment and then swap only the API key:

  mollie env copy default production
  mollie --env production auth setup`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runEnvCopy,
}

var envSwitchCmd = &cobra.Command{
	Use:   "switch <name>",
	Short: "Make a named environment the active one",
	Args:  cobra.ExactArgs(1),
	RunE:  runEnvSwitch,
}

var envDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a configuration environment",
	Long: "Permanently remove a named configuration environment.\n\n" +
		"You cannot delete the currently active environment. Switch to another\n" +
		"environment first:\n\n" +
		"  mollie env switch <other-name>",
	Args: cobra.ExactArgs(1),
	RunE: runEnvDelete,
}

func init() {
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envCreateCmd)
	envCmd.AddCommand(envCopyCmd)
	envCmd.AddCommand(envSwitchCmd)
	envCmd.AddCommand(envDeleteCmd)
	rootCmd.AddCommand(envCmd)
}

// ── handlers ──────────────────────────────────────────────────────────────────

func runEnvList(_ *cobra.Command, _ []string) error {
	f, err := config.LoadFile()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Sort environment names for stable output.
	names := make([]string, 0, len(f.Environments))
	for name := range f.Environments {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([][]string, 0, len(names))
	for _, name := range names {
		env := f.Environments[name]

		active := " "
		if name == f.ActiveEnvironment {
			active = "✓"
		}

		apiKey := "—"
		if env.APIKey != "" {
			apiKey = maskAPIKey(env.APIKey)
		}

		profile := dash(env.ProfileID)

		liveMode := "no"
		if config.IsAPIKey(env.APIKey) {
			// For API keys the mode is baked into the key prefix, not the LiveMode flag.
			if config.IsLiveAPIKey(env.APIKey) {
				liveMode = "yes (key)"
			} else {
				liveMode = "no (key)"
			}
		} else if env.LiveMode {
			liveMode = "yes"
		}

		rows = append(rows, []string{active, name, apiKey, profile, liveMode})
	}

	output.PrintTable(
		[]string{"", "ENVIRONMENT", "API KEY", "PROFILE", "LIVE MODE"},
		rows,
		false,
	)
	return nil
}

func runEnvCreate(_ *cobra.Command, args []string) error {
	f, err := config.LoadFile()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// ── Determine the new environment name ───────────────────────────────────
	var name string
	if len(args) == 1 {
		name = strings.TrimSpace(args[0])
	} else {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Environment name").
				Description(`A short identifier, e.g. "production" or "staging".`).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("name cannot be empty")
					}
					return nil
				}).
				Value(&name),
		)).Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("Cancelled.")
				return nil
			}
			return fmt.Errorf("prompt failed: %w", err)
		}
		name = strings.TrimSpace(name)
	}

	if _, exists := f.Environments[name]; exists {
		return fmt.Errorf("environment %q already exists — use `mollie env copy` to duplicate it", name)
	}

	// ── Create the empty environment ─────────────────────────────────────────
	if f.Environments == nil {
		f.Environments = map[string]*config.Config{}
	}
	f.Environments[name] = &config.Config{Output: "table"}

	if err := config.SaveFile(f); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Printf("✓ Environment %q created.\n", name)

	// ── Offer to configure it immediately ────────────────────────────────────
	runSetup := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Configure this environment now?").
			Description("Run interactive API key setup for the new environment.").
			Affirmative("Yes, set it up now").
			Negative("No, I'll do it later").
			Value(&runSetup),
	)).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			printEnvConfigHint(name)
			return nil
		}
		return fmt.Errorf("prompt failed: %w", err)
	}

	if runSetup {
		// Temporarily redirect config.Load()/Save() to the new environment.
		config.SetCurrentEnv(name)
		defer config.SetCurrentEnv("")
		return runAuthSetup(nil, nil)
	}

	printEnvConfigHint(name)
	return nil
}

func runEnvCopy(_ *cobra.Command, args []string) error {
	f, err := config.LoadFile()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	src := args[0]
	srcEnv, exists := f.Environments[src]
	if !exists {
		return fmt.Errorf("source environment %q not found — run `mollie env list` to see available environments", src)
	}

	// ── Determine destination name ────────────────────────────────────────────
	var dest string
	if len(args) == 2 {
		dest = strings.TrimSpace(args[1])
	} else {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Destination environment name").
				Description(fmt.Sprintf(`Copy of "%s" will be saved under this name.`, src)).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("name cannot be empty")
					}
					return nil
				}).
				Value(&dest),
		)).Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("Cancelled.")
				return nil
			}
			return fmt.Errorf("prompt failed: %w", err)
		}
		dest = strings.TrimSpace(dest)
	}

	if _, exists := f.Environments[dest]; exists {
		return fmt.Errorf("environment %q already exists", dest)
	}

	// ── Deep-copy the source ──────────────────────────────────────────────────
	copied := *srcEnv
	f.Environments[dest] = &copied

	if err := config.SaveFile(f); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("✓ Environment %q copied from %q.\n", dest, src)
	fmt.Printf("  To swap just the API key in the new environment:\n")
	fmt.Printf("    mollie --env %s auth setup\n", dest)
	return nil
}

func runEnvSwitch(_ *cobra.Command, args []string) error {
	name := args[0]

	f, err := config.LoadFile()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if _, exists := f.Environments[name]; !exists {
		return fmt.Errorf("environment %q not found — run `mollie env list` to see available environments", name)
	}

	if f.ActiveEnvironment == name {
		fmt.Printf("Environment %q is already active.\n", name)
		return nil
	}

	f.ActiveEnvironment = name
	if err := config.SaveFile(f); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("✓ Switched to environment %q.\n", name)
	return nil
}

func runEnvDelete(_ *cobra.Command, args []string) error {
	name := args[0]

	f, err := config.LoadFile()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if _, exists := f.Environments[name]; !exists {
		return fmt.Errorf("environment %q not found — run `mollie env list` to see available environments", name)
	}

	if f.ActiveEnvironment == name {
		return fmt.Errorf(
			"cannot delete the active environment %q\n"+
				"  Switch to another environment first: mollie env switch <name>",
			name,
		)
	}

	confirmed, err := prompt.Confirm(fmt.Sprintf("Delete environment %q? This cannot be undone.", name))
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

	delete(f.Environments, name)
	if err := config.SaveFile(f); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("✓ Environment %q deleted.\n", name)
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// isEnvCommand returns true when cmd or any of its ancestors is the env command.
func isEnvCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "env" {
			return true
		}
	}
	return false
}

func printEnvConfigHint(name string) {
	fmt.Printf("\nTo configure it later, run either:\n")
	fmt.Printf("  mollie --env %s auth setup\n", name)
	fmt.Printf("  — or —\n")
	fmt.Printf("  mollie env switch %s\n", name)
	fmt.Printf("  mollie auth setup\n")
}
