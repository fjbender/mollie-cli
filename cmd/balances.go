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
	// list flags
	balListLimit    int64
	balListFrom     string
	balListCurrency string

	// report flags
	balReportFrom     string
	balReportUntil    string
	balReportGrouping string

	// transactions flags
	balTxLimit int64
	balTxFrom  string
)

// ── command tree ─────────────────────────────────────────────────────────────

var balancesCmd = &cobra.Command{
	Use:   "balances",
	Short: "Manage Mollie balances",
}

var balancesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all balances",
	RunE:  runBalancesList,
}

var balancesGetCmd = &cobra.Command{
	Use:   "get <balance-id>",
	Short: "Get a specific balance",
	Args:  cobra.ExactArgs(1),
	RunE:  runBalancesGet,
}

var balancesPrimaryCmd = &cobra.Command{
	Use:   "primary",
	Short: "Get the primary balance",
	RunE:  runBalancesPrimary,
}

var balancesReportCmd = &cobra.Command{
	Use:   "report <balance-id>",
	Short: "Get a balance report (requires --from and --until date flags)",
	Args:  cobra.ExactArgs(1),
	RunE:  runBalancesReport,
}

var balancesTransactionsCmd = &cobra.Command{
	Use:   "transactions <balance-id>",
	Short: "List transactions for a balance",
	Args:  cobra.ExactArgs(1),
	RunE:  runBalancesTransactions,
}

func init() {
	// list flags
	balancesListCmd.Flags().Int64Var(&balListLimit, "limit", 50, "Maximum number of results to return")
	balancesListCmd.Flags().StringVar(&balListFrom, "from", "", "Return results starting from this balance ID (cursor pagination)")
	balancesListCmd.Flags().StringVar(&balListCurrency, "currency", "", "Only return balances with this currency, e.g. EUR")

	// report flags
	balancesReportCmd.Flags().StringVar(&balReportFrom, "from", "", "Start date of the report in YYYY-MM-DD format (required)")
	balancesReportCmd.Flags().StringVar(&balReportUntil, "until", "", "End date of the report in YYYY-MM-DD format (required)")
	balancesReportCmd.Flags().StringVar(&balReportGrouping, "grouping", "", "Grouping: statusBalances or transactionCategories")
	balancesReportCmd.MarkFlagRequired("from")  //nolint:errcheck
	balancesReportCmd.MarkFlagRequired("until") //nolint:errcheck

	// transactions flags
	balancesTransactionsCmd.Flags().Int64Var(&balTxLimit, "limit", 50, "Maximum number of results to return")
	balancesTransactionsCmd.Flags().StringVar(&balTxFrom, "from", "", "Return results starting from this transaction ID (cursor pagination)")

	balancesCmd.AddCommand(balancesListCmd)
	balancesCmd.AddCommand(balancesGetCmd)
	balancesCmd.AddCommand(balancesPrimaryCmd)
	balancesCmd.AddCommand(balancesReportCmd)
	balancesCmd.AddCommand(balancesTransactionsCmd)
	rootCmd.AddCommand(balancesCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runBalancesList(_ *cobra.Command, _ []string) error {
	// Balances does not support testmode or profileId; use an org-level client.
	client, err := mollieclient.NewOrganizationClient(cfg, flagAPIKey)
	if err != nil {
		return err
	}

	req := operations.ListBalancesRequest{
		Limit: &balListLimit,
	}
	if balListFrom != "" {
		req.From = &balListFrom
	}
	if balListCurrency != "" {
		req.Currency = &balListCurrency
	}

	resp, err := client.Balances.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing balances: %w", err)
	}
	body := resp.GetObject()
	if body == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	emb := body.GetEmbedded()
	balList := (&emb).GetBalances()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(balList)
	default:
		rows := make([][]string, 0, len(balList))
		for _, b := range balList {
			avl := b.GetAvailableAmount()
			pnd := b.GetPendingAmount()
			rows = append(rows, []string{
				b.GetID(),
				string(b.GetCurrency()),
				string(b.GetStatus()),
				(&avl).GetCurrency() + " " + (&avl).GetValue(),
				(&pnd).GetCurrency() + " " + (&pnd).GetValue(),
			})
		}
		output.PrintTable(
			[]string{"ID", "CURRENCY", "STATUS", "AVAILABLE", "PENDING"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runBalancesGet(_ *cobra.Command, args []string) error {
	balanceID := args[0]

	// Balances does not support testmode or profileId; use an org-level client.
	client, err := mollieclient.NewOrganizationClient(cfg, flagAPIKey)
	if err != nil {
		return err
	}

	resp, err := client.Balances.Get(context.Background(), balanceID, nil, nil)
	if err != nil {
		return fmt.Errorf("getting balance: %w", err)
	}
	b := resp.GetEntityBalance()
	if b == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(b)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			balanceEntityRows(b),
			!flagLive,
		)
	}
	return nil
}

func runBalancesPrimary(_ *cobra.Command, _ []string) error {
	// Balances does not support testmode or profileId; use an org-level client.
	client, err := mollieclient.NewOrganizationClient(cfg, flagAPIKey)
	if err != nil {
		return err
	}

	resp, err := client.Balances.GetPrimary(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("getting primary balance: %w", err)
	}
	b := resp.GetEntityBalance()
	if b == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(b)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			balanceEntityRows(b),
			!flagLive,
		)
	}
	return nil
}

