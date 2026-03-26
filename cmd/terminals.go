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
	terminalListLimit int64
	terminalListFrom  string
)

// ── command tree ─────────────────────────────────────────────────────────────

var terminalsCmd = &cobra.Command{
	Use:   "terminals",
	Short: "Inspect Mollie POS terminals",
}

var terminalsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all POS terminals",
	RunE:  runTerminalsList,
}

var terminalsGetCmd = &cobra.Command{
	Use:   "get <terminal-id>",
	Short: "Get a POS terminal by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runTerminalsGet,
}

func init() {
	terminalsListCmd.Flags().Int64Var(&terminalListLimit, "limit", 50, "Maximum number of terminals to return")
	terminalsListCmd.Flags().StringVar(&terminalListFrom, "from", "", "Cursor: return results from this terminal ID onwards")

	terminalsCmd.AddCommand(terminalsListCmd)
	terminalsCmd.AddCommand(terminalsGetCmd)
	rootCmd.AddCommand(terminalsCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runTerminalsList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListTerminalsRequest{
		Limit: &terminalListLimit,
	}
	if terminalListFrom != "" {
		req.From = &terminalListFrom
	}

	resp, err := client.Terminals.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing terminals: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	embedded := resp.Object.GetEmbedded()
	terminals := embedded.GetTerminals()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(terminals))
		for _, t := range terminals {
			rows = append(rows, []string{
				t.GetID(),
				string(t.GetStatus()),
				terminalBrandStr(t.GetBrand()),
				terminalModelStr(t.GetModel()),
				t.GetDescription(),
				t.GetCurrency(),
			})
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "BRAND", "MODEL", "DESCRIPTION", "CURRENCY"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runTerminalsGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Terminals.Get(context.Background(), args[0], nil, nil)
	if err != nil {
		return fmt.Errorf("getting terminal: %w", err)
	}
	t := resp.GetEntityTerminal()
	if t == nil {
		return fmt.Errorf("terminal not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(t)
	default:
		row := func(k, v string) []string { return []string{k, v} }
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				row("ID", t.GetID()),
				row("Status", string(t.GetStatus())),
				row("Brand", terminalBrandStr(t.GetBrand())),
				row("Model", terminalModelStr(t.GetModel())),
				row("Description", t.GetDescription()),
				row("Currency", t.GetCurrency()),
				row("Serial Number", derefOpt(t.GetSerialNumber())),
				row("Profile ID", t.GetProfileID()),
				row("Mode", string(t.GetMode())),
				row("Created At", t.GetCreatedAt()),
				row("Updated At", t.GetUpdatedAt()),
			},
			!flagLive,
		)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func terminalBrandStr(b *components.TerminalBrand) string {
	if b == nil {
		return "\u2014"
	}
	return string(*b)
}

func terminalModelStr(m *components.TerminalModel) string {
	if m == nil {
		return "\u2014"
	}
	return string(*m)
}
