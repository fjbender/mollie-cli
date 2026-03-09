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
	methodListSequenceType        string
	methodListLocale              string
	methodListAmountValue         string
	methodListAmountCurrency      string
	methodListBillingCountry      string
	methodListIncludeWallets      string
	methodListOrderLineCategories string
	methodListInclude             string

	// list-all flags
	methodListAllLocale         string
	methodListAllAmountValue    string
	methodListAllAmountCurrency string
	methodListAllInclude        string
	methodListAllSequenceType   string

	// get flags
	methodGetLocale       string
	methodGetCurrency     string
	methodGetInclude      string
	methodGetSequenceType string
)

// ── command tree ─────────────────────────────────────────────────────────────

var methodsCmd = &cobra.Command{
	Use:   "methods",
	Short: "Inspect Mollie payment methods",
}

var methodsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List enabled payment methods for the profile",
	RunE:  runMethodsList,
}

var methodsListAllCmd = &cobra.Command{
	Use:   "list-all",
	Short: "List all available payment methods",
	RunE:  runMethodsListAll,
}

var methodsGetCmd = &cobra.Command{
	Use:   "get <method-id>",
	Short: "Get details for a specific payment method",
	Args:  cobra.ExactArgs(1),
	RunE:  runMethodsGet,
}

func init() {
	// list filters
	methodsListCmd.Flags().StringVar(&methodListSequenceType, "sequence-type", "", "Filter by sequence type (oneoff, first, recurring)")
	methodsListCmd.Flags().StringVar(&methodListLocale, "locale", "", "Locale for sorting and translation (e.g. en_US, de_DE)")
	methodsListCmd.Flags().StringVar(&methodListAmountValue, "amount-value", "", "Filter by amount value (e.g. 10.00); requires --amount-currency")
	methodsListCmd.Flags().StringVar(&methodListAmountCurrency, "amount-currency", "", "Currency for amount filter (e.g. EUR)")
	methodsListCmd.Flags().StringVar(&methodListBillingCountry, "billing-country", "", "ISO 3166-1 alpha-2 billing country (e.g. DE)")
	methodsListCmd.Flags().StringVar(&methodListIncludeWallets, "include-wallets", "", "Wallets to include (e.g. applepay)")
	methodsListCmd.Flags().StringVar(&methodListOrderLineCategories, "order-line-categories", "", "Comma-separated order line categories (e.g. eco,meal)")
	methodsListCmd.Flags().StringVar(&methodListInclude, "include", "", "Additional data to include")

	// list-all filters
	methodsListAllCmd.Flags().StringVar(&methodListAllLocale, "locale", "", "Locale for sorting and translation (e.g. en_US, de_DE)")
	methodsListAllCmd.Flags().StringVar(&methodListAllAmountValue, "amount-value", "", "Filter by amount value (e.g. 10.00); requires --amount-currency")
	methodsListAllCmd.Flags().StringVar(&methodListAllAmountCurrency, "amount-currency", "", "Currency for amount filter (e.g. EUR)")
	methodsListAllCmd.Flags().StringVar(&methodListAllInclude, "include", "", "Additional data to include")
	methodsListAllCmd.Flags().StringVar(&methodListAllSequenceType, "sequence-type", "", "Filter by sequence type (oneoff, first, recurring)")

	// get filters
	methodsGetCmd.Flags().StringVar(&methodGetLocale, "locale", "", "Locale for sorting and translation (e.g. en_US, de_DE)")
	methodsGetCmd.Flags().StringVar(&methodGetCurrency, "currency", "", "Convert min/max amounts to this currency (e.g. USD)")
	methodsGetCmd.Flags().StringVar(&methodGetInclude, "include", "", "Additional data to include")
	methodsGetCmd.Flags().StringVar(&methodGetSequenceType, "sequence-type", "", "Filter by sequence type (oneoff, first, recurring)")

	methodsCmd.AddCommand(methodsListCmd)
	methodsCmd.AddCommand(methodsListAllCmd)
	methodsCmd.AddCommand(methodsGetCmd)
	rootCmd.AddCommand(methodsCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runMethodsList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListMethodsRequest{}
	if methodListSequenceType != "" {
		st := components.SequenceType(methodListSequenceType)
		req.SequenceType = &st
	}
	if methodListLocale != "" {
		l := components.Locale(methodListLocale)
		req.Locale = &l
	}
	if methodListAmountValue != "" || methodListAmountCurrency != "" {
		req.Amount = &components.Amount{
			Value:    methodListAmountValue,
			Currency: methodListAmountCurrency,
		}
	}
	if methodListBillingCountry != "" {
		req.BillingCountry = &methodListBillingCountry
	}
	if methodListIncludeWallets != "" {
		iw := components.MethodIncludeWalletsParameter(methodListIncludeWallets)
		req.IncludeWallets = &iw
	}
	if methodListOrderLineCategories != "" {
		olc := components.LineCategories(methodListOrderLineCategories)
		req.OrderLineCategories = &olc
	}
	if methodListInclude != "" {
		req.Include = &methodListInclude
	}

	resp, err := client.Methods.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing methods: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	listEmb := resp.Object.GetEmbedded()
	methods := listEmb.GetMethods()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(methods))
		for _, m := range methods {
			id := "—"
			if mi := m.GetID(); mi != nil {
				id = string(*mi)
			}
			listMn := m.GetMinimumAmount()
			minAmt := fmtMethodAmt(listMn.GetValue(), listMn.GetCurrency())
			maxAmt := "\u2014"
			if ma := m.GetMaximumAmount(); ma != nil {
				maxAmt = fmtMethodAmt(ma.GetValue(), ma.GetCurrency())
			}
			rows = append(rows, []string{id, m.GetDescription(), minAmt, maxAmt})
		}
		output.PrintTable(
			[]string{"ID", "DESCRIPTION", "MIN AMOUNT", "MAX AMOUNT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runMethodsListAll(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListAllMethodsRequest{}
	if methodListAllLocale != "" {
		l := components.Locale(methodListAllLocale)
		req.Locale = &l
	}
	if methodListAllAmountValue != "" || methodListAllAmountCurrency != "" {
		req.Amount = &components.Amount{
			Value:    methodListAllAmountValue,
			Currency: methodListAllAmountCurrency,
		}
	}
	if methodListAllInclude != "" {
		req.Include = &methodListAllInclude
	}
	if methodListAllSequenceType != "" {
		st := components.SequenceType(methodListAllSequenceType)
		req.SequenceType = &st
	}

	resp, err := client.Methods.All(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing all methods: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	emb := resp.Object.GetEmbedded()
	methods := emb.GetMethods()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(methods))
		for _, m := range methods {
			id := "\u2014"
			if mi := m.GetID(); mi != nil {
				id = string(*mi)
			}
			status := "\u2014"
			if s := m.GetStatus(); s != nil {
				status = string(*s)
			}
			min := m.GetMinimumAmount()
			minAmt := fmtMethodAmt(min.GetValue(), min.GetCurrency())
			maxAmt := "\u2014"
			if ma := m.GetMaximumAmount(); ma != nil {
				maxAmt = fmtMethodAmt(ma.GetValue(), ma.GetCurrency())
			}
			rows = append(rows, []string{id, m.GetDescription(), status, minAmt, maxAmt})
		}
		output.PrintTable(
			[]string{"ID", "DESCRIPTION", "STATUS", "MIN AMOUNT", "MAX AMOUNT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runMethodsGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	methodID := components.MethodEnum(args[0])
	req := operations.GetMethodRequest{
		MethodID: &methodID,
	}
	if methodGetLocale != "" {
		l := components.Locale(methodGetLocale)
		req.Locale = &l
	}
	if methodGetCurrency != "" {
		req.Currency = &methodGetCurrency
	}
	if methodGetInclude != "" {
		req.Include = &methodGetInclude
	}
	if methodGetSequenceType != "" {
		st := components.SequenceType(methodGetSequenceType)
		req.SequenceType = &st
	}
	resp, err := client.Methods.Get(context.Background(), req)
	if err != nil {
		return fmt.Errorf("getting method: %w", err)
	}
	m := resp.GetEntityMethodGet()
	if m == nil {
		return fmt.Errorf("method not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(m)
	default:
		id := "\u2014"
		if mi := m.GetID(); mi != nil {
			id = string(*mi)
		}
		minAmt := methodGetMinAmt(m)
		maxAmt := "\u2014"
		if ma := m.GetMaximumAmount(); ma != nil {
			maxAmt = fmtMethodAmt(ma.GetValue(), ma.GetCurrency())
		}
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", id},
				{"Description", m.GetDescription()},
				{"Min Amount", minAmt},
				{"Max Amount", maxAmt},
			},
			!flagLive,
		)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// fmtMethodAmt formats a (value, currency) pair as "VALUE CURRENCY",
// returning "\u2014" when both are empty.
func fmtMethodAmt(value, currency string) string {
	if value == "" && currency == "" {
		return "\u2014"
	}
	return fmt.Sprintf("%s %s", value, currency)
}

// methodGetMinAmt extracts the minimum amount string from an EntityMethodGet.
func methodGetMinAmt(m *components.EntityMethodGet) string {
	min := m.GetMinimumAmount()
	return fmtMethodAmt(min.GetValue(), min.GetCurrency())
}
