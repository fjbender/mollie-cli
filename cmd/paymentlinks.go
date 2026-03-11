package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/input"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"
	"github.com/spf13/cobra"
)

// ── flag value holders ───────────────────────────────────────────────────────

var (
	// create flags
	plCreateAmount      string
	plCreateCurrency    string
	plCreateDescription string
	plCreateRedirectURL string
	plCreateWebhookURL  string
	plCreateReusable    bool
	plCreateExpiresAt   string

	// list flags
	plListLimit int64
	plListFrom  string

	// update flags
	plUpdateDescription string
	plUpdateArchived    bool

	// delete flag
	plConfirm bool

	// payments sub-command flags
	plPaymentsLimit int64
	plPaymentsFrom  string
)

// ── command tree ─────────────────────────────────────────────────────────────

var paymentLinksCmd = &cobra.Command{
	Use:   "payment-links",
	Short: "Manage Mollie payment links",
}

var paymentLinksCreateCmd = &cobra.Command{
	Use:         "create",
	Short:       "Create a new payment link",
	RunE:        runPaymentLinksCreate,
	Annotations: map[string]string{"usesDefaults": "true"},
}

var paymentLinksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List payment links",
	RunE:  runPaymentLinksList,
}

var paymentLinksGetCmd = &cobra.Command{
	Use:   "get <payment-link-id>",
	Short: "Get a single payment link",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaymentLinksGet,
}

var paymentLinksUpdateCmd = &cobra.Command{
	Use:   "update <payment-link-id>",
	Short: "Update a payment link",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaymentLinksUpdate,
}

var paymentLinksDeleteCmd = &cobra.Command{
	Use:   "delete <payment-link-id>",
	Short: "Delete a payment link",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaymentLinksDelete,
}

var paymentLinksPaymentsCmd = &cobra.Command{
	Use:   "payments <payment-link-id>",
	Short: "List payments created from a payment link",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaymentLinksPayments,
}

