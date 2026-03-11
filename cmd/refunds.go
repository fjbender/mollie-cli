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
	refCreateAmount      string
	refCreateCurrency    string
	refCreateDescription string

	// list flags (per-payment)
	refListLimit int64
	refListFrom  string

	// list-all flags
	refAllLimit int64
	refAllFrom  string

	// cancel flag
	refCancelConfirm bool
)

// ── command tree ─────────────────────────────────────────────────────────────

var refundsCmd = &cobra.Command{
	Use:   "refunds",
	Short: "Manage Mollie refunds",
}

var refundsCreateCmd = &cobra.Command{
	Use:   "create <payment-id>",
	Short: "Create a refund for a payment",
	Args:  cobra.ExactArgs(1),
	RunE:  runRefundsCreate,
}

var refundsListCmd = &cobra.Command{
	Use:   "list <payment-id>",
	Short: "List refunds for a payment",
	Args:  cobra.ExactArgs(1),
	RunE:  runRefundsList,
}

var refundsListAllCmd = &cobra.Command{
	Use:   "list-all",
	Short: "List all refunds across all payments",
	RunE:  runRefundsListAll,
}

var refundsGetCmd = &cobra.Command{
	Use:   "get <payment-id> <refund-id>",
	Short: "Get a single refund",
	Args:  cobra.ExactArgs(2),
	RunE:  runRefundsGet,
}

var refundsCancelCmd = &cobra.Command{
	Use:   "cancel <payment-id> <refund-id>",
	Short: "Cancel a queued refund",
	Args:  cobra.ExactArgs(2),
	RunE:  runRefundsCancel,
}

