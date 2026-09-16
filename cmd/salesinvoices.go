package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/input"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"
	"github.com/spf13/cobra"
)

// ── flag value holders ───────────────────────────────────────────────────────

var (
	// create flags
	siCreateStatus              string
	siCreateProfileID           string
	siCreateVatScheme           string
	siCreateVatMode             string
	siCreateMemo                string
	siCreateMetadata            string
	siCreatePaymentTerm         string
	siCreateCustomerID          string
	siCreateMandateID           string
	siCreateRecipientIdentifier string
	siCreateIsEInvoice          bool

	siCreateRecipientType             string
	siCreateRecipientTitle            string
	siCreateRecipientGivenName        string
	siCreateRecipientFamilyName       string
	siCreateRecipientOrgName          string
	siCreateRecipientOrgNumber        string
	siCreateRecipientVatNumber        string
	siCreateRecipientEmail            string
	siCreateRecipientPhone            string
	siCreateRecipientStreet           string
	siCreateRecipientStreetAdditional string
	siCreateRecipientPostalCode       string
	siCreateRecipientCity             string
	siCreateRecipientRegion           string
	siCreateRecipientCountry          string
	siCreateRecipientLocale           string

	siCreateLineDescription string
	siCreateLineQuantity    int64
	siCreateLineVatRate     string
	siCreateLineUnitPrice   string
	siCreateCurrency        string

	siCreateDiscountType  string
	siCreateDiscountValue string

	siCreatePaymentSource          string
	siCreatePaymentSourceReference string

	siCreateEmailSubject string
	siCreateEmailBody    string

	// list flags
	siListLimit int64
	siListFrom  string

	// update flags
	siUpdateStatus              string
	siUpdateMemo                string
	siUpdatePaymentTerm         string
	siUpdateRecipientIdentifier string
	siUpdateIsEInvoice          bool

	siUpdateRecipientType             string
	siUpdateRecipientTitle            string
	siUpdateRecipientGivenName        string
	siUpdateRecipientFamilyName       string
	siUpdateRecipientOrgName          string
	siUpdateRecipientOrgNumber        string
	siUpdateRecipientVatNumber        string
	siUpdateRecipientEmail            string
	siUpdateRecipientPhone            string
	siUpdateRecipientStreet           string
	siUpdateRecipientStreetAdditional string
	siUpdateRecipientPostalCode       string
	siUpdateRecipientCity             string
	siUpdateRecipientRegion           string
	siUpdateRecipientCountry          string
	siUpdateRecipientLocale           string

	siUpdateLineDescription string
	siUpdateLineQuantity    int64
	siUpdateLineVatRate     string
	siUpdateLineUnitPrice   string
	siUpdateCurrency        string

	siUpdateDiscountType  string
	siUpdateDiscountValue string

	siUpdatePaymentSource          string
	siUpdatePaymentSourceReference string

	siUpdateEmailSubject string
	siUpdateEmailBody    string

	// delete flag
	siDeleteConfirm bool
)

// ── command tree ─────────────────────────────────────────────────────────────

var salesInvoicesCmd = &cobra.Command{
	Use:   "sales-invoices",
	Short: "Manage Mollie sales invoices",
}

var salesInvoicesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new sales invoice",
	Long: `Create a new sales invoice.

A recipient and at least one line item are required. Build them with the
--recipient-* and --line-* flags below, or pipe a JSON body containing
"recipient" and/or "lines" via stdin — stdin is only used for a section when
its primary flag (--recipient-type, --line-description) was not set.`,
	RunE: runSalesInvoicesCreate,
}

var salesInvoicesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sales invoices",
	RunE:  runSalesInvoicesList,
}

var salesInvoicesGetCmd = &cobra.Command{
	Use:   "get <sales-invoice-id>",
	Short: "Get a sales invoice",
	Args:  cobra.ExactArgs(1),
	RunE:  runSalesInvoicesGet,
}

var salesInvoicesUpdateCmd = &cobra.Command{
	Use:   "update <sales-invoice-id>",
	Short: "Update a sales invoice",
	Long: `Update an existing sales invoice.

Only draft invoices can be freely updated; issued/paid invoices have extra
requirements (see the Mollie API docs). All flags are optional — only the
fields you set are sent.`,
	Args: cobra.ExactArgs(1),
	RunE: runSalesInvoicesUpdate,
}

var salesInvoicesDeleteCmd = &cobra.Command{
	Use:   "delete <sales-invoice-id>",
	Short: "Delete a draft sales invoice",
	Args:  cobra.ExactArgs(1),
	RunE:  runSalesInvoicesDelete,
}

