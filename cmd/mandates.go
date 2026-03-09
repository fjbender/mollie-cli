package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"
	"github.com/spf13/cobra"
)

// ── flag value holders ────────────────────────────────────────────────────────────────────────────

var (
	// create flags
	manMethod          string
	manConsumerName    string
	manConsumerAccount string
	manConsumerBic     string
	manConsumerEmail   string
	manSignatureDate   string
	manReference       string

	// list flags
	manListLimit int64
	manListFrom  string

	// revoke flag
	manRevokeConfirm bool
)

// ── command tree ────────────────────────────────────────────────────────────────────────────────

var mandatesCmd = &cobra.Command{
	Use:   "mandates",
	Short: "Manage Mollie mandates",
}

var mandatesCreateCmd = &cobra.Command{
	Use:   "create <customer-id>",
	Short: "Create a mandate for a customer",
	Args:  cobra.ExactArgs(1),
	RunE:  runMandatesCreate,
}

var mandatesListCmd = &cobra.Command{
	Use:   "list <customer-id>",
	Short: "List mandates for a customer",
	Args:  cobra.ExactArgs(1),
	RunE:  runMandatesList,
}

var mandatesGetCmd = &cobra.Command{
	Use:   "get <customer-id> <mandate-id>",
	Short: "Get a mandate",
	Args:  cobra.ExactArgs(2),
	RunE:  runMandatesGet,
}

var mandatesRevokeCmd = &cobra.Command{
	Use:   "revoke <customer-id> <mandate-id>",
	Short: "Revoke a mandate",
	Args:  cobra.ExactArgs(2),
	RunE:  runMandatesRevoke,
}

func init() {
	// create flags
	mandatesCreateCmd.Flags().StringVar(&manMethod, "method", "directdebit", "Payment method: creditcard, directdebit, or paypal")
	mandatesCreateCmd.Flags().StringVar(&manConsumerName, "consumer-name", "", "Consumer full name (required)")
	mandatesCreateCmd.Flags().StringVar(&manConsumerAccount, "consumer-account", "", "Consumer IBAN (required for directdebit)")
	mandatesCreateCmd.Flags().StringVar(&manConsumerBic, "consumer-bic", "", "Bank BIC (optional, directdebit)")
	mandatesCreateCmd.Flags().StringVar(&manConsumerEmail, "consumer-email", "", "Consumer email (required for paypal)")
	mandatesCreateCmd.Flags().StringVar(&manSignatureDate, "signature-date", "", "Date mandate was signed, YYYY-MM-DD")
	mandatesCreateCmd.Flags().StringVar(&manReference, "mandate-reference", "", "Custom mandate reference")
	mandatesCreateCmd.MarkFlagRequired("consumer-name") //nolint:errcheck

	// list flags
	mandatesListCmd.Flags().Int64Var(&manListLimit, "limit", 50, "Maximum number of results to return")
	mandatesListCmd.Flags().StringVar(&manListFrom, "from", "", "Return results starting from this mandate ID (cursor pagination)")

	// revoke flag
	mandatesRevokeCmd.Flags().BoolVar(&manRevokeConfirm, "confirm", false, "Skip the confirmation prompt")

	mandatesCmd.AddCommand(mandatesCreateCmd)
	mandatesCmd.AddCommand(mandatesListCmd)
	mandatesCmd.AddCommand(mandatesGetCmd)
	mandatesCmd.AddCommand(mandatesRevokeCmd)

	rootCmd.AddCommand(mandatesCmd)
}

// ── handlers ───────────────────────────────────────────────────────────────────────────────────

func runMandatesCreate(_ *cobra.Command, args []string) error {
	customerID := args[0]

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := &components.MandateRequest{
		Method:       components.MandateMethod(manMethod),
		ConsumerName: manConsumerName,
	}
	if manConsumerAccount != "" {
		req.ConsumerAccount = &manConsumerAccount
	}
	if manConsumerBic != "" {
		req.ConsumerBic = &manConsumerBic
	}
	if manConsumerEmail != "" {
		req.ConsumerEmail = &manConsumerEmail
	}
	if manSignatureDate != "" {
		req.SignatureDate = &manSignatureDate
	}
	if manReference != "" {
		req.MandateReference = &manReference
	}

	resp, err := client.Mandates.Create(context.Background(), customerID, nil, req)
	if err != nil {
		return fmt.Errorf("creating mandate: %w", err)
	}
	m := resp.GetMandateResponse()
	if m == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(m)
	default:
		output.PrintTable(
			[]string{"ID", "STATUS", "METHOD", "CONSUMER NAME", "CREATED AT"},
			[][]string{{
				m.GetID(),
				string(m.GetStatus()),
				string(m.GetMethod()),
				derefOpt(m.GetDetails().ConsumerName),
				m.GetCreatedAt(),
			}},
			!flagLive,
		)
	}
	return nil
}

func runMandatesList(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListMandatesRequest{
		CustomerID: args[0],
		Limit:      &manListLimit,
	}
	if manListFrom != "" {
		req.From = &manListFrom
	}

	resp, err := client.Mandates.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing mandates: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	manEmbedded := resp.Object.GetEmbedded()
	mandates := manEmbedded.GetMandates()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(mandates))
		for _, m := range mandates {
			rows = append(rows, []string{
				m.GetID(),
				string(m.GetStatus()),
				string(m.GetMethod()),
				derefOpt(m.GetDetails().ConsumerName),
				m.GetCreatedAt(),
			})
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "METHOD", "CONSUMER NAME", "CREATED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runMandatesGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Mandates.Get(context.Background(), args[0], args[1], nil, nil)
	if err != nil {
		return fmt.Errorf("getting mandate: %w", err)
	}
	m := resp.GetMandateResponse()
	if m == nil {
		return fmt.Errorf("mandate not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(m)
	default:
		details := m.GetDetails()
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", m.GetID()},
				{"Mode", string(m.GetMode())},
				{"Status", string(m.GetStatus())},
				{"Method", string(m.GetMethod())},
				{"Consumer Name", derefOpt(details.ConsumerName)},
				{"Consumer Account", derefOpt(details.ConsumerAccount)},
				{"Consumer BIC", derefOpt(details.ConsumerBic)},
				{"Mandate Reference", derefOpt(m.GetMandateReference())},
				{"Signature Date", derefOpt(m.GetSignatureDate())},
				{"Customer ID", m.GetCustomerID()},
				{"Created At", m.GetCreatedAt()},
			},
			!flagLive,
		)
	}
	return nil
}

func runMandatesRevoke(_ *cobra.Command, args []string) error {
	customerID, mandateID := args[0], args[1]

	if !manRevokeConfirm {
		confirmed, err := prompt.Confirm(fmt.Sprintf("Revoke mandate %s for customer %s?", mandateID, customerID))
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

	if _, err := client.Mandates.Revoke(context.Background(), customerID, mandateID, nil, nil); err != nil {
		return fmt.Errorf("revoking mandate: %w", err)
	}

	fmt.Printf("✓ Mandate %s revoked\n", mandateID)
	return nil
}