func init() {
	// create flags
	paymentLinksCreateCmd.Flags().StringVar(&plCreateAmount, "amount", "", "Payment amount, e.g. 10.00 (falls back to `defaults set --amount`; omit for open-amount link)")
	paymentLinksCreateCmd.Flags().StringVar(&plCreateCurrency, "currency", "", "ISO 4217 currency code, e.g. EUR (falls back to `defaults set --currency`; required when --amount is set)")
	paymentLinksCreateCmd.Flags().StringVar(&plCreateDescription, "description", "", "Description shown on the payment link (required; falls back to `defaults set --description`)")
	paymentLinksCreateCmd.Flags().StringVar(&plCreateRedirectURL, "redirect-url", "", "URL to redirect the customer to after payment (falls back to `defaults set --redirect-url`)")
	paymentLinksCreateCmd.Flags().StringVar(&plCreateWebhookURL, "webhook-url", "", "Webhook URL for payment status updates (falls back to `defaults set --webhook-url`)")
	paymentLinksCreateCmd.Flags().BoolVar(&plCreateReusable, "reusable", false, "Allow the link to be used multiple times")
	paymentLinksCreateCmd.Flags().StringVar(&plCreateExpiresAt, "expires-at", "", "Expiry datetime in ISO 8601 format (e.g. 2026-12-31T23:59:59+00:00)")

	// list flags
	paymentLinksListCmd.Flags().Int64Var(&plListLimit, "limit", 50, "Maximum number of results to return")
	paymentLinksListCmd.Flags().StringVar(&plListFrom, "from", "", "Return results starting from this payment link ID (cursor pagination)")

	// update flags
	paymentLinksUpdateCmd.Flags().StringVar(&plUpdateDescription, "description", "", "New description")
	paymentLinksUpdateCmd.Flags().BoolVar(&plUpdateArchived, "archived", false, "Archive (true) or unarchive (false) the payment link")

	// delete flag
	paymentLinksDeleteCmd.Flags().BoolVar(&plConfirm, "confirm", false, "Skip the confirmation prompt")

	// payments sub-command flags
	paymentLinksPaymentsCmd.Flags().Int64Var(&plPaymentsLimit, "limit", 50, "Maximum number of results to return")
	paymentLinksPaymentsCmd.Flags().StringVar(&plPaymentsFrom, "from", "", "Return results starting from this payment ID (cursor pagination)")

	paymentLinksCmd.AddCommand(paymentLinksCreateCmd)
	paymentLinksCmd.AddCommand(paymentLinksListCmd)
	paymentLinksCmd.AddCommand(paymentLinksGetCmd)
	paymentLinksCmd.AddCommand(paymentLinksUpdateCmd)
	paymentLinksCmd.AddCommand(paymentLinksDeleteCmd)
	paymentLinksCmd.AddCommand(paymentLinksPaymentsCmd)
	rootCmd.AddCommand(paymentLinksCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runPaymentLinksCreate(cmd *cobra.Command, _ []string) error {
	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "description"); ok && !cmd.Flags().Changed("description") {
			plCreateDescription = v
		}
		if val, cur, ok := input.Amount(jsonInput, "amount"); ok {
			if !cmd.Flags().Changed("amount") {
				plCreateAmount = val
			}
			if !cmd.Flags().Changed("currency") {
				plCreateCurrency = cur
			}
		}
		if v, ok := input.Str(jsonInput, "redirectUrl"); ok && !cmd.Flags().Changed("redirect-url") {
			plCreateRedirectURL = v
		}
		if v, ok := input.Str(jsonInput, "webhookUrl"); ok && !cmd.Flags().Changed("webhook-url") {
			plCreateWebhookURL = v
		}
		if v, ok := input.Bool(jsonInput, "reusable"); ok && !cmd.Flags().Changed("reusable") {
			plCreateReusable = v
		}
		if v, ok := input.Str(jsonInput, "expiresAt"); ok && !cmd.Flags().Changed("expires-at") {
			plCreateExpiresAt = v
		}
	}

	applyCreateDefaults(cmd,
		&plCreateDescription, &plCreateAmount, &plCreateCurrency,
		&plCreateRedirectURL, &plCreateWebhookURL,
	)

	if plCreateDescription == "" {
		return fmt.Errorf("required flag \"description\" not set and no default configured (run `mollie defaults set`)")
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	body := &operations.CreatePaymentLinkRequestBody{
		Description: plCreateDescription,
	}

	if plCreateAmount != "" {
		if plCreateCurrency == "" {
			return fmt.Errorf("--currency is required when --amount is set")
		}
		body.Amount = &components.AmountNullable{
			Value:    plCreateAmount,
			Currency: plCreateCurrency,
		}
	}

	if plCreateRedirectURL != "" {
		body.RedirectURL = &plCreateRedirectURL
	}
	if plCreateWebhookURL != "" {
		body.WebhookURL = &plCreateWebhookURL
	}
	if plCreateReusable {
		body.Reusable = &plCreateReusable
	}
	if plCreateExpiresAt != "" {
		body.ExpiresAt = &plCreateExpiresAt
	}

	resp, err := client.PaymentLinks.Create(context.Background(), nil, body)
	if err != nil {
		return fmt.Errorf("creating payment link: %w", err)
	}
	pl := resp.GetPaymentLinkResponse()
	if pl == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(pl)
	default:
		linkURL := pl.Links.GetPaymentLink().Href
		output.PrintTable(
			[]string{"ID", "DESCRIPTION", "AMOUNT", "REUSABLE", "PAYMENT LINK URL"},
			[][]string{{
				pl.GetID(),
				pl.GetDescription(),
				formatNullableAmount(pl.GetAmount()),
				fmt.Sprintf("%v", derefBool(pl.GetReusable())),
				linkURL,
			}},
			!flagLive,
		)
	}
	return nil
}

