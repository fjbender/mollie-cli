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
	// list flags
	setListLimit int64
	setListFrom  string

	// sub-resource list flags (payments, refunds, captures, chargebacks)
	setSubLimit int64
	setSubFrom  string
)

// ── command tree ─────────────────────────────────────────────────────────────

var settlementsCmd = &cobra.Command{
	Use:   "settlements",
	Short: "Manage Mollie settlements",
}

var settlementsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List settlements",
	RunE:  runSettlementsList,
}

var settlementsGetCmd = &cobra.Command{
	Use:   "get <settlement-id>",
	Short: "Get a settlement",
	Args:  cobra.ExactArgs(1),
	RunE:  runSettlementsGet,
}

var settlementsOpenCmd = &cobra.Command{
	Use:   "open",
	Short: "Get the open (not-yet-settled) settlement",
	RunE:  runSettlementsOpen,
}

var settlementsNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Get the next scheduled settlement",
	RunE:  runSettlementsNext,
}

var settlementsPaymentsCmd = &cobra.Command{
	Use:   "payments <settlement-id>",
	Short: "List payments included in a settlement",
	Args:  cobra.ExactArgs(1),
	RunE:  runSettlementsPayments,
}

var settlementsRefundsCmd = &cobra.Command{
	Use:   "refunds <settlement-id>",
	Short: "List refunds included in a settlement",
	Args:  cobra.ExactArgs(1),
	RunE:  runSettlementsRefunds,
}

var settlementsCapturesCmd = &cobra.Command{
	Use:   "captures <settlement-id>",
	Short: "List captures included in a settlement",
	Args:  cobra.ExactArgs(1),
	RunE:  runSettlementsCaptures,
}

var settlementsChargebacksCmd = &cobra.Command{
	Use:   "chargebacks <settlement-id>",
	Short: "List chargebacks included in a settlement",
	Args:  cobra.ExactArgs(1),
	RunE:  runSettlementsChargebacks,
}

func init() {
	// list flags
	settlementsListCmd.Flags().Int64Var(&setListLimit, "limit", 50, "Maximum number of results to return")
	settlementsListCmd.Flags().StringVar(&setListFrom, "from", "", "Return results starting from this settlement ID (cursor pagination)")

	// sub-resource flags (shared across payments/refunds/captures/chargebacks sub-commands)
	for _, cmd := range []*cobra.Command{
		settlementsPaymentsCmd,
		settlementsRefundsCmd,
		settlementsCapturesCmd,
		settlementsChargebacksCmd,
	} {
		cmd.Flags().Int64Var(&setSubLimit, "limit", 50, "Maximum number of results to return")
		cmd.Flags().StringVar(&setSubFrom, "from", "", "Return results starting from this ID (cursor pagination)")
	}

	settlementsCmd.AddCommand(settlementsListCmd)
	settlementsCmd.AddCommand(settlementsGetCmd)
	settlementsCmd.AddCommand(settlementsOpenCmd)
	settlementsCmd.AddCommand(settlementsNextCmd)
	settlementsCmd.AddCommand(settlementsPaymentsCmd)
	settlementsCmd.AddCommand(settlementsRefundsCmd)
	settlementsCmd.AddCommand(settlementsCapturesCmd)
	settlementsCmd.AddCommand(settlementsChargebacksCmd)
	rootCmd.AddCommand(settlementsCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runSettlementsList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListSettlementsRequest{
		Limit: &setListLimit,
	}
	if setListFrom != "" {
		req.From = &setListFrom
	}

	resp, err := client.Settlements.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing settlements: %w", err)
	}
	body := resp.GetObject()
	if body == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	emb := body.GetEmbedded()
	list := (&emb).GetSettlements()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(list)
	default:
		rows := make([][]string, 0, len(list))
		for _, s := range list {
			amt := s.GetAmount()
			rows = append(rows, []string{
				s.GetID(),
				derefOpt(s.GetReference()),
				string(s.GetStatus()),
				amt.GetCurrency() + " " + amt.GetValue(),
				derefOpt(s.GetSettledAt()),
			})
		}
		output.PrintTable(
			[]string{"ID", "REFERENCE", "STATUS", "AMOUNT", "SETTLED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runSettlementsGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Settlements.Get(context.Background(), args[0], nil)
	if err != nil {
		return fmt.Errorf("getting settlement: %w", err)
	}
	s := resp.GetEntitySettlement()
	if s == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(s)
	default:
		amt := s.GetAmount()
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", s.GetID()},
				{"Reference", derefOpt(s.GetReference())},
				{"Status", string(s.GetStatus())},
				{"Amount", amt.GetCurrency() + " " + amt.GetValue()},
				{"Balance ID", s.GetBalanceID()},
				{"Invoice ID", derefOpt(s.GetInvoiceID())},
				{"Created At", derefOpt(s.GetCreatedAt())},
				{"Settled At", derefOpt(s.GetSettledAt())},
			},
			!flagLive,
		)
	}
	return nil
}

