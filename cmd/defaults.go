package cmd

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/spf13/cobra"
)

// ── flag value holders ───────────────────────────────────────────────────────

var (
	defSetDescription string
	defSetAmount      string
	defSetCurrency    string
	defSetRedirectURL string
	defSetWebhookURL  string

	defUnsetAll bool
)

// ── command tree ─────────────────────────────────────────────────────────────

var defaultsCmd = &cobra.Command{
	Use:   "defaults",
	Short: "Manage default values for common create-command parameters",
	Long: `Store frequently used values (description, amount, currency, redirect URL,
webhook URL) so you don't have to retype them on every create command.

Defaults are applied only when the corresponding flag is not explicitly supplied
on the command line. An explicit flag always wins.`,
}

var defaultsSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set one or more default values interactively or via flags",
	Long: `Set default values for common create-command parameters.

Run without flags for an interactive form pre-filled with current values.
Supply any combination of --description, --amount, --currency,
--redirect-url, --webhook-url to set values non-interactively.`,
	RunE: runDefaultsSet,
}

var defaultsShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current default values",
	RunE:  runDefaultsShow,
}

var defaultsUnsetCmd = &cobra.Command{
	Use:   "unset",
	Short: "Clear stored default values",
	Long: `Clear stored default values.

Run without --all to select individual defaults to clear interactively.
Pass --all to wipe every stored default at once.`,
	RunE: runDefaultsUnset,
}

func init() {
	// set flags (all optional — omitting them triggers the interactive form)
	defaultsSetCmd.Flags().StringVar(&defSetDescription, "description", "", "Default payment description")
	defaultsSetCmd.Flags().StringVar(&defSetAmount, "amount", "", "Default payment amount, e.g. 10.00")
	defaultsSetCmd.Flags().StringVar(&defSetCurrency, "currency", "", "Default ISO 4217 currency code, e.g. EUR")
	defaultsSetCmd.Flags().StringVar(&defSetRedirectURL, "redirect-url", "", "Default redirect URL")
	defaultsSetCmd.Flags().StringVar(&defSetWebhookURL, "webhook-url", "", "Default webhook URL")

	// unset flags
	defaultsUnsetCmd.Flags().BoolVar(&defUnsetAll, "all", false, "Clear every stored default value")

	defaultsCmd.AddCommand(defaultsSetCmd)
	defaultsCmd.AddCommand(defaultsShowCmd)
	defaultsCmd.AddCommand(defaultsUnsetCmd)
	rootCmd.AddCommand(defaultsCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runDefaultsSet(cmd *cobra.Command, _ []string) error {
	current, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	anyFlagSet := cmd.Flags().Changed("description") ||
		cmd.Flags().Changed("amount") ||
		cmd.Flags().Changed("currency") ||
		cmd.Flags().Changed("redirect-url") ||
		cmd.Flags().Changed("webhook-url")

	if anyFlagSet {
		// Non-interactive: merge only the flags that were explicitly provided.
		if cmd.Flags().Changed("description") {
			current.DefaultDescription = defSetDescription
		}
		if cmd.Flags().Changed("amount") {
			current.DefaultAmount = defSetAmount
		}
		if cmd.Flags().Changed("currency") {
			current.DefaultCurrency = defSetCurrency
		}
		if cmd.Flags().Changed("redirect-url") {
			current.DefaultRedirectURL = defSetRedirectURL
		}
		if cmd.Flags().Changed("webhook-url") {
			current.DefaultWebhookURL = defSetWebhookURL
		}
	} else {
		// Interactive: show a huh form pre-filled with current values.
		desc := current.DefaultDescription
		amt := current.DefaultAmount
		cur := current.DefaultCurrency
		redir := current.DefaultRedirectURL
		webhook := current.DefaultWebhookURL

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Default description").
					Description("Shown to the customer on the payment page.").
					Placeholder("e.g. Test payment").
					Value(&desc),
				huh.NewInput().
					Title("Default amount").
					Description("Numeric value without currency symbol, e.g. 10.00").
					Placeholder("e.g. 10.00").
					Value(&amt),
				huh.NewInput().
					Title("Default currency").
					Description("ISO 4217 currency code, e.g. EUR").
					Placeholder("EUR").
					Value(&cur),
				huh.NewInput().
					Title("Default redirect URL").
					Description("Where to send the customer after payment.").
					Placeholder("https://example.com/return").
					Value(&redir),
				huh.NewInput().
					Title("Default webhook URL").
					Description("Where Mollie will POST payment status updates.").
					Placeholder("https://example.com/webhook").
					Value(&webhook),
			),
		)

		if err := form.Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("Cancelled.")
				return nil
			}
			return fmt.Errorf("prompt failed: %w", err)
		}

		current.DefaultDescription = desc
		current.DefaultAmount = amt
		current.DefaultCurrency = cur
		current.DefaultRedirectURL = redir
		current.DefaultWebhookURL = webhook
	}

	if err := config.Save(current); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("✓ Defaults saved.")
	return nil
}

