package cmd

import (
	"context"
	"encoding/json"
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

// ── flag value holders ────────────────────────────────────────────────────────────────────────────

var (
	// create flags
	custName     string
	custEmail    string
	custMetadata string

	// list flags
	custListLimit int64
	custListFrom  string

	// update flags
	custUpdateName     string
	custUpdateEmail    string
	custUpdateMetadata string

	// delete flag
	custDeleteConfirm bool

	// customer payments create flags
	custPayCreateAmount       string
	custPayCreateCurrency     string
	custPayCreateDescription  string
	custPayCreateRedirectURL  string
	custPayCreateMethod       string
	custPayCreateSequenceType string
	custPayCreateWebhookURL   string
	custPayCreateMetadata     string

	// customer payments list flags
	custPayListLimit int64
	custPayListFrom  string
)

// ── command tree ────────────────────────────────────────────────────────────────────────────────

var customersCmd = &cobra.Command{
	Use:   "customers",
	Short: "Manage Mollie customers",
}

var customersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new customer",
	RunE:  runCustomersCreate,
}

var customersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List customers",
	RunE:  runCustomersList,
}

var customersGetCmd = &cobra.Command{
	Use:   "get <customer-id>",
	Short: "Get a customer",
	Args:  cobra.ExactArgs(1),
	RunE:  runCustomersGet,
}

var customersUpdateCmd = &cobra.Command{
	Use:   "update <customer-id>",
	Short: "Update a customer's name, email, or metadata",
	Args:  cobra.ExactArgs(1),
	RunE:  runCustomersUpdate,
}

var customersDeleteCmd = &cobra.Command{
	Use:   "delete <customer-id>",
	Short: "Delete a customer (also cancels all mandates and subscriptions)",
	Args:  cobra.ExactArgs(1),
	RunE:  runCustomersDelete,
}

var customersPaymentsCmd = &cobra.Command{
	Use:   "payments",
	Short: "Manage payments for a customer",
}

var customersPaymentsCreateCmd = &cobra.Command{
	Use:   "create <customer-id>",
	Short: "Create a payment for a customer",
	Args:  cobra.ExactArgs(1),
	RunE:  runCustomersPaymentsCreate,
}

var customersPaymentsListCmd = &cobra.Command{
	Use:   "list <customer-id>",
	Short: "List payments for a customer",
	Args:  cobra.ExactArgs(1),
	RunE:  runCustomersPaymentsList,
}

func init() {
	// create
	customersCreateCmd.Flags().StringVar(&custName, "name", "", "Full name of the customer")
	customersCreateCmd.Flags().StringVar(&custEmail, "email", "", "Email address of the customer")
	customersCreateCmd.Flags().StringVar(&custMetadata, "metadata", "", "Arbitrary JSON metadata")

	// list
	customersListCmd.Flags().Int64Var(&custListLimit, "limit", 50, "Maximum number of results to return")
	customersListCmd.Flags().StringVar(&custListFrom, "from", "", "Return results starting from this customer ID (cursor pagination)")

	// update
	customersUpdateCmd.Flags().StringVar(&custUpdateName, "name", "", "New full name")
	customersUpdateCmd.Flags().StringVar(&custUpdateEmail, "email", "", "New email address")
	customersUpdateCmd.Flags().StringVar(&custUpdateMetadata, "metadata", "", "New metadata (JSON string)")

	// delete
	customersDeleteCmd.Flags().BoolVar(&custDeleteConfirm, "confirm", false, "Skip the confirmation prompt")

	// customer payments create
	customersPaymentsCreateCmd.Flags().StringVar(&custPayCreateAmount, "amount", "", "Payment amount, e.g. 10.00 (required)")
	customersPaymentsCreateCmd.Flags().StringVar(&custPayCreateCurrency, "currency", "", "ISO 4217 currency code, e.g. EUR (required)")
	customersPaymentsCreateCmd.Flags().StringVar(&custPayCreateDescription, "description", "", "Payment description (required)")
	customersPaymentsCreateCmd.Flags().StringVar(&custPayCreateRedirectURL, "redirect-url", "", "URL to redirect the customer to after payment (required)")
	customersPaymentsCreateCmd.Flags().StringVar(&custPayCreateMethod, "method", "", "Payment method, e.g. ideal, creditcard")
	customersPaymentsCreateCmd.Flags().StringVar(&custPayCreateSequenceType, "sequence-type", "", "Sequence type: oneoff, first, or recurring")
	customersPaymentsCreateCmd.Flags().StringVar(&custPayCreateWebhookURL, "webhook-url", "", "Webhook URL for payment status updates")
	customersPaymentsCreateCmd.Flags().StringVar(&custPayCreateMetadata, "metadata", "", "Arbitrary JSON metadata to attach to the payment")
	// Required fields are validated in runCustomersPaymentsCreate so that JSON
	// stdin can supply them without triggering Cobra's pre-RunE flag validation.

	// customer payments list
	customersPaymentsListCmd.Flags().Int64Var(&custPayListLimit, "limit", 50, "Maximum number of results to return")
	customersPaymentsListCmd.Flags().StringVar(&custPayListFrom, "from", "", "Return results starting from this payment ID (cursor pagination)")

	customersPaymentsCmd.AddCommand(customersPaymentsCreateCmd)
	customersPaymentsCmd.AddCommand(customersPaymentsListCmd)

	customersCmd.AddCommand(customersCreateCmd)
	customersCmd.AddCommand(customersListCmd)
	customersCmd.AddCommand(customersGetCmd)
	customersCmd.AddCommand(customersUpdateCmd)
	customersCmd.AddCommand(customersDeleteCmd)
	customersCmd.AddCommand(customersPaymentsCmd)

	rootCmd.AddCommand(customersCmd)
}