func runBalancesReport(_ *cobra.Command, args []string) error {
	balanceID := args[0]

	// Balances does not support testmode or profileId; use an org-level client.
	client, err := mollieclient.NewOrganizationClient(cfg, flagAPIKey)
	if err != nil {
		return err
	}

	req := operations.GetBalanceReportRequest{
		BalanceID: balanceID,
		From:      balReportFrom,
		Until:     balReportUntil,
	}

	resp, err := client.Balances.GetReport(context.Background(), req)
	if err != nil {
		return fmt.Errorf("getting balance report: %w", err)
	}
	r := resp.GetEntityBalanceReport()
	if r == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(r)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"Balance ID", r.GetBalanceID()},
				{"From", r.GetFrom()},
				{"Until", r.GetUntil()},
				{"Timezone", r.GetTimeZone()},
				{"Grouping", string(r.GetGrouping())},
			},
			!flagLive,
		)
		fmt.Println("\nUse --output json for the full report including totals.")
	}
	return nil
}

func runBalancesTransactions(_ *cobra.Command, args []string) error {
	balanceID := args[0]

	// Balances does not support testmode or profileId; use an org-level client.
	client, err := mollieclient.NewOrganizationClient(cfg, flagAPIKey)
	if err != nil {
		return err
	}

	req := operations.ListBalanceTransactionsRequest{
		BalanceID: balanceID,
		Limit:     &balTxLimit,
	}
	if balTxFrom != "" {
		req.From = &balTxFrom
	}

	resp, err := client.Balances.ListTransactions(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing balance transactions: %w", err)
	}
	body := resp.GetObject()
	if body == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	emb := body.GetEmbedded()
	txs := (&emb).GetBalanceTransactions()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(txs)
	default:
		rows := make([][]string, 0, len(txs))
		for _, t := range txs {
			resAmt := t.GetResultAmount()
			rows = append(rows, []string{
				t.GetID(),
				string(t.GetType()),
				resAmt.Currency + " " + resAmt.Value,
				t.GetCreatedAt(),
			})
		}
		output.PrintTable(
			[]string{"ID", "TYPE", "RESULT AMOUNT", "CREATED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// balanceEntityRows builds key–value rows for a single EntityBalance detail view.
func balanceEntityRows(b *components.EntityBalance) [][]string {
	avl := b.GetAvailableAmount()
	pnd := b.GetPendingAmount()
	return [][]string{
		{"ID", b.GetID()},
		{"Description", b.GetDescription()},
		{"Currency", string(b.GetCurrency())},
		{"Status", string(b.GetStatus())},
		{"Available Amount", avl.GetCurrency() + " " + avl.GetValue()},
		{"Pending Amount", pnd.GetCurrency() + " " + pnd.GetValue()},
		{"Created At", b.GetCreatedAt()},
	}
}