func init() {
	// create: top-level flags
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateStatus, "status", "draft", "Invoice status to create: draft, issued, or paid")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateProfileID, "profile-id", "", "Profile ID (required for organization access tokens)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateVatScheme, "vat-scheme", "", "VAT scheme: standard or one-stop-shop")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateVatMode, "vat-mode", "", "VAT mode: exclusive or inclusive")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateMemo, "memo", "", "Free-form memo shown on the invoice PDF")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateMetadata, "metadata", "", "Arbitrary JSON metadata to attach to the invoice")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreatePaymentTerm, "payment-term", "", "Payment term: 7 days, 14 days, 30 days, 45 days, 60 days, 90 days, or 120 days")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateCustomerID, "customer-id", "", "Customer ID to attempt an automated payment for (requires --mandate-id; status must be paid)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateMandateID, "mandate-id", "", "Mandate ID to use for the automated payment (requires --customer-id; status must be paid)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientIdentifier, "recipient-identifier", "", "Your own unique identifier for the recipient (required)")
	salesInvoicesCreateCmd.Flags().BoolVar(&siCreateIsEInvoice, "is-e-invoice", false, "Deliver the invoice as an e-invoice via Peppol (NL/BE/DE merchants and recipients only)")

	// create: recipient flags
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientType, "recipient-type", "", "Recipient type: consumer or business")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientTitle, "recipient-title", "", "Recipient title, e.g. Mr. or Mrs. (consumer)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientGivenName, "recipient-given-name", "", "Recipient given name (consumer)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientFamilyName, "recipient-family-name", "", "Recipient family name (consumer)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientOrgName, "recipient-org-name", "", "Recipient organisation trading name (business)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientOrgNumber, "recipient-org-number", "", "Recipient Chamber of Commerce number (business; or use --recipient-vat-number)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientVatNumber, "recipient-vat-number", "", "Recipient VAT number (business; or use --recipient-org-number)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientEmail, "recipient-email", "", "Recipient email address")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientPhone, "recipient-phone", "", "Recipient phone number")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientStreet, "recipient-street", "", "Recipient street and number")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientStreetAdditional, "recipient-street-additional", "", "Recipient additional addressing details")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientPostalCode, "recipient-postal-code", "", "Recipient postal code")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientCity, "recipient-city", "", "Recipient city")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientRegion, "recipient-region", "", "Recipient region / state")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientCountry, "recipient-country", "", "Recipient ISO 3166-1 alpha-2 country code")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateRecipientLocale, "recipient-locale", "", "Recipient locale, e.g. en_US, nl_NL, de_DE")

	// create: single line item flags (multi-line: pipe JSON via stdin)
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateLineDescription, "line-description", "", "Description of the (single) line item; for multiple lines pipe JSON via stdin")
	salesInvoicesCreateCmd.Flags().Int64Var(&siCreateLineQuantity, "line-quantity", 1, "Quantity for the line item")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateLineVatRate, "line-vat-rate", "", "VAT rate for the line item, e.g. 21.00")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateLineUnitPrice, "line-unit-price", "", "Unit price for the line item, e.g. 10.00")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateCurrency, "currency", "EUR", "ISO 4217 currency code for the line item's unit price")

	// create: discount flags
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateDiscountType, "discount-type", "", "Discount type: amount or percentage")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateDiscountValue, "discount-value", "", "Discount value, e.g. 10.00")

	// create: payment details flags
	salesInvoicesCreateCmd.Flags().StringVar(&siCreatePaymentSource, "payment-source", "", "How the invoice is paid: manual, payment-link, or payment (required if --status paid)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreatePaymentSourceReference, "payment-source-reference", "", "Reference to the payment/payment link (required unless --payment-source manual)")

	// create: email details flags
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateEmailSubject, "email-subject", "", "Subject of the email sent to the recipient (sends the invoice by email)")
	salesInvoicesCreateCmd.Flags().StringVar(&siCreateEmailBody, "email-body", "", "Body of the email sent to the recipient")

	// list
	salesInvoicesListCmd.Flags().Int64Var(&siListLimit, "limit", 50, "Maximum number of results to return")
	salesInvoicesListCmd.Flags().StringVar(&siListFrom, "from", "", "Return results starting from this sales invoice ID (cursor pagination)")

	// update: top-level flags
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateStatus, "status", "", "New invoice status: draft, issued, paid, or cancelled")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateMemo, "memo", "", "New free-form memo")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdatePaymentTerm, "payment-term", "", "New payment term: 7 days, 14 days, 30 days, 45 days, 60 days, 90 days, or 120 days")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientIdentifier, "recipient-identifier", "", "New recipient identifier")
	salesInvoicesUpdateCmd.Flags().BoolVar(&siUpdateIsEInvoice, "is-e-invoice", false, "Deliver the invoice as an e-invoice via Peppol")

	// update: recipient flags
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientType, "recipient-type", "", "New recipient type: consumer or business")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientTitle, "recipient-title", "", "New recipient title")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientGivenName, "recipient-given-name", "", "New recipient given name")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientFamilyName, "recipient-family-name", "", "New recipient family name")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientOrgName, "recipient-org-name", "", "New recipient organisation trading name")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientOrgNumber, "recipient-org-number", "", "New recipient Chamber of Commerce number")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientVatNumber, "recipient-vat-number", "", "New recipient VAT number")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientEmail, "recipient-email", "", "New recipient email address")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientPhone, "recipient-phone", "", "New recipient phone number")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientStreet, "recipient-street", "", "New recipient street and number")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientStreetAdditional, "recipient-street-additional", "", "New recipient additional addressing details")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientPostalCode, "recipient-postal-code", "", "New recipient postal code")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientCity, "recipient-city", "", "New recipient city")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientRegion, "recipient-region", "", "New recipient region / state")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientCountry, "recipient-country", "", "New recipient ISO 3166-1 alpha-2 country code")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateRecipientLocale, "recipient-locale", "", "New recipient locale, e.g. en_US, nl_NL, de_DE")

	// update: single line item flags (multi-line: pipe JSON via stdin)
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateLineDescription, "line-description", "", "Description of the (single) replacement line item; for multiple lines pipe JSON via stdin")
	salesInvoicesUpdateCmd.Flags().Int64Var(&siUpdateLineQuantity, "line-quantity", 1, "Quantity for the line item")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateLineVatRate, "line-vat-rate", "", "VAT rate for the line item, e.g. 21.00")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateLineUnitPrice, "line-unit-price", "", "Unit price for the line item, e.g. 10.00")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateCurrency, "currency", "EUR", "ISO 4217 currency code for the line item's unit price")

	// update: discount flags
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateDiscountType, "discount-type", "", "New discount type: amount or percentage")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateDiscountValue, "discount-value", "", "New discount value, e.g. 10.00")

	// update: payment details flags
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdatePaymentSource, "payment-source", "", "How the invoice is paid: manual, payment-link, or payment (required if --status paid)")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdatePaymentSourceReference, "payment-source-reference", "", "Reference to the payment/payment link (required unless --payment-source manual)")

	// update: email details flags
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateEmailSubject, "email-subject", "", "Subject of the email sent to the recipient")
	salesInvoicesUpdateCmd.Flags().StringVar(&siUpdateEmailBody, "email-body", "", "Body of the email sent to the recipient")

	// delete
	salesInvoicesDeleteCmd.Flags().BoolVar(&siDeleteConfirm, "confirm", false, "Skip the confirmation prompt")

	salesInvoicesCmd.AddCommand(salesInvoicesCreateCmd)
	salesInvoicesCmd.AddCommand(salesInvoicesListCmd)
	salesInvoicesCmd.AddCommand(salesInvoicesGetCmd)
	salesInvoicesCmd.AddCommand(salesInvoicesUpdateCmd)
	salesInvoicesCmd.AddCommand(salesInvoicesDeleteCmd)
	rootCmd.AddCommand(salesInvoicesCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runSalesInvoicesCreate(cmd *cobra.Command, _ []string) error {
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "status"); ok && !cmd.Flags().Changed("status") {
			siCreateStatus = v
		}
		if v, ok := input.Str(jsonInput, "profileId"); ok && !cmd.Flags().Changed("profile-id") {
			siCreateProfileID = v
		}
		if v, ok := input.Str(jsonInput, "vatScheme"); ok && !cmd.Flags().Changed("vat-scheme") {
			siCreateVatScheme = v
		}
		if v, ok := input.Str(jsonInput, "vatMode"); ok && !cmd.Flags().Changed("vat-mode") {
			siCreateVatMode = v
		}
		if v, ok := input.Str(jsonInput, "memo"); ok && !cmd.Flags().Changed("memo") {
			siCreateMemo = v
		}
		if v, ok := input.RawJSON(jsonInput, "metadata"); ok && !cmd.Flags().Changed("metadata") {
			siCreateMetadata = v
		}
		if v, ok := input.Str(jsonInput, "paymentTerm"); ok && !cmd.Flags().Changed("payment-term") {
			siCreatePaymentTerm = v
		}
		if v, ok := input.Str(jsonInput, "customerId"); ok && !cmd.Flags().Changed("customer-id") {
			siCreateCustomerID = v
		}
		if v, ok := input.Str(jsonInput, "mandateId"); ok && !cmd.Flags().Changed("mandate-id") {
			siCreateMandateID = v
		}
		if v, ok := input.Str(jsonInput, "recipientIdentifier"); ok && !cmd.Flags().Changed("recipient-identifier") {
			siCreateRecipientIdentifier = v
		}
		if v, ok := input.Bool(jsonInput, "isEInvoice"); ok && !cmd.Flags().Changed("is-e-invoice") {
			siCreateIsEInvoice = v
		}
	}

	status := components.SalesInvoiceStatus(siCreateStatus)
	if siCreateRecipientIdentifier == "" {
		return fmt.Errorf("required flag \"recipient-identifier\" not set")
	}

	recipient, err := resolveSalesInvoiceRecipient(jsonInput,
		siCreateRecipientType, siCreateRecipientTitle, siCreateRecipientGivenName, siCreateRecipientFamilyName,
		siCreateRecipientOrgName, siCreateRecipientOrgNumber, siCreateRecipientVatNumber, siCreateRecipientEmail,
		siCreateRecipientPhone, siCreateRecipientStreet, siCreateRecipientStreetAdditional, siCreateRecipientPostalCode,
		siCreateRecipientCity, siCreateRecipientRegion, siCreateRecipientCountry, siCreateRecipientLocale,
	)
	if err != nil {
		return err
	}
	if recipient == nil {
		return fmt.Errorf("recipient is required: set --recipient-type (and related --recipient-* flags) or provide a \"recipient\" object via stdin JSON")
	}

	lines, err := resolveSalesInvoiceLines(jsonInput,
		siCreateLineDescription, siCreateLineQuantity, siCreateLineVatRate, siCreateLineUnitPrice, siCreateCurrency,
	)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("at least one line item is required: set --line-description (and related --line-* flags) or provide a \"lines\" array via stdin JSON")
	}

	discount, err := resolveSalesInvoiceDiscount(jsonInput, siCreateDiscountType, siCreateDiscountValue)
	if err != nil {
		return err
	}
	paymentDetails, err := resolveSalesInvoicePaymentDetails(jsonInput, siCreatePaymentSource, siCreatePaymentSourceReference)
	if err != nil {
		return err
	}
	emailDetails, err := resolveSalesInvoiceEmailDetails(jsonInput, siCreateEmailSubject, siCreateEmailBody)
	if err != nil {
		return err
	}

	req := &components.SalesInvoiceRequest{
		Status:              status,
		RecipientIdentifier: siCreateRecipientIdentifier,
		Recipient:           recipient,
		Lines:               lines,
	}
	if siCreateProfileID != "" {
		req.ProfileID = &siCreateProfileID
	}
	if siCreateVatScheme != "" {
		vs := components.SalesInvoiceVatScheme(siCreateVatScheme)
		req.VatScheme = &vs
	}
	if siCreateVatMode != "" {
		vm := components.SalesInvoiceVatMode(siCreateVatMode)
		req.VatMode = &vm
	}
	if siCreateMemo != "" {
		req.Memo = &siCreateMemo
	}
	if siCreateMetadata != "" {
		meta, err := parseSalesInvoiceMetadata(siCreateMetadata)
		if err != nil {
			return fmt.Errorf("invalid --metadata: %w", err)
		}
		req.Metadata = meta
	}
	if siCreatePaymentTerm != "" {
		pt := components.SalesInvoicePaymentTerm(siCreatePaymentTerm)
		req.PaymentTerm = &pt
	}
	if siCreateCustomerID != "" {
		req.CustomerID = &siCreateCustomerID
	}
	if siCreateMandateID != "" {
		req.MandateID = &siCreateMandateID
	}
	if cmd.Flags().Changed("is-e-invoice") {
		req.IsEInvoice = &siCreateIsEInvoice
	}
	req.Discount = discount
	req.PaymentDetails = paymentDetails
	req.EmailDetails = emailDetails

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	resp, err := client.SalesInvoices.Create(context.Background(), nil, req)
	if err != nil {
		return fmt.Errorf("creating sales invoice: %w", err)
	}
	inv := resp.GetSalesInvoiceResponse()
	if inv == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(inv)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			salesInvoiceDetailRows(inv),
			!flagLive,
		)
	}
	return nil
}

func runSalesInvoicesList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	var from *string
	if siListFrom != "" {
		from = &siListFrom
	}

	resp, err := client.SalesInvoices.List(context.Background(), from, &siListLimit, nil, nil)
	if err != nil {
		return fmt.Errorf("listing sales invoices: %w", err)
	}
	if resp.Object == nil {
		return nil
	}
	if err := fixSalesInvoicesEmbedded(resp); err != nil {
		return err
	}

	embedded := resp.Object.GetEmbedded()
	invoices := embedded.GetSalesInvoices()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(invoices))
		for _, inv := range invoices {
			status := "—"
			if s := inv.GetStatus(); s != nil {
				status = string(*s)
			}
			totalStr := "—"
			if t := inv.GetTotalAmount(); t != nil {
				totalStr = fmt.Sprintf("%s %s", t.GetValue(), t.GetCurrency())
			}
			rows = append(rows, []string{
				inv.GetID(),
				derefOpt(inv.GetInvoiceNumber()),
				status,
				salesInvoiceListRecipientName(inv.GetRecipient()),
				totalStr,
				derefOpt(inv.GetIssuedAt()),
			})
		}
		output.PrintTable(
			[]string{"ID", "INVOICE NUMBER", "STATUS", "RECIPIENT", "TOTAL AMOUNT", "ISSUED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runSalesInvoicesGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	resp, err := client.SalesInvoices.Get(context.Background(), args[0], nil, nil)
	if err != nil {
		return fmt.Errorf("getting sales invoice: %w", err)
	}
	inv := resp.GetSalesInvoiceResponse()
	if inv == nil {
		return fmt.Errorf("sales invoice not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(inv)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			salesInvoiceDetailRows(inv),
			!flagLive,
		)
	}
	return nil
}

func runSalesInvoicesUpdate(cmd *cobra.Command, args []string) error {
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "status"); ok && !cmd.Flags().Changed("status") {
			siUpdateStatus = v
		}
		if v, ok := input.Str(jsonInput, "memo"); ok && !cmd.Flags().Changed("memo") {
			siUpdateMemo = v
		}
		if v, ok := input.Str(jsonInput, "paymentTerm"); ok && !cmd.Flags().Changed("payment-term") {
			siUpdatePaymentTerm = v
		}
		if v, ok := input.Str(jsonInput, "recipientIdentifier"); ok && !cmd.Flags().Changed("recipient-identifier") {
			siUpdateRecipientIdentifier = v
		}
		if v, ok := input.Bool(jsonInput, "isEInvoice"); ok && !cmd.Flags().Changed("is-e-invoice") {
			siUpdateIsEInvoice = v
		}
	}

	recipient, err := resolveSalesInvoiceRecipient(jsonInput,
		siUpdateRecipientType, siUpdateRecipientTitle, siUpdateRecipientGivenName, siUpdateRecipientFamilyName,
		siUpdateRecipientOrgName, siUpdateRecipientOrgNumber, siUpdateRecipientVatNumber, siUpdateRecipientEmail,
		siUpdateRecipientPhone, siUpdateRecipientStreet, siUpdateRecipientStreetAdditional, siUpdateRecipientPostalCode,
		siUpdateRecipientCity, siUpdateRecipientRegion, siUpdateRecipientCountry, siUpdateRecipientLocale,
	)
	if err != nil {
		return err
	}
	lines, err := resolveSalesInvoiceLines(jsonInput,
		siUpdateLineDescription, siUpdateLineQuantity, siUpdateLineVatRate, siUpdateLineUnitPrice, siUpdateCurrency,
	)
	if err != nil {
		return err
	}
	discount, err := resolveSalesInvoiceDiscount(jsonInput, siUpdateDiscountType, siUpdateDiscountValue)
	if err != nil {
		return err
	}
	paymentDetails, err := resolveSalesInvoicePaymentDetails(jsonInput, siUpdatePaymentSource, siUpdatePaymentSourceReference)
	if err != nil {
		return err
	}
	emailDetails, err := resolveSalesInvoiceEmailDetails(jsonInput, siUpdateEmailSubject, siUpdateEmailBody)
	if err != nil {
		return err
	}

	body := &operations.UpdateSalesInvoiceRequestBody{}
	if siUpdateStatus != "" {
		st := components.SalesInvoiceStatusUpdate(siUpdateStatus)
		body.Status = &st
	}
	if siUpdateMemo != "" {
		body.Memo = &siUpdateMemo
	}
	if siUpdatePaymentTerm != "" {
		pt := components.SalesInvoicePaymentTerm(siUpdatePaymentTerm)
		body.PaymentTerm = &pt
	}
	if siUpdateRecipientIdentifier != "" {
		body.RecipientIdentifier = &siUpdateRecipientIdentifier
	}
	if cmd.Flags().Changed("is-e-invoice") {
		body.IsEInvoice = &siUpdateIsEInvoice
	}
	body.Recipient = recipient
	body.Lines = lines
	body.Discount = discount
	body.PaymentDetails = paymentDetails
	body.EmailDetails = emailDetails

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	resp, err := client.SalesInvoices.Update(context.Background(), args[0], nil, body)
	if err != nil {
		return fmt.Errorf("updating sales invoice: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.GetSalesInvoiceResponse())
	default:
		fmt.Printf("✓ Sales invoice %s updated\n", args[0])
	}
	return nil
}

func runSalesInvoicesDelete(_ *cobra.Command, args []string) error {
	invoiceID := args[0]

	if !siDeleteConfirm && !flagYes {
		confirmed, err := prompt.Confirm(fmt.Sprintf("Delete sales invoice %s? Only draft invoices can be deleted.", invoiceID))
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

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	if _, err := client.SalesInvoices.Delete(context.Background(), invoiceID, nil, nil); err != nil {
		return fmt.Errorf("deleting sales invoice: %w", err)
	}

	fmt.Printf("✓ Sales invoice %s deleted\n", invoiceID)
	return nil
}

// ── nested object resolvers ───────────────────────────────────────────────────
//
// Each resolver builds a nested request object from its flags. If the
// section's primary flag was not set, it falls back to the matching key in
// the raw stdin JSON map (when present). This follows the CLI-wide
// precedence rule (CLI flags > stdin JSON) at the granularity of a whole
// nested object, since the flags above always build a complete object.

func resolveSalesInvoiceRecipient(
	jsonInput map[string]json.RawMessage,
	recipientType, title, givenName, familyName, orgName, orgNumber, vatNumber,
	email, phone, street, streetAdditional, postalCode, city, region, country, locale string,
) (*components.SalesInvoiceRecipient, error) {
	if recipientType == "" {
		if jsonInput != nil {
			if raw, ok := jsonInput["recipient"]; ok {
				var r components.SalesInvoiceRecipient
				if err := json.Unmarshal(raw, &r); err != nil {
					return nil, fmt.Errorf("invalid \"recipient\" in stdin JSON: %w", err)
				}
				return &r, nil
			}
		}
		return nil, nil
	}

	r := &components.SalesInvoiceRecipient{
		Type:            components.SalesInvoiceRecipientType(recipientType),
		Email:           email,
		StreetAndNumber: street,
		PostalCode:      postalCode,
		City:            city,
		Country:         country,
		Locale:          components.SalesInvoiceRecipientLocale(locale),
	}
	if title != "" {
		r.Title = &title
	}
	if givenName != "" {
		r.GivenName = &givenName
	}
	if familyName != "" {
		r.FamilyName = &familyName
	}
	if orgName != "" {
		r.OrganizationName = &orgName
	}
	if orgNumber != "" {
		r.OrganizationNumber = &orgNumber
	}
	if vatNumber != "" {
		r.VatNumber = &vatNumber
	}
	if phone != "" {
		r.Phone = &phone
	}
	if streetAdditional != "" {
		r.StreetAdditional = &streetAdditional
	}
	if region != "" {
		r.Region = &region
	}
	return r, nil
}

func resolveSalesInvoiceLines(
	jsonInput map[string]json.RawMessage,
	description string, quantity int64, vatRate, unitPrice, currency string,
) ([]components.SalesInvoiceLineItem, error) {
	if description == "" {
		if jsonInput != nil {
			if raw, ok := jsonInput["lines"]; ok {
				var lines []components.SalesInvoiceLineItem
				if err := json.Unmarshal(raw, &lines); err != nil {
					return nil, fmt.Errorf("invalid \"lines\" in stdin JSON: %w", err)
				}
				return lines, nil
			}
		}
		return nil, nil
	}

	if vatRate == "" {
		return nil, fmt.Errorf("required flag \"line-vat-rate\" not set")
	}
	if unitPrice == "" {
		return nil, fmt.Errorf("required flag \"line-unit-price\" not set")
	}

	return []components.SalesInvoiceLineItem{{
		Description: description,
		Quantity:    quantity,
		VatRate:     vatRate,
		UnitPrice: components.Amount{
			Currency: currency,
			Value:    unitPrice,
		},
	}}, nil
}

func resolveSalesInvoiceDiscount(
	jsonInput map[string]json.RawMessage,
	discountType, value string,
) (*components.SalesInvoiceDiscount, error) {
	if discountType == "" {
		if jsonInput != nil {
			if raw, ok := jsonInput["discount"]; ok {
				var d components.SalesInvoiceDiscount
				if err := json.Unmarshal(raw, &d); err != nil {
					return nil, fmt.Errorf("invalid \"discount\" in stdin JSON: %w", err)
				}
				return &d, nil
			}
		}
		return nil, nil
	}
	if value == "" {
		return nil, fmt.Errorf("--discount-value is required when --discount-type is set")
	}
	return &components.SalesInvoiceDiscount{
		Type:  components.SalesInvoiceDiscountType(discountType),
		Value: value,
	}, nil
}

func resolveSalesInvoicePaymentDetails(
	jsonInput map[string]json.RawMessage,
	source, sourceReference string,
) (*components.SalesInvoicePaymentDetails, error) {
	if source == "" {
		if jsonInput != nil {
			if raw, ok := jsonInput["paymentDetails"]; ok {
				var pd components.SalesInvoicePaymentDetails
				if err := json.Unmarshal(raw, &pd); err != nil {
					return nil, fmt.Errorf("invalid \"paymentDetails\" in stdin JSON: %w", err)
				}
				return &pd, nil
			}
		}
		return nil, nil
	}
	pd := &components.SalesInvoicePaymentDetails{
		Source: components.SalesInvoicePaymentDetailsSource(source),
	}
	if sourceReference != "" {
		pd.SourceReference = &sourceReference
	} else if source != string(components.SalesInvoicePaymentDetailsSourceManual) {
		return nil, fmt.Errorf("--payment-source-reference is required unless --payment-source is manual")
	}
	return pd, nil
}

func resolveSalesInvoiceEmailDetails(
	jsonInput map[string]json.RawMessage,
	subject, body string,
) (*components.SalesInvoiceEmailDetails, error) {
	if subject == "" && body == "" {
		if jsonInput != nil {
			if raw, ok := jsonInput["emailDetails"]; ok {
				var ed components.SalesInvoiceEmailDetails
				if err := json.Unmarshal(raw, &ed); err != nil {
					return nil, fmt.Errorf("invalid \"emailDetails\" in stdin JSON: %w", err)
				}
				return &ed, nil
			}
		}
		return nil, nil
	}
	if subject == "" || body == "" {
		return nil, fmt.Errorf("--email-subject and --email-body must be set together")
	}
	return &components.SalesInvoiceEmailDetails{Subject: subject, Body: body}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// fixSalesInvoicesEmbedded works around a bug in mollie-api-golang v1.3.42:
// ListSalesInvoicesEmbedded.SalesInvoices is tagged `json:"sales_invoices"`,
// but the live API nests the array under `_embedded.invoices`, so the SDK's
// own unmarshalling always leaves it empty. The SDK deliberately re-buffers
// the response body after consuming it (see ConsumeRawBody) for exactly this
// kind of custom re-parsing, so re-read it here and patch the typed field in
// place before it's rendered as a table or re-marshalled as JSON output.
func fixSalesInvoicesEmbedded(resp *operations.ListSalesInvoicesResponse) error {
	meta := resp.GetHTTPMeta()
	httpResp := meta.GetResponse()
	if httpResp == nil || httpResp.Body == nil {
		return nil
	}
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	httpResp.Body = io.NopCloser(bytes.NewBuffer(data))

	var body struct {
		Embedded struct {
			Invoices []components.ListSalesInvoiceResponse `json:"invoices"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return fmt.Errorf("parsing response body: %w", err)
	}

	if resp.Object.Embedded == nil {
		resp.Object.Embedded = &operations.ListSalesInvoicesEmbedded{}
	}
	resp.Object.Embedded.SalesInvoices = body.Embedded.Invoices
	return nil
}

// parseSalesInvoiceMetadata converts a raw JSON string into the map[string]any
// shape SalesInvoiceRequest.Metadata expects.
func parseSalesInvoiceMetadata(raw string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("must be a valid JSON object: %w", err)
	}
	return m, nil
}

// salesInvoiceListRecipientName picks a display name for a list-row recipient:
// given+family name, else organisation name, else email.
func salesInvoiceListRecipientName(r *components.SalesInvoiceRecipientResponse) string {
	if r == nil {
		return "—"
	}
	given, family := derefOpt(r.GetGivenName()), derefOpt(r.GetFamilyName())
	if given != "—" || family != "—" {
		name := given
		if family != "—" {
			if name != "—" {
				name += " "
			} else {
				name = ""
			}
			name += family
		}
		if name != "" {
			return name
		}
	}
	if org := r.GetOrganizationName(); org != nil && *org != "" {
		return *org
	}
	return r.GetEmail()
}

// salesInvoiceDetailRows converts a SalesInvoiceResponse into the key/value
// rows shown by `sales-invoices get`/`create` (table mode).
func salesInvoiceDetailRows(inv *components.SalesInvoiceResponse) [][]string {
	row := func(k, v string) []string { return []string{k, v} }

	status := "—"
	if s := inv.GetStatus(); s != nil {
		status = string(*s)
	}
	eInvoiceStatus := "—"
	if s := inv.GetEInvoiceStatus(); s != nil {
		eInvoiceStatus = string(*s)
	}
	vatScheme := "—"
	if s := inv.GetVatScheme(); s != nil {
		vatScheme = string(*s)
	}
	vatMode := "—"
	if s := inv.GetVatMode(); s != nil {
		vatMode = string(*s)
	}
	paymentTerm := "—"
	if s := inv.GetPaymentTerm(); s != nil {
		paymentTerm = string(*s)
	}
	isEInvoice := "—"
	if v := inv.GetIsEInvoice(); v != nil {
		isEInvoice = fmt.Sprintf("%v", *v)
	}

	subtotal := "—"
	if a := inv.GetSubtotalAmount(); a != nil {
		subtotal = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}
	discountedSubtotal := "—"
	if a := inv.GetDiscountedSubtotalAmount(); a != nil {
		discountedSubtotal = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}
	totalVat := "—"
	if a := inv.GetTotalVatAmount(); a != nil {
		totalVat = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}
	total := "—"
	if a := inv.GetTotalAmount(); a != nil {
		total = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}
	amountDue := "—"
	if a := inv.GetAmountDue(); a != nil {
		amountDue = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}

	recipientName, recipientEmail, recipientAddress := "—", "—", "—"
	if r := inv.GetRecipient(); r != nil {
		recipientEmail = r.GetEmail()
		given, family := derefOpt(r.GetGivenName()), derefOpt(r.GetFamilyName())
		switch {
		case given != "—" && family != "—":
			recipientName = given + " " + family
		case given != "—":
			recipientName = given
		case family != "—":
			recipientName = family
		default:
			if org := r.GetOrganizationName(); org != nil && *org != "" {
				recipientName = *org
			}
		}
		streetAdditional := ""
		if sa := r.GetStreetAdditional(); sa != nil && *sa != "" {
			streetAdditional = ", " + *sa
		}
		recipientAddress = fmt.Sprintf("%s%s, %s %s, %s", r.GetStreetAndNumber(), streetAdditional, r.GetPostalCode(), r.GetCity(), r.GetCountry())
	}

	discountStr := "—"
	if d := inv.GetDiscount(); d != nil {
		discountStr = fmt.Sprintf("%s (%s)", d.GetValue(), string(d.GetType()))
	}

	return [][]string{
		row("ID", inv.GetID()),
		row("Mode", string(inv.GetMode())),
		row("Invoice Number", derefOpt(inv.GetInvoiceNumber())),
		row("Status", status),
		row("E-Invoice Status", eInvoiceStatus),
		row("VAT Scheme", vatScheme),
		row("VAT Mode", vatMode),
		row("Memo", derefOpt(inv.GetMemo())),
		row("Payment Term", paymentTerm),
		row("Customer ID", derefOpt(inv.GetCustomerID())),
		row("Mandate ID", derefOpt(inv.GetMandateID())),
		row("Recipient Identifier", derefOpt(inv.GetRecipientIdentifier())),
		row("Recipient Name", recipientName),
		row("Recipient Email", recipientEmail),
		row("Recipient Address", recipientAddress),
		row("Line Items", fmt.Sprintf("%d", len(inv.GetLines()))),
		row("Discount", discountStr),
		row("Is E-Invoice", isEInvoice),
		row("Subtotal Amount", subtotal),
		row("Discounted Subtotal", discountedSubtotal),
		row("Total VAT Amount", totalVat),
		row("Total Amount", total),
		row("Amount Due", amountDue),
		row("Created At", derefOpt(inv.GetCreatedAt())),
		row("Issued At", derefOpt(inv.GetIssuedAt())),
		row("Paid At", derefOpt(inv.GetPaidAt())),
		row("Due At", derefOpt(inv.GetDueAt())),
	}
}