func runSettlementsOpen(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Settlements.GetOpen(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("getting open settlement: %w", err)
	}
	s := resp.GetEntitySettlement()
	if s == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(s)
	default:
		amt := s.GetAmount()
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", s.GetID()},
				{"Reference", derefOpt(s.GetReference())},
				{"Status", string(s.GetStatus())},
				{"Amount", amt.GetCurrency() + " " + amt.GetValue()},
				{"Balance ID", s.GetBalanceID()},
				{"Created At", derefOpt(s.GetCreatedAt())},
			},
			!flagLive,
		)
	}
	return nil
}

func runSettlementsNext(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Settlements.GetNext(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("getting next settlement: %w", err)
	}
	s := resp.GetEntitySettlement()
	if s == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(s)
	default:
		amt := s.GetAmount()
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", s.GetID()},
				{"Reference", derefOpt(s.GetReference())},
				{"Status", string(s.GetStatus())},
				{"Amount", amt.GetCurrency() + " " + amt.GetValue()},
				{"Balance ID", s.GetBalanceID()},
				{"Created At", derefOpt(s.GetCreatedAt())},
			},
			!flagLive,
		)
	}
	return nil
}

func runSettlementsPayments(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListSettlementPaymentsRequest{
		SettlementID: args[0],
		Limit:        &setSubLimit,
	}
	if setSubFrom != "" {
		req.From = &setSubFrom
	}

	resp, err := client.Settlements.ListPayments(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing settlement payments: %w", err)
	}
	body := resp.GetObject()
	if body == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	emb := body.GetEmbedded()
	list := (&emb).GetPayments()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(list)
	default:
		rows := make([][]string, 0, len(list))
		for _, p := range list {
			amt := p.GetAmount()
			rows = append(rows, []string{
				p.GetID(),
				string(p.GetStatus()),
				amt.Currency + " " + amt.Value,
				p.GetDescription(),
			})
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "AMOUNT", "DESCRIPTION"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runSettlementsRefunds(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListSettlementRefundsRequest{
		SettlementID: args[0],
		Limit:        &setSubLimit,
	}
	if setSubFrom != "" {
		req.From = &setSubFrom
	}

	resp, err := client.Settlements.ListRefunds(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing settlement refunds: %w", err)
	}
	body := resp.GetObject()
	if body == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	emb := body.GetEmbedded()
	list := (&emb).GetRefunds()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(list)
	default:
		rows := make([][]string, 0, len(list))
		for _, r := range list {
			amt := r.GetAmount()
			rows = append(rows, []string{
				r.GetID(),
				string(r.GetStatus()),
				amt.Currency + " " + amt.Value,
				r.GetDescription(),
				derefOpt(r.GetPaymentID()),
			})
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "AMOUNT", "DESCRIPTION", "PAYMENT ID"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runSettlementsCaptures(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListSettlementCapturesRequest{
		SettlementID: args[0],
		Limit:        &setSubLimit,
	}
	if setSubFrom != "" {
		req.From = &setSubFrom
	}

	resp, err := client.Settlements.ListCaptures(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing settlement captures: %w", err)
	}
	body := resp.GetObject()
	if body == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	emb := body.GetEmbedded()
	list := (&emb).GetCaptures()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(list)
	default:
		rows := make([][]string, 0, len(list))
		for _, c := range list {
			amtStr := "-"
			if a := c.GetAmount(); a != nil {
				amtStr = a.Currency + " " + a.Value
			}
			rows = append(rows, []string{
				c.GetID(),
				string(c.GetStatus()),
				amtStr,
				derefOpt(c.GetDescription()),
			})
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "AMOUNT", "DESCRIPTION"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runSettlementsChargebacks(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListSettlementChargebacksRequest{
		SettlementID: args[0],
		Limit:        &setSubLimit,
	}
	if setSubFrom != "" {
		req.From = &setSubFrom
	}

	resp, err := client.Settlements.ListChargebacks(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing settlement chargebacks: %w", err)
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
