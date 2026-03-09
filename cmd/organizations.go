package cmd

import (
	"context"
	"fmt"

	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/spf13/cobra"
)

// ── command tree ─────────────────────────────────────────────────────────────

var organizationsCmd = &cobra.Command{
	Use:   "organizations",
	Short: "Manage Mollie organizations",
}

var organizationsCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Get the organization linked to the current API key",
	RunE:  runOrganizationsCurrent,
}

var organizationsGetCmd = &cobra.Command{
	Use:   "get <organization-id>",
	Short: "Get an organization by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrganizationsGet,
}

func init() {
	organizationsCmd.AddCommand(organizationsCurrentCmd)
	organizationsCmd.AddCommand(organizationsGetCmd)
	rootCmd.AddCommand(organizationsCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runOrganizationsCurrent(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Organizations.GetCurrent(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("getting current organization: %w", err)
	}
	org := resp.GetEntityOrganization()
	if org == nil {
		return fmt.Errorf("organization not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(org)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			organizationDetailRows(org),
			!flagLive,
		)
	}
	return nil
}

func runOrganizationsGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Organizations.Get(context.Background(), args[0], nil, nil)
	if err != nil {
		return fmt.Errorf("getting organization: %w", err)
	}
	org := resp.GetEntityOrganization()
	if org == nil {
		return fmt.Errorf("organization not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(org)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			organizationDetailRows(org),
			!flagLive,
		)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func organizationDetailRows(org *components.EntityOrganization) [][]string {
	row := func(k, v string) []string { return []string{k, v} }

	addr := org.GetAddress()
	addressStr := fmt.Sprintf("%s, %s %s, %s",
		addr.GetStreetAndNumber(),
		addr.GetPostalCode(),
		addr.GetCity(),
		addr.GetCountry(),
	)

	locale := ""
	if l := org.GetLocale(); l != nil {
		locale = string(*l)
	}

	vatNumber := ""
	if v := org.GetVatNumber(); v != nil {
		vatNumber = *v
	}

	return [][]string{
		row("ID", org.GetID()),
		row("Name", org.GetName()),
		row("Email", org.GetEmail()),
		row("Locale", locale),
		row("Address", addressStr),
		row("Registration Number", org.GetRegistrationNumber()),
		row("VAT Number", vatNumber),
	}
}