// ── handlers ───────────────────────────────────────────────────────────────────────────────────

func runCustomersCreate(cmd *cobra.Command, _ []string) error {
	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "name"); ok && !cmd.Flags().Changed("name") {
			custName = v
		}
		if v, ok := input.Str(jsonInput, "email"); ok && !cmd.Flags().Changed("email") {
			custEmail = v
		}
		if v, ok := input.RawJSON(jsonInput, "metadata"); ok && !cmd.Flags().Changed("metadata") {
			custMetadata = v
		}
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := &components.EntityCustomer{}
	if custName != "" {
		req.Name = &custName
	}
	if custEmail != "" {
		req.Email = &custEmail
	}
	if custMetadata != "" {
		meta, err := parseMetadata(custMetadata)
		if err != nil {
			return fmt.Errorf("invalid --metadata: %w", err)
		}
		req.Metadata = &meta
	}

	resp, err := client.Customers.Create(context.Background(), nil, req)
	if err != nil {
		return fmt.Errorf("creating customer: %w", err)
	}
	c := resp.GetCustomerResponse()
	if c == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(c)
	default:
		output.PrintTable(
			[]string{"ID", "NAME", "EMAIL", "CREATED AT"},
			[][]string{{
				c.GetID(),
				derefOpt(c.GetName()),
				derefOpt(c.GetEmail()),
				c.GetCreatedAt(),
			}},
			!flagLive,
		)
	}
	return nil
}

func runCustomersList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListCustomersRequest{
		Limit: &custListLimit,
	}
	if custListFrom != "" {
		req.From = &custListFrom
	}

	resp, err := client.Customers.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing customers: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	embedded := resp.Object.GetEmbedded()
	customers := embedded.GetCustomers()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(customers))
		for _, c := range customers {
			rows = append(rows, []string{
				c.GetID(),
				derefOpt(c.GetName()),
				derefOpt(c.GetEmail()),
				c.GetCreatedAt(),
			})
		}
		output.PrintTable(
			[]string{"ID", "NAME", "EMAIL", "CREATED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runCustomersGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Customers.Get(context.Background(), args[0], nil, nil, nil)
	if err != nil {
		return fmt.Errorf("getting customer: %w", err)
	}
	c := resp.GetObject()
	if c == nil {
		return fmt.Errorf("customer not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(c)
	default:
		metaStr := "—"
		if m := c.GetMetadata(); m != nil {
			if b, err := json.Marshal(m); err == nil {
				metaStr = string(b)
			}
		}
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", c.GetID()},
				{"Mode", string(c.GetMode())},
				{"Name", derefOpt(c.GetName())},
				{"Email", derefOpt(c.GetEmail())},
				{"Created At", c.GetCreatedAt()},
				{"Metadata", metaStr},
			},
			!flagLive,
		)
	}
	return nil
}

func runCustomersUpdate(cmd *cobra.Command, args []string) error {
	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "name"); ok && !cmd.Flags().Changed("name") {
			custUpdateName = v
		}
		if v, ok := input.Str(jsonInput, "email"); ok && !cmd.Flags().Changed("email") {
			custUpdateEmail = v
		}
		if v, ok := input.RawJSON(jsonInput, "metadata"); ok && !cmd.Flags().Changed("metadata") {
			custUpdateMetadata = v
		}
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	body := &operations.UpdateCustomerRequestBody{}
	if custUpdateName != "" {
		body.Name = &custUpdateName
	}
	if custUpdateEmail != "" {
		body.Email = &custUpdateEmail
	}
	if custUpdateMetadata != "" {
		meta, err := parseMetadata(custUpdateMetadata)
		if err != nil {
			return fmt.Errorf("invalid --metadata: %w", err)
		}
		body.Metadata = &meta
	}

	resp, err := client.Customers.Update(context.Background(), args[0], nil, body)
	if err != nil {
		return fmt.Errorf("updating customer: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.GetCustomerResponse())
	default:
		fmt.Printf("✓ Customer %s updated\n", args[0])
	}
	return nil
}

