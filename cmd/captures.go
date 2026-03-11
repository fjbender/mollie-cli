package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fjbender/mollie-cli/internal/input"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"
	"github.com/spf13/cobra"
)

// ── flag value holders ───────────────────────────────────────────────────────

var (
	// create flags
	capCreateAmount      string
	capCreateCurrency    string
	capCreateDescription string
	capCreateMetadata    string

	// list flags
	capListLimit int64
	capListFrom  string
)

// ── command tree ─────────────────────────────────────────────────────────────

var capturesCmd = &cobra.Command{
	Use:   "captures",
	Short: "Manage Mollie captures",
}

var capturesCreateCmd = &cobra.Command{
	Use:   "create <payment-id>",
	Short: "Create a capture for an authorized payment",
	Args:  cobra.ExactArgs(1),
	RunE:  runCapturesCreate,
}

var capturesListCmd = &cobra.Command{
	Use:   "list <payment-id>",
	Short: "List captures for a payment",
	Args:  cobra.ExactArgs(1),
	RunE:  runCapturesList,
}

var capturesGetCmd = &cobra.Command{
	Use:   "get <payment-id> <capture-id>",
	Short: "Get a single capture",
	Args:  cobra.ExactArgs(2),
	RunE:  runCapturesGet,
}

func init() {
	// create flags
	capturesCreateCmd.Flags().StringVar(&capCreateAmount, "amount", "", "Capture amount, e.g. 10.00 (omit to capture the full authorized amount)")
	capturesCreateCmd.Flags().StringVar(&capCreateCurrency, "currency", "", "ISO 4217 currency code (required when --amount is set)")
	capturesCreateCmd.Flags().StringVar(&capCreateDescription, "description", "", "Description of the capture")
	capturesCreateCmd.Flags().StringVar(&capCreateMetadata, "metadata", "", "Arbitrary JSON metadata to attach to the capture")

	// list flags
	capturesListCmd.Flags().Int64Var(&capListLimit, "limit", 50, "Maximum number of results to return")
	capturesListCmd.Flags().StringVar(&capListFrom, "from", "", "Return results starting from this capture ID (cursor pagination)")

	capturesCmd.AddCommand(capturesCreateCmd)
	capturesCmd.AddCommand(capturesListCmd)
	capturesCmd.AddCommand(capturesGetCmd)
	rootCmd.AddCommand(capturesCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runCapturesCreate(cmd *cobra.Command, args []string) error {
	paymentID := args[0]

	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if val, cur, ok := input.Amount(jsonInput, "amount"); ok {
			if !cmd.Flags().Changed("amount") {
				capCreateAmount = val
			}
			if !cmd.Flags().Changed("currency") {
				capCreateCurrency = cur
			}
		}
		if v, ok := input.Str(jsonInput, "description"); ok && !cmd.Flags().Changed("description") {
			capCreateDescription = v
		}
		if v, ok := input.RawJSON(jsonInput, "metadata"); ok && !cmd.Flags().Changed("metadata") {
			capCreateMetadata = v
		}
	}

	// --amount and --currency must be supplied together.
	if (capCreateAmount == "") != (capCreateCurrency == "") {
		return fmt.Errorf("--amount and --currency must both be set (or both omitted to capture the full amount)")
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	body := &components.EntityCapture{}
	if capCreateDescription != "" {
		body.Description = &capCreateDescription
	}
	if capCreateAmount != "" {
		body.Amount = &components.AmountNullable{
			Currency: capCreateCurrency,
			Value:    capCreateAmount,
		}
	}
	if capCreateMetadata != "" {
		meta, err := parseMetadata(capCreateMetadata)
		if err != nil {
			return fmt.Errorf("invalid --metadata: %w", err)
		}
		body.Metadata = &meta
	}

	resp, err := client.Captures.Create(context.Background(), paymentID, nil, body)
	if err != nil {
		return fmt.Errorf("creating capture: %w", err)
	}
	cap := resp.GetCaptureResponse()
	if cap == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(cap)
	default:
		amt := "—"
		if a := cap.GetAmount(); a != nil {
			amt = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "AMOUNT", "PAYMENT ID"},
			[][]string{{
				cap.GetID(),
				string(cap.GetStatus()),
				amt,
				cap.GetPaymentID(),
			}},
			!flagLive,
		)
	}
	return nil
}

func runCapturesList(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListCapturesRequest{
		PaymentID: args[0],
		Limit:     &capListLimit,
	}
	if capListFrom != "" {
		req.From = &capListFrom
	}

	resp, err := client.Captures.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing captures: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	embedded := resp.Object.GetEmbedded()
	captures := embedded.GetCaptures()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(captures))
		for _, c := range captures {
			amt := "—"
			if a := c.GetAmount(); a != nil {
				amt = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
			}
			rows = append(rows, []string{
				c.GetID(),
				string(c.GetStatus()),
				amt,
				c.GetCreatedAt(),
				derefOpt(c.GetDescription()),
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

func runCapturesGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Captures.Get(context.Background(), operations.GetCaptureRequest{
		PaymentID: args[0],
		CaptureID: args[1],
	})
	if err != nil {
		return fmt.Errorf("getting capture: %w", err)
	}
	cap := resp.GetCaptureResponse()
	if cap == nil {
		return fmt.Errorf("capture not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(cap)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			captureDetailRows(cap),
			!flagLive,
		)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// captureDetailRows converts a CaptureResponse into the key/value rows shown
// by `captures get` in table mode.
func captureDetailRows(c *components.CaptureResponse) [][]string {
	row := func(k, v string) []string { return []string{k, v} }

	amt := "—"
	if a := c.GetAmount(); a != nil {
		amt = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}

	settlementAmt := "—"
	if a := c.GetSettlementAmount(); a != nil {
		settlementAmt = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}

	metaStr := "—"
	if m := c.GetMetadata(); m != nil {
		if b, err := json.Marshal(m); err == nil {
			metaStr = string(b)
		}
	}

	return [][]string{
		row("ID", c.GetID()),
		row("Mode", string(c.GetMode())),
		row("Status", string(c.GetStatus())),
		row("Amount", amt),
		row("Settlement Amount", settlementAmt),
		row("Description", derefOpt(c.GetDescription())),
		row("Payment ID", c.GetPaymentID()),
		row("Shipment ID", derefOpt(c.GetShipmentID())),
		row("Settlement ID", derefOpt(c.GetSettlementID())),
		row("Created At", c.GetCreatedAt()),
		row("Metadata", metaStr),
	}
}