func init() {
	// create flags
	refundsCreateCmd.Flags().StringVar(&refCreateAmount, "amount", "", "Refund amount, e.g. 10.00 (omit to refund the full payment amount)")
	refundsCreateCmd.Flags().StringVar(&refCreateCurrency, "currency", "", "ISO 4217 currency code (required when --amount is set)")
	refundsCreateCmd.Flags().StringVar(&refCreateDescription, "description", "", "Description shown to the customer (optional)")

	// list flags
	refundsListCmd.Flags().Int64Var(&refListLimit, "limit", 50, "Maximum number of results to return")
	refundsListCmd.Flags().StringVar(&refListFrom, "from", "", "Return results starting from this refund ID (cursor pagination)")

	// list-all flags
	refundsListAllCmd.Flags().Int64Var(&refAllLimit, "limit", 50, "Maximum number of results to return")
	refundsListAllCmd.Flags().StringVar(&refAllFrom, "from", "", "Return results starting from this refund ID (cursor pagination)")

	// cancel flag
	refundsCancelCmd.Flags().BoolVar(&refCancelConfirm, "confirm", false, "Skip the confirmation prompt")

	refundsCmd.AddCommand(refundsCreateCmd)
	refundsCmd.AddCommand(refundsListCmd)
	refundsCmd.AddCommand(refundsListAllCmd)
	refundsCmd.AddCommand(refundsGetCmd)
	refundsCmd.AddCommand(refundsCancelCmd)
	rootCmd.AddCommand(refundsCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runRefundsCreate(cmd *cobra.Command, args []string) error {
	paymentID := args[0]

	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if val, cur, ok := input.Amount(jsonInput, "amount"); ok {
			if !cmd.Flags().Changed("amount") {
				refCreateAmount = val
			}
			if !cmd.Flags().Changed("currency") {
				refCreateCurrency = cur
			}
		}
		if v, ok := input.Str(jsonInput, "description"); ok && !cmd.Flags().Changed("description") {
			refCreateDescription = v
		}
	}

	// --amount and --currency must be supplied together.
	if (refCreateAmount == "") != (refCreateCurrency == "") {
		return fmt.Errorf("--amount and --currency must both be set (or both omitted for a full refund)")
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := &components.RefundRequest{
		Description: refCreateDescription,
		Amount: components.Amount{
			Currency: refCreateCurrency,
			Value:    refCreateAmount,
		},
	}

	resp, err := client.Refunds.Create(context.Background(), paymentID, nil, req)
	if err != nil {
		return fmt.Errorf("creating refund: %w", err)
	}
	ref := resp.GetEntityRefundResponse()
	if ref == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(ref)
	default:
		output.PrintTable(
			[]string{"ID", "STATUS", "AMOUNT", "DESCRIPTION", "PAYMENT ID"},
			[][]string{{
				ref.GetID(),
				string(ref.GetStatus()),
				formatAmount(ref.GetAmount()),
				ref.GetDescription(),
				derefOpt(ref.GetPaymentID()),
			}},
			!flagLive,
		)
	}
	return nil
}

func runRefundsList(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListRefundsRequest{
		PaymentID: args[0],
		Limit:     &refListLimit,
	}
	if refListFrom != "" {
		req.From = &refListFrom
	}

	resp, err := client.Refunds.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing refunds: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	embedded := resp.Object.GetEmbedded()
	refunds := embedded.GetRefunds()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(refunds))
		for _, r := range refunds {
			rows = append(rows, []string{
				r.GetID(),
				string(r.GetStatus()),
				formatAmount(r.GetAmount()),
				r.GetCreatedAt(),
				r.GetDescription(),
			})
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "AMOUNT", "CREATED AT", "DESCRIPTION"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runRefundsListAll(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListAllRefundsRequest{
		Limit: &refAllLimit,
	}
	if refAllFrom != "" {
		req.From = &refAllFrom
	}

	resp, err := client.Refunds.All(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing all refunds: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	allEmbedded := resp.Object.GetEmbedded()
	refunds := allEmbedded.GetRefunds()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(refunds))
		for _, r := range refunds {
			rows = append(rows, []string{
				r.GetID(),
				string(r.GetStatus()),
				formatAmount(r.GetAmount()),
				r.GetCreatedAt(),
				r.GetDescription(),
			})
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "AMOUNT", "CREATED AT", "DESCRIPTION"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runRefundsGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Refunds.Get(context.Background(), operations.GetRefundRequest{
		PaymentID: args[0],
		RefundID:  args[1],
	})
	if err != nil {
		return fmt.Errorf("getting refund: %w", err)
	}
	ref := resp.GetEntityRefundResponse()
	if ref == nil {
		return fmt.Errorf("refund not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(ref)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			refundDetailRows(ref),
			!flagLive,
		)
	}
	return nil
}

func runRefundsCancel(_ *cobra.Command, args []string) error {
	paymentID := args[0]
	refundID := args[1]

	if !refCancelConfirm {
		confirmed, err := prompt.Confirm(fmt.Sprintf("Cancel refund %s for payment %s?", refundID, paymentID))
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

	if _, err := client.Refunds.Cancel(context.Background(), paymentID, refundID, nil, nil); err != nil {
		return fmt.Errorf("canceling refund: %w", err)
	}

	fmt.Printf("✓ Refund %s canceled\n", refundID)
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// refundDetailRows converts an EntityRefundResponse into the key/value rows
// shown by `refunds get` in table mode.
func refundDetailRows(r *components.EntityRefundResponse) [][]string {
	row := func(k, v string) []string { return []string{k, v} }

	settlementAmt := "—"
	if a := r.GetSettlementAmount(); a != nil {
		settlementAmt = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}

	return [][]string{
		row("ID", r.GetID()),
		row("Mode", string(r.GetMode())),
		row("Status", string(r.GetStatus())),
		row("Amount", formatAmount(r.GetAmount())),
		row("Settlement Amount", settlementAmt),
		row("Description", r.GetDescription()),
		row("Payment ID", derefOpt(r.GetPaymentID())),
		row("Settlement ID", derefOpt(r.GetSettlementID())),
		row("Created At", r.GetCreatedAt()),
	}
}
