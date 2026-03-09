package cmd

import (
	"context"
	"fmt"

	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/mollie/mollie-api-golang/models/operations"
	"github.com/spf13/cobra"
)

// ── flag value holders ───────────────────────────────────────────────────────

var (
	// list flags (per-payment)
	cbListLimit int64
	cbListFrom  string

	// list-all flags
	cbAllLimit int64
	cbAllFrom  string
)

// ── command tree ─────────────────────────────────────────────────────────────

var chargebacksCmd = &cobra.Command{
	Use:   "chargebacks",
	Short: "Inspect Mollie chargebacks",
}

var chargebacksListCmd = &cobra.Command{
	Use:   "list <payment-id>",
	Short: "List chargebacks for a specific payment",
	Args:  cobra.ExactArgs(1),
	RunE:  runChargebacksList,
}

var chargebacksListAllCmd = &cobra.Command{
	Use:   "list-all",
	Short: "List all chargebacks across all payments",
	RunE:  runChargebacksListAll,
}

var chargebacksGetCmd = &cobra.Command{
	Use:   "get <payment-id> <chargeback-id>",
	Short: "Get a single chargeback",
	Args:  cobra.ExactArgs(2),
	RunE:  runChargebacksGet,
}

func init() {
	// list flags
	chargebacksListCmd.Flags().Int64Var(&cbListLimit, "limit", 50, "Maximum number of results to return")
	chargebacksListCmd.Flags().StringVar(&cbListFrom, "from", "", "Return results starting from this chargeback ID (cursor pagination)")

	// list-all flags
	chargebacksListAllCmd.Flags().Int64Var(&cbAllLimit, "limit", 50, "Maximum number of results to return")
	chargebacksListAllCmd.Flags().StringVar(&cbAllFrom, "from", "", "Return results starting from this chargeback ID (cursor pagination)")

	chargebacksCmd.AddCommand(chargebacksListCmd)
	chargebacksCmd.AddCommand(chargebacksListAllCmd)
	chargebacksCmd.AddCommand(chargebacksGetCmd)
	rootCmd.AddCommand(chargebacksCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runChargebacksList(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListChargebacksRequest{
		PaymentID: args[0],
		Limit:     &cbListLimit,
	}
	if cbListFrom != "" {
		req.From = &cbListFrom
	}

	resp, err := client.Chargebacks.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing chargebacks: %w", err)
	}
	body := resp.GetObject()
	if body == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	emb := body.GetEmbedded()
	list := (&emb).GetChargebacks()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(list)
	default:
		rows := make([][]string, 0, len(list))
		for _, cb := range list {
			amt := cb.GetAmount()
			reasonCode := "-"
			if r := cb.GetReason(); r != nil {
				reasonCode = r.GetCode()
			}
			rows = append(rows, []string{
				cb.GetID(),
				amt.Currency + " " + amt.Value,
				cb.GetPaymentID(),
				reasonCode,
				cb.GetCreatedAt(),
			})
		}
		output.PrintTable(
			[]string{"ID", "AMOUNT", "PAYMENT ID", "REASON CODE", "CREATED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runChargebacksListAll(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListAllChargebacksRequest{
		Limit: &cbAllLimit,
	}
	if cbAllFrom != "" {
		req.From = &cbAllFrom
	}

	resp, err := client.Chargebacks.All(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing all chargebacks: %w", err)
	}
	body := resp.GetObject()
	if body == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	emb := body.GetEmbedded()
	list := (&emb).GetChargebacks()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(list)
	default:
		rows := make([][]string, 0, len(list))
		for _, cb := range list {
			amt := cb.GetAmount()
			reasonCode := "-"
			if r := cb.GetReason(); r != nil {
				reasonCode = r.GetCode()
			}
			rows = append(rows, []string{
				cb.GetID(),
				amt.Currency + " " + amt.Value,
				cb.GetPaymentID(),
				reasonCode,
				cb.GetCreatedAt(),
			})
		}
		output.PrintTable(
			[]string{"ID", "AMOUNT", "PAYMENT ID", "REASON CODE", "CREATED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runChargebacksGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.GetChargebackRequest{
		PaymentID:    args[0],
		ChargebackID: args[1],
	}

	resp, err := client.Chargebacks.Get(context.Background(), req)
	if err != nil {
		return fmt.Errorf("getting chargeback: %w", err)
	}
	cb := resp.GetEntityChargeback()
	if cb == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(cb)
	default:
		amt := cb.GetAmount()
		reasonCode, reasonDesc := "-", "-"
		if r := cb.GetReason(); r != nil {
			reasonCode = r.GetCode()
			reasonDesc = r.GetDescription()
		}
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", cb.GetID()},
				{"Amount", amt.Currency + " " + amt.Value},
				{"Payment ID", cb.GetPaymentID()},
				{"Settlement ID", derefOpt(cb.GetSettlementID())},
				{"Reason Code", reasonCode},
				{"Reason Description", reasonDesc},
				{"Created At", cb.GetCreatedAt()},
				{"Reversed At", derefOpt(cb.GetReversedAt())},
			},
			!flagLive,
		)
	}
	return nil
}

// chargebackRows is intentionally not used — rows are built inline above.