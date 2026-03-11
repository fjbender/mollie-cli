package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/spf13/cobra"
)

// Global flag values — populated by Cobra before any RunE is called.
var (
	flagLive    bool
	flagOutput  string
	flagProfile string
	flagAPIKey  string
	flagNoColor bool
	flagEnv     string // selects a config environment for this invocation
)

// cfg holds the loaded configuration for the current invocation.
// It is populated by PersistentPreRunE for commands that require it.
var cfg *config.Config

// rootCmd is the top-level "mollie" command.
var rootCmd = &cobra.Command{
	Use:   "mollie",
	Short: "Mollie CLI — manage your Mollie resources from the terminal",
	Long: `mollie-cli wraps the Mollie REST API so you can create, inspect, and manage
payment resources without writing throwaway scripts or crafting raw HTTP requests.

All commands run in test mode by default. Pass --live to operate on live data.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	// PersistentPreRunE fires for every subcommand before its own RunE.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Honour --no-color and the NO_COLOR env variable.
		if flagNoColor || os.Getenv("NO_COLOR") != "" {
			output.NoColor = true
		}

		// MOLLIE_LIVE_MODE=true is equivalent to --live.
		if os.Getenv("MOLLIE_LIVE_MODE") == "true" {
			flagLive = true
		}

		// Apply --env override globally so that config.Load()/Save() in every
		// command — including auth and env subcommands — target the right env.
		if flagEnv != "" {
			config.SetCurrentEnv(flagEnv)
		}

		// auth and env subcommands manage configuration themselves and don't
		// require a pre-configured API key to run.
		if isAuthCommand(cmd) || isEnvCommand(cmd) {
			return nil
		}

		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if !cfg.IsConfigured() {
			return fmt.Errorf("no API key configured — run `mollie auth setup` to get started")
		}
		return nil
	},
}

// Execute runs the root command and exits with code 1 on any error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		output.Errorf("%s", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(
		&flagLive, "live", "l", false,
		"Run against the live Mollie environment (default: test mode)",
	)
	rootCmd.PersistentFlags().StringVarP(
		&flagOutput, "output", "o", "",
		`Output format: table (default), json, yaml`,
	)
	rootCmd.PersistentFlags().StringVar(
		&flagProfile, "profile", "",
		"Override the profile ID for this invocation",
	)
	rootCmd.PersistentFlags().StringVar(
		&flagAPIKey, "api-key", "",
		"Override the stored API key for this invocation",
	)
	rootCmd.PersistentFlags().BoolVar(
		&flagNoColor, "no-color", false,
		"Disable ANSI color output",
	)
	rootCmd.PersistentFlags().StringVarP(
		&flagEnv, "env", "e", "",
		"Use a specific config environment for this invocation (overrides active environment)",
	)
}

// isAuthCommand returns true when cmd or any of its ancestors is the auth command.
func isAuthCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "auth" {
			return true
		}
	}
	return false
}

// resolvedOutput returns the effective output format for the current invocation,
// applying the flag → config → default precedence chain.
func resolvedOutput() output.Format {
	if flagOutput != "" {
		return output.Format(strings.ToLower(flagOutput))
	}
	if cfg != nil && cfg.Output != "" {
		return output.Format(cfg.Output)
	}
	return output.FormatTable
}