func runPaymentLinksList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	var from *string
	if plListFrom != "" {
		from = &plListFrom
	}

	resp, err := client.PaymentLinks.List(context.Background(), from, &plListLimit, nil, nil)
	if err != nil {
		return fmt.Errorf("listing payment links: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	links := resp.Object.Embedded.GetPaymentLinks()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(links))
		for _, pl := range links {
			rows = append(rows, []string{
				pl.GetID(),
				pl.GetDescription(),
				formatNullableAmount(pl.GetAmount()),
				pl.GetCreatedAt(),
				fmt.Sprintf("%v", pl.GetArchived()),
			})
		}
		output.PrintTable(
			[]string{"ID", "DESCRIPTION", "AMOUNT", "CREATED AT", "ARCHIVED"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runPaymentLinksGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.PaymentLinks.Get(context.Background(), args[0], nil, nil)
	if err != nil {
		return fmt.Errorf("getting payment link: %w", err)
	}
	pl := resp.GetPaymentLinkResponse()
	if pl == nil {
		return fmt.Errorf("payment link not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(pl)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			paymentLinkDetailRows(pl),
			!flagLive,
		)
	}
	return nil
}

func runPaymentLinksUpdate(cmd *cobra.Command, args []string) error {
	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "description"); ok && !cmd.Flags().Changed("description") {
			plUpdateDescription = v
		}
		if v, ok := input.Bool(jsonInput, "archived"); ok && !cmd.Flags().Changed("archived") {
			plUpdateArchived = v
		}
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	body := &operations.UpdatePaymentLinkRequestBody{}
	if plUpdateDescription != "" {
		body.Description = &plUpdateDescription
	}
	// Only send archived if the flag was explicitly set by the user.
	if cmd.Flags().Changed("archived") {
		body.Archived = &plUpdateArchived
	}

	resp, err := client.PaymentLinks.Update(context.Background(), args[0], nil, body)
	if err != nil {
		return fmt.Errorf("updating payment link: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp)
	default:
		fmt.Printf("✓ Payment link %s updated\n", args[0])
	}
	return nil
}

func runPaymentLinksDelete(_ *cobra.Command, args []string) error {
	linkID := args[0]

	if !plConfirm {
		confirmed, err := prompt.Confirm(fmt.Sprintf("Delete payment link %s?", linkID))
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
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	if _, err := client.PaymentLinks.Delete(context.Background(), linkID, nil, nil); err != nil {
		return fmt.Errorf("deleting payment link: %w", err)
	}

	fmt.Printf("✓ Payment link %s deleted\n", linkID)
	return nil
}

func runPaymentLinksPayments(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.GetPaymentLinkPaymentsRequest{
		PaymentLinkID: args[0],
		Limit:         &plPaymentsLimit,
	}
	if plPaymentsFrom != "" {
		req.From = &plPaymentsFrom
	}

	resp, err := client.PaymentLinks.ListPayments(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing payments for payment link: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	payments := resp.Object.Embedded.GetPayments()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(payments))
		for _, p := range payments {
			rows = append(rows, []string{
				p.GetID(),
				string(p.GetStatus()),
				p.GetCreatedAt(),
				p.GetDescription(),
			})
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "CREATED AT", "DESCRIPTION"},
			rows,
			!flagLive,
		)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// formatNullableAmount returns "VALUE CURRENCY" or "—" for open-amount links.
func formatNullableAmount(a *components.AmountNullable) string {
	if a == nil {
		return "—"
	}
	return fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
}

// derefBool returns the value of a *bool or false when nil.
func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

// paymentLinkDetailRows converts a PaymentLinkResponse into the key/value rows
// shown by `payment-links get` (table mode).
func paymentLinkDetailRows(pl *components.PaymentLinkResponse) [][]string {
	row := func(k, v string) []string { return []string{k, v} }

	reusable := fmt.Sprintf("%v", derefBool(pl.GetReusable()))

	linkURL := "—"
	if href := pl.Links.GetPaymentLink().Href; href != "" {
		linkURL = href
	}

	return [][]string{
		row("ID", pl.GetID()),
		row("Mode", string(pl.GetMode())),
		row("Description", pl.GetDescription()),
		row("Amount", formatNullableAmount(pl.GetAmount())),
		row("Minimum Amount", formatNullableAmount(pl.GetMinimumAmount())),
		row("Archived", fmt.Sprintf("%v", pl.GetArchived())),
		row("Reusable", reusable),
		row("Profile ID", derefOpt(pl.GetProfileID())),
		row("Customer ID", derefOpt(pl.GetCustomerID())),
		row("Redirect URL", derefOpt(pl.GetRedirectURL())),
		row("Webhook URL", derefOpt(pl.GetWebhookURL())),
		row("Payment Link URL", linkURL),
		row("Created At", pl.GetCreatedAt()),
		row("Expires At", derefOpt(pl.GetExpiresAt())),
		row("Paid At", derefOpt(pl.GetPaidAt())),
	}
}