func runDefaultsShow(_ *cobra.Command, _ []string) error {
	c, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	output.PrintTable(
		[]string{"FIELD", "DEFAULT VALUE"},
		[][]string{
			{"description", dash(c.DefaultDescription)},
			{"amount", dash(c.DefaultAmount)},
			{"currency", dash(c.DefaultCurrency)},
			{"redirect-url", dash(c.DefaultRedirectURL)},
			{"webhook-url", dash(c.DefaultWebhookURL)},
		},
		false,
	)
	return nil
}

func runDefaultsUnset(cmd *cobra.Command, _ []string) error {
	current, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if defUnsetAll {
		current.DefaultDescription = ""
		current.DefaultAmount = ""
		current.DefaultCurrency = ""
		current.DefaultRedirectURL = ""
		current.DefaultWebhookURL = ""
	} else {
		// Interactive multi-select: let the user pick which defaults to clear.
		candidates := []huh.Option[string]{}
		if current.DefaultDescription != "" {
			candidates = append(candidates, huh.NewOption("description ("+current.DefaultDescription+")", "description"))
		}
		if current.DefaultAmount != "" {
			candidates = append(candidates, huh.NewOption("amount ("+current.DefaultAmount+")", "amount"))
		}
		if current.DefaultCurrency != "" {
			candidates = append(candidates, huh.NewOption("currency ("+current.DefaultCurrency+")", "currency"))
		}
		if current.DefaultRedirectURL != "" {
			candidates = append(candidates, huh.NewOption("redirect-url ("+current.DefaultRedirectURL+")", "redirect-url"))
		}
		if current.DefaultWebhookURL != "" {
			candidates = append(candidates, huh.NewOption("webhook-url ("+current.DefaultWebhookURL+")", "webhook-url"))
		}

		if len(candidates) == 0 {
			fmt.Println("No defaults are currently set.")
			return nil
		}

		var selected []string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("Select defaults to clear").
					Options(candidates...).
					Value(&selected),
			),
		)

		if err := form.Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("Cancelled.")
				return nil
			}
			return fmt.Errorf("prompt failed: %w", err)
		}

		if len(selected) == 0 {
			fmt.Println("Nothing selected.")
			return nil
		}

		for _, s := range selected {
			switch s {
			case "description":
				current.DefaultDescription = ""
			case "amount":
				current.DefaultAmount = ""
			case "currency":
				current.DefaultCurrency = ""
			case "redirect-url":
				current.DefaultRedirectURL = ""
			case "webhook-url":
				current.DefaultWebhookURL = ""
			}
		}
	}

	if err := config.Save(current); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("✓ Defaults cleared.")
	return nil
}

// ── helper shared by create commands ─────────────────────────────────────────

// applyCreateDefaults fills flag variables from the stored config defaults for
// any flag that was not explicitly set on the command line.
//
// Pass a pointer for each defaultable field; nil pointers are skipped.
// Call this at the top of every "create" RunE, after cfg is populated.
func applyCreateDefaults(
	cmd *cobra.Command,
	description, amount, currency, redirectURL, webhookURL *string,
) {
	if description != nil && !cmd.Flags().Changed("description") && cfg.DefaultDescription != "" {
		*description = cfg.DefaultDescription
	}
	if amount != nil && !cmd.Flags().Changed("amount") && cfg.DefaultAmount != "" {
		*amount = cfg.DefaultAmount
	}
	if currency != nil && !cmd.Flags().Changed("currency") && cfg.DefaultCurrency != "" {
		*currency = cfg.DefaultCurrency
	}
	if redirectURL != nil && !cmd.Flags().Changed("redirect-url") && cfg.DefaultRedirectURL != "" {
		*redirectURL = cfg.DefaultRedirectURL
	}
	if webhookURL != nil && !cmd.Flags().Changed("webhook-url") && cfg.DefaultWebhookURL != "" {
		*webhookURL = cfg.DefaultWebhookURL
	}
}
