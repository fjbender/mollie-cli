package cmd

import (
	"context"
	"fmt"

	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"
	"github.com/spf13/cobra"
)

// ── flag value holders ───────────────────────────────────────────────────────

var (
	invListLimit     int64
	invListFrom      string
	invListReference string
	invListYear      string
)

// ── command tree ─────────────────────────────────────────────────────────────

var invoicesCmd = &cobra.Command{
	Use:   "invoices",
	Short: "Manage Mollie invoices",
}

var invoicesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List invoices",
	RunE:  runInvoicesList,
}

var invoicesGetCmd = &cobra.Command{
	Use:   "get <invoice-id>",
	Short: "Get an invoice",
	Args:  cobra.ExactArgs(1),
	RunE:  runInvoicesGet,
}

func init() {
	// list flags
	invoicesListCmd.Flags().Int64Var(&invListLimit, "limit", 50, "Maximum number of results to return")
	invoicesListCmd.Flags().StringVar(&invListFrom, "from", "", "Return results starting from this invoice ID (cursor pagination)")
	invoicesListCmd.Flags().StringVar(&invListReference, "reference", "", "Filter by a specific invoice reference (e.g. 2024.10000)")
	invoicesListCmd.Flags().StringVar(&invListYear, "year", "", "Filter by year (e.g. 2024)")

	invoicesCmd.AddCommand(invoicesListCmd)
	invoicesCmd.AddCommand(invoicesGetCmd)
	rootCmd.AddCommand(invoicesCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runInvoicesList(_ *cobra.Command, _ []string) error {
	// Invoices does not support testmode or profileId; use an org-level client.
	client, err := mollieclient.NewOrganizationClient(cfg, flagAPIKey)
	if err != nil {
		return err
	}

	req := operations.ListInvoicesRequest{
		Limit: &invListLimit,
	}
	if invListFrom != "" {
		req.From = &invListFrom
	}
	if invListReference != "" {
		req.Reference = &invListReference
	}
	if invListYear != "" {
		req.Year = &invListYear
	}

	resp, err := client.Invoices.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing invoices: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	embedded := resp.Object.GetEmbedded()
	invoices := embedded.Invoices

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(invoices))
		for _, inv := range invoices {
			grossAmount := inv.GetGrossAmount()
			rows = append(rows, []string{
				inv.GetID(),
				inv.GetReference(),
				string(inv.GetStatus()),
				fmt.Sprintf("%s %s", grossAmount.GetCurrency(), grossAmount.GetValue()),
				inv.GetIssuedAt(),
			})
		}
		output.PrintTable(
			[]string{"ID", "REFERENCE", "STATUS", "GROSS AMOUNT", "ISSUED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runInvoicesGet(_ *cobra.Command, args []string) error {
	// Invoices does not support testmode or profileId; use an org-level client.
	client, err := mollieclient.NewOrganizationClient(cfg, flagAPIKey)
	if err != nil {
		return err
	}

	resp, err := client.Invoices.Get(context.Background(), args[0], nil)
	if err != nil {
		return fmt.Errorf("getting invoice: %w", err)
	}
	inv := resp.GetEntityInvoice()
	if inv == nil {
		return fmt.Errorf("invoice not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(inv)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			invoiceDetailRows(inv),
			!flagLive,
		)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func invoiceDetailRows(inv *components.EntityInvoice) [][]string {
	row := func(k, v string) []string { return []string{k, v} }

	net := inv.GetNetAmount()
	vat := inv.GetVatAmount()
	gross := inv.GetGrossAmount()

	paidAt := ""
	if v := inv.GetPaidAt(); v != nil {
		paidAt = *v
	}
	dueAt := ""
	if v := inv.GetDueAt(); v != nil {
		dueAt = *v
	}
	vatNumber := ""
	if v := inv.GetVatNumber(); v != nil {
		vatNumber = *v
	}

	return [][]string{
		row("ID", inv.GetID()),
		row("Reference", inv.GetReference()),
		row("Status", string(inv.GetStatus())),
		row("VAT Number", vatNumber),
		row("Net Amount", fmt.Sprintf("%s %s", net.GetCurrency(), net.GetValue())),
		row("VAT Amount", fmt.Sprintf("%s %s", vat.GetCurrency(), vat.GetValue())),
		row("Gross Amount", fmt.Sprintf("%s %s", gross.GetCurrency(), gross.GetValue())),
		row("Issued At", inv.GetIssuedAt()),
		row("Paid At", paidAt),
		row("Due At", dueAt),
	}
}