func runCustomersDelete(_ *cobra.Command, args []string) error {
	customerID := args[0]

	if !custDeleteConfirm {
		confirmed, err := prompt.Confirm(fmt.Sprintf("Delete customer %s? This will also cancel all their mandates and subscriptions.", customerID))
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

	if _, err := client.Customers.Delete(context.Background(), customerID, nil, nil); err != nil {
		return fmt.Errorf("deleting customer: %w", err)
	}

	fmt.Printf("✓ Customer %s deleted\n", customerID)
	return nil
}

func runCustomersPaymentsCreate(cmd *cobra.Command, args []string) error {
	customerID := args[0]

	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "description"); ok && !cmd.Flags().Changed("description") {
			custPayCreateDescription = v
		}
		if val, cur, ok := input.Amount(jsonInput, "amount"); ok {
			if !cmd.Flags().Changed("amount") {
				custPayCreateAmount = val
			}
			if !cmd.Flags().Changed("currency") {
				custPayCreateCurrency = cur
			}
		}
		if v, ok := input.Str(jsonInput, "redirectUrl"); ok && !cmd.Flags().Changed("redirect-url") {
			custPayCreateRedirectURL = v
		}
		if v, ok := input.Str(jsonInput, "webhookUrl"); ok && !cmd.Flags().Changed("webhook-url") {
			custPayCreateWebhookURL = v
		}
		if v, ok := input.Str(jsonInput, "method"); ok && !cmd.Flags().Changed("method") {
			custPayCreateMethod = v
		}
		if v, ok := input.Str(jsonInput, "sequenceType"); ok && !cmd.Flags().Changed("sequence-type") {
			custPayCreateSequenceType = v
		}
		if v, ok := input.RawJSON(jsonInput, "metadata"); ok && !cmd.Flags().Changed("metadata") {
			custPayCreateMetadata = v
		}
	}

	// Validate required fields (may have been supplied via JSON stdin).
	switch {
	case custPayCreateAmount == "":
		return fmt.Errorf("required flag \"amount\" not set")
	case custPayCreateCurrency == "":
		return fmt.Errorf("required flag \"currency\" not set")
	case custPayCreateDescription == "":
		return fmt.Errorf("required flag \"description\" not set")
	case custPayCreateRedirectURL == "":
		return fmt.Errorf("required flag \"redirect-url\" not set")
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := &components.PaymentRequest{
		Description: custPayCreateDescription,
		Amount: components.Amount{
			Currency: custPayCreateCurrency,
			Value:    custPayCreateAmount,
		},
		RedirectURL: &custPayCreateRedirectURL,
		CustomerID:  &customerID,
	}

	if custPayCreateWebhookURL != "" {
		req.WebhookURL = &custPayCreateWebhookURL
	}
	if custPayCreateSequenceType != "" {
		st := components.SequenceType(custPayCreateSequenceType)
		req.SequenceType = &st
	}
	if custPayCreateMethod != "" {
		me := components.MethodEnum(custPayCreateMethod)
		m := components.CreateMethodMethodEnum(me)
		req.Method = &m
	}
	if custPayCreateMetadata != "" {
		meta, err := parseMetadata(custPayCreateMetadata)
		if err != nil {
			return fmt.Errorf("invalid --metadata: %w", err)
		}
		req.Metadata = &meta
	}

	resp, err := client.Customers.CreatePayment(context.Background(), customerID, nil, req)
	if err != nil {
		return fmt.Errorf("creating payment for customer: %w", err)
	}
	pay := resp.GetPaymentResponse()
	if pay == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(pay)
	default:
		checkoutURL := ""
		if co := pay.Links.GetCheckout(); co != nil {
			checkoutURL = co.GetHref()
		}
		output.PrintTable(
			[]string{"ID", "STATUS", "AMOUNT", "DESCRIPTION", "CHECKOUT URL"},
			[][]string{{
				pay.GetID(),
				string(pay.GetStatus()),
				formatAmount(pay.Amount),
				pay.GetDescription(),
				checkoutURL,
			}},
			!flagLive,
		)
	}
	return nil
}

func runCustomersPaymentsList(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListCustomerPaymentsRequest{
		CustomerID: args[0],
		Limit:      &custPayListLimit,
	}
	if custPayListFrom != "" {
		req.From = &custPayListFrom
	}

	resp, err := client.Customers.ListPayments(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing payments for customer: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	custPayEmbedded := resp.Object.GetEmbedded()
	payments := custPayEmbedded.GetPayments()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(payments))
		for _, p := range payments {
			rows = append(rows, []string{
				p.GetID(),
				string(p.GetStatus()),
				formatAmount(p.GetAmount()),
				p.GetCreatedAt(),
				p.GetDescription(),
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
