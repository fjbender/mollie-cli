package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

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
	payCreateAmount       string
	payCreateCurrency     string
	payCreateDescription  string
	payCreateRedirectURL  string
	payCreateMethod       string
	payCreateCustomerID   string
	payCreateSequenceType string
	payCreateWebhookURL   string
	payCreateMetadata     string

	// list flags
	payListLimit int64
	payListFrom  string

	// update flags
	payUpdateDescription string
	payUpdateRedirectURL string
	payUpdateWebhookURL  string
	payUpdateMetadata    string

	// cancel flag
	payConfirm bool

	// capture mode flag
	payCreateCaptureMode string

	// locale flag
	payCreateLocale string

	// --with-lines flags
	payCreateWithLines     bool
	payCreateWithDiscount  bool
	payCreateLinesVatRate  string
	payCreateLinesShipping string

	// --with-billing flags
	payCreateWithBilling       bool
	payCreateBillingGivenName  string
	payCreateBillingFamilyName string
	payCreateBillingEmail      string
	payCreateBillingPhone      string
	payCreateBillingOrg        string
	payCreateBillingStreet     string
	payCreateBillingStreetAdl  string
	payCreateBillingPostalCode string
	payCreateBillingCity       string
	payCreateBillingRegion     string
	payCreateBillingCountry    string

	// --with-shipping flags
	payCreateWithShipping       bool
	payCreateShippingGivenName  string
	payCreateShippingFamilyName string
	payCreateShippingEmail      string
	payCreateShippingPhone      string
	payCreateShippingOrg        string
	payCreateShippingStreet     string
	payCreateShippingStreetAdl  string
	payCreateShippingPostalCode string
	payCreateShippingCity       string
	payCreateShippingRegion     string
	payCreateShippingCountry    string
)

// ── command tree ─────────────────────────────────────────────────────────────

var paymentsCmd = &cobra.Command{
	Use:   "payments",
	Short: "Manage Mollie payments",
}

var paymentsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new payment",
	Long: `Create a new Mollie payment.

--with-lines auto-generates order lines that sum exactly to --amount:
  • 2 physical item lines (~60/40 split of the product sub-total)
  • 1 shipping_fee line (default 4.99; override with --lines-shipping-amount)
  • VAT applied to every line at --lines-vat-rate (default 21.00%)

Add --with-discount to append a ~10% discount line; requires --with-lines.

--with-billing / --with-shipping attach a Dutch test address; override fields with --billing-* / --shipping-*.`,
	RunE:        runPaymentsCreate,
	Annotations: map[string]string{"usesDefaults": "true"},
}

var paymentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List payments",
	RunE:  runPaymentsList,
}

var paymentsGetCmd = &cobra.Command{
	Use:   "get <payment-id>",
	Short: "Get a single payment",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaymentsGet,
}

var paymentsUpdateCmd = &cobra.Command{
	Use:   "update <payment-id>",
	Short: "Update mutable payment fields (description, redirect URL, webhook URL, metadata)",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaymentsUpdate,
}

var paymentsCancelCmd = &cobra.Command{
	Use:   "cancel <payment-id>",
	Short: "Cancel a cancelable payment",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaymentsCancel,
}

func init() {
	// create flags
	paymentsCreateCmd.Flags().StringVar(&payCreateAmount, "amount", "", "Payment amount, e.g. 10.00 (required; falls back to `defaults set --amount`)")
	paymentsCreateCmd.Flags().StringVar(&payCreateCurrency, "currency", "", "ISO 4217 currency code, e.g. EUR (required; falls back to `defaults set --currency`)")
	paymentsCreateCmd.Flags().StringVar(&payCreateDescription, "description", "", "Payment description shown to the customer (required; falls back to `defaults set --description`)")
	paymentsCreateCmd.Flags().StringVar(&payCreateRedirectURL, "redirect-url", "", "URL to redirect the customer to after payment (required; falls back to `defaults set --redirect-url`)")
	paymentsCreateCmd.Flags().StringVar(&payCreateMethod, "method", "", "Payment method, e.g. ideal, creditcard")
	paymentsCreateCmd.Flags().StringVar(&payCreateCustomerID, "customer-id", "", "Link this payment to a Customer ID")
	paymentsCreateCmd.Flags().StringVar(&payCreateSequenceType, "sequence-type", "", "Sequence type: oneoff, first, or recurring")
	paymentsCreateCmd.Flags().StringVar(&payCreateWebhookURL, "webhook-url", "", "Webhook URL for payment status updates (falls back to `defaults set --webhook-url`)")
	paymentsCreateCmd.Flags().StringVar(&payCreateMetadata, "metadata", "", "Arbitrary JSON metadata to attach to the payment")
	paymentsCreateCmd.Flags().StringVar(&payCreateCaptureMode, "capture-mode", "", "Capture mode: automatic or manual")
	paymentsCreateCmd.Flags().StringVar(&payCreateLocale, "locale", "", "Locale for the payment, e.g. en_US, nl_NL (determines checkout language)")

	// --with-lines flags
	paymentsCreateCmd.Flags().BoolVar(&payCreateWithLines, "with-lines", false, "Auto-generate order lines summing to --amount (always 2 item lines + 1 shipping line)")
	paymentsCreateCmd.Flags().BoolVar(&payCreateWithDiscount, "with-discount", false, "Add a discount line; requires --with-lines")
	paymentsCreateCmd.Flags().StringVar(&payCreateLinesVatRate, "lines-vat-rate", "21.00", "VAT rate for generated lines, e.g. 21.00 (default 21.00)")
	paymentsCreateCmd.Flags().StringVar(&payCreateLinesShipping, "lines-shipping-amount", "", "Shipping line amount (e.g. 4.99); defaults to 4.99 when omitted; must be smaller than --amount")

	// --with-billing flags
	paymentsCreateCmd.Flags().BoolVar(&payCreateWithBilling, "with-billing", false, "Add a default billing address (NL test address); override individual fields with --billing-* flags")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingGivenName, "billing-given-name", "", "Billing given name")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingFamilyName, "billing-family-name", "", "Billing family name")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingEmail, "billing-email", "", "Billing e-mail address")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingPhone, "billing-phone", "", "Billing phone number (E.164)")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingOrg, "billing-org", "", "Billing organisation name")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingStreet, "billing-street", "", "Billing street and number")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingStreetAdl, "billing-street-additional", "", "Billing street additional info")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingPostalCode, "billing-postal-code", "", "Billing postal code")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingCity, "billing-city", "", "Billing city")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingRegion, "billing-region", "", "Billing region / state")
	paymentsCreateCmd.Flags().StringVar(&payCreateBillingCountry, "billing-country", "", "Billing ISO 3166-1 alpha-2 country code")

	// --with-shipping flags
	paymentsCreateCmd.Flags().BoolVar(&payCreateWithShipping, "with-shipping", false, "Add a default shipping address (NL test address); override individual fields with --shipping-* flags")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingGivenName, "shipping-given-name", "", "Shipping given name")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingFamilyName, "shipping-family-name", "", "Shipping family name")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingEmail, "shipping-email", "", "Shipping e-mail address")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingPhone, "shipping-phone", "", "Shipping phone number (E.164)")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingOrg, "shipping-org", "", "Shipping organisation name")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingStreet, "shipping-street", "", "Shipping street and number")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingStreetAdl, "shipping-street-additional", "", "Shipping street additional info")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingPostalCode, "shipping-postal-code", "", "Shipping postal code")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingCity, "shipping-city", "", "Shipping city")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingRegion, "shipping-region", "", "Shipping region / state")
	paymentsCreateCmd.Flags().StringVar(&payCreateShippingCountry, "shipping-country", "", "Shipping ISO 3166-1 alpha-2 country code")

	// list flags
	paymentsListCmd.Flags().Int64Var(&payListLimit, "limit", 50, "Maximum number of results to return")
	paymentsListCmd.Flags().StringVar(&payListFrom, "from", "", "Return results starting from this payment ID (cursor pagination)")

	// update flags
	paymentsUpdateCmd.Flags().StringVar(&payUpdateDescription, "description", "", "New description")
	paymentsUpdateCmd.Flags().StringVar(&payUpdateRedirectURL, "redirect-url", "", "New redirect URL")
	paymentsUpdateCmd.Flags().StringVar(&payUpdateWebhookURL, "webhook-url", "", "New webhook URL")
	paymentsUpdateCmd.Flags().StringVar(&payUpdateMetadata, "metadata", "", "New metadata (JSON string)")

	// cancel flag
	paymentsCancelCmd.Flags().BoolVar(&payConfirm, "confirm", false, "Skip the confirmation prompt")

	paymentsCmd.AddCommand(paymentsCreateCmd)
	paymentsCmd.AddCommand(paymentsListCmd)
	paymentsCmd.AddCommand(paymentsGetCmd)
	paymentsCmd.AddCommand(paymentsUpdateCmd)
	paymentsCmd.AddCommand(paymentsCancelCmd)
	rootCmd.AddCommand(paymentsCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runPaymentsCreate(cmd *cobra.Command, _ []string) error {
	// ── JSON stdin input ─────────────────────────────────────────────────────
	// Flags take highest precedence; JSON overrides only fields not explicitly
	// set on the command line. Config defaults fill in whatever remains.
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "description"); ok && !cmd.Flags().Changed("description") {
			payCreateDescription = v
		}
		if val, cur, ok := input.Amount(jsonInput, "amount"); ok {
			if !cmd.Flags().Changed("amount") {
				payCreateAmount = val
			}
			if !cmd.Flags().Changed("currency") {
				payCreateCurrency = cur
			}
		}
		if v, ok := input.Str(jsonInput, "redirectUrl"); ok && !cmd.Flags().Changed("redirect-url") {
			payCreateRedirectURL = v
		}
		if v, ok := input.Str(jsonInput, "webhookUrl"); ok && !cmd.Flags().Changed("webhook-url") {
			payCreateWebhookURL = v
		}
		if v, ok := input.Str(jsonInput, "method"); ok && !cmd.Flags().Changed("method") {
			payCreateMethod = v
		}
		if v, ok := input.Str(jsonInput, "customerId"); ok && !cmd.Flags().Changed("customer-id") {
			payCreateCustomerID = v
		}
		if v, ok := input.Str(jsonInput, "sequenceType"); ok && !cmd.Flags().Changed("sequence-type") {
			payCreateSequenceType = v
		}
		if v, ok := input.Str(jsonInput, "captureMode"); ok && !cmd.Flags().Changed("capture-mode") {
			payCreateCaptureMode = v
		}
		if v, ok := input.Str(jsonInput, "locale"); ok && !cmd.Flags().Changed("locale") {
			payCreateLocale = v
		}
		if v, ok := input.RawJSON(jsonInput, "metadata"); ok && !cmd.Flags().Changed("metadata") {
			payCreateMetadata = v
		}
	}

	applyCreateDefaults(cmd,
		&payCreateDescription, &payCreateAmount, &payCreateCurrency,
		&payCreateRedirectURL, &payCreateWebhookURL,
	)

	// Validate fields that are required but may come from defaults.
	switch {
	case payCreateAmount == "":
		return fmt.Errorf("required flag \"amount\" not set and no default configured (run `mollie defaults set`)")
	case payCreateCurrency == "":
		return fmt.Errorf("required flag \"currency\" not set and no default configured (run `mollie defaults set`)")
	case payCreateDescription == "":
		return fmt.Errorf("required flag \"description\" not set and no default configured (run `mollie defaults set`)")
	case payCreateRedirectURL == "":
		return fmt.Errorf("required flag \"redirect-url\" not set and no default configured (run `mollie defaults set`)")
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	// ── Build request ─────────────────────────────────────────────────────────
	//
	// Seed req from the full JSON object first so that fields without a flag
	// equivalent (billingAddress, shippingAddress, lines, cancelUrl, issuer,
	// restrictPaymentMethodsToCountry, …) are forwarded to the API as-is.
	// Resolved flag-mapped values are applied on top afterwards, preserving the
	// flag > JSON stdin > config default precedence for every field that has a
	// CLI flag.
	//
	// We extract each pass-through field individually rather than doing a single
	// json.Unmarshal(rawBytes, req) round-trip.  The reason: PaymentRequest uses
	// a Speakeasy-generated custom UnmarshalJSON that processes struct fields in
	// declaration order and returns on the first error.  A single type mismatch
	// (e.g. vatRate supplied as a JSON number instead of a quoted string) would
	// silently abort the whole unmarshal, leaving every subsequent field —
	// including billingAddress — unset.
	req := &components.PaymentRequest{}
	if jsonInput != nil {
		seedPassThroughFields(req, jsonInput)
	}

	// Always apply the resolved flag-mapped values (these override whatever the
	// JSON seed may have set for these particular fields).
	req.Description = payCreateDescription
	req.Amount = components.Amount{
		Currency: payCreateCurrency,
		Value:    payCreateAmount,
	}
	req.RedirectURL = &payCreateRedirectURL

	// Reset optional flag-mapped pointer fields so stale JSON-seeded values
	// don't bleed through when neither a flag nor JSON specified them.
	req.WebhookURL = nil
	req.CustomerID = nil
	req.SequenceType = nil
	req.Method = nil
	req.CaptureMode = nil
	req.Locale = nil
	req.Metadata = nil

	if payCreateWebhookURL != "" {
		req.WebhookURL = &payCreateWebhookURL
	}
	if payCreateCustomerID != "" {
		req.CustomerID = &payCreateCustomerID
	}
	if payCreateSequenceType != "" {
		st := components.SequenceType(payCreateSequenceType)
		req.SequenceType = &st
	}
	if payCreateMethod != "" {
		me := components.MethodEnum(payCreateMethod)
		m := components.CreateMethodMethodEnum(me)
		req.Method = &m
	}
	if payCreateCaptureMode != "" {
		cm := components.CaptureMode(payCreateCaptureMode)
		req.CaptureMode = &cm
	}
	if payCreateLocale != "" {
		l := components.Locale(payCreateLocale)
		req.Locale = &l
	}
	if payCreateMetadata != "" {
		meta, err := parseMetadata(payCreateMetadata)
		if err != nil {
			return fmt.Errorf("invalid --metadata: %w", err)
		}
		req.Metadata = &meta
	}

	// --with-lines / --with-billing / --with-shipping: these flags override any
	// lines / addresses that were seeded from JSON stdin.
	if payCreateWithDiscount && !payCreateWithLines {
		return fmt.Errorf("--with-discount requires --with-lines")
	}
	if payCreateWithLines {
		lines, err := buildPaymentLines(payCreateCurrency, payCreateAmount, payCreateDescription, payCreateLinesVatRate, payCreateLinesShipping, payCreateWithDiscount)
		if err != nil {
			return err
		}
		req.Lines = lines
	}
	if payCreateWithBilling {
		req.BillingAddress = buildBillingAddress()
	}
	if payCreateWithShipping {
		req.ShippingAddress = buildShippingAddress()
	}

	resp, err := client.Payments.Create(context.Background(), nil, nil, req)
	if err != nil {
		return fmt.Errorf("creating payment: %w", err)
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

func runPaymentsList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := operations.ListPaymentsRequest{
		Limit: &payListLimit,
	}
	if payListFrom != "" {
		req.From = &payListFrom
	}

	resp, err := client.Payments.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing payments: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	embedded := resp.Object.GetEmbedded()
	payments := embedded.GetPayments()

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

func runPaymentsGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Payments.Get(context.Background(), operations.GetPaymentRequest{
		PaymentID: args[0],
	})
	if err != nil {
		return fmt.Errorf("getting payment: %w", err)
	}
	pay := resp.GetPaymentResponse()
	if pay == nil {
		return fmt.Errorf("payment not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(pay)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			paymentDetailRows(pay),
			!flagLive,
		)
	}
	return nil
}

func runPaymentsUpdate(cmd *cobra.Command, args []string) error {
	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "description"); ok && !cmd.Flags().Changed("description") {
			payUpdateDescription = v
		}
		if v, ok := input.Str(jsonInput, "redirectUrl"); ok && !cmd.Flags().Changed("redirect-url") {
			payUpdateRedirectURL = v
		}
		if v, ok := input.Str(jsonInput, "webhookUrl"); ok && !cmd.Flags().Changed("webhook-url") {
			payUpdateWebhookURL = v
		}
		if v, ok := input.RawJSON(jsonInput, "metadata"); ok && !cmd.Flags().Changed("metadata") {
			payUpdateMetadata = v
		}
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	body := &operations.UpdatePaymentRequestBody{}
	if payUpdateDescription != "" {
		body.Description = &payUpdateDescription
	}
	if payUpdateRedirectURL != "" {
		body.RedirectURL = &payUpdateRedirectURL
	}
	if payUpdateWebhookURL != "" {
		body.WebhookURL = &payUpdateWebhookURL
	}
	if payUpdateMetadata != "" {
		meta, err := parseMetadata(payUpdateMetadata)
		if err != nil {
			return fmt.Errorf("invalid --metadata: %w", err)
		}
		body.Metadata = &meta
	}

	resp, err := client.Payments.Update(context.Background(), args[0], nil, body)
	if err != nil {
		return fmt.Errorf("updating payment: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp)
	default:
		fmt.Printf("✓ Payment %s updated\n", args[0])
	}
	return nil
}

func runPaymentsCancel(_ *cobra.Command, args []string) error {
	paymentID := args[0]

	if !payConfirm {
		confirmed, err := prompt.Confirm(fmt.Sprintf("Cancel payment %s?", paymentID))
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

	if _, err := client.Payments.Cancel(context.Background(), paymentID, nil, nil); err != nil {
		return fmt.Errorf("canceling payment: %w", err)
	}

	fmt.Printf("✓ Payment %s canceled\n", paymentID)
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// formatAmount returns a human-readable "VALUE CURRENCY" string.
func formatAmount(a components.Amount) string {
	return fmt.Sprintf("%s %s", a.Value, a.Currency)
}

// derefOpt returns the pointed-to string or "—" when the pointer is nil.
func derefOpt(s *string) string {
	if s == nil {
		return "—"
	}
	return *s
}

// paymentDetailRows converts a PaymentResponse into the key/value rows shown
// by `payments get` (table mode). All available fields are included; optional
// fields that are not set are rendered as "—" so the layout is stable.
func paymentDetailRows(p *components.PaymentResponse) [][]string {
	row := func(k, v string) []string { return []string{k, v} }

	// Build optional amount helpers inline.
	amtRefunded := "—"
	if a := p.GetAmountRefunded(); a != nil {
		amtRefunded = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}
	amtRemaining := "—"
	if a := p.GetAmountRemaining(); a != nil {
		amtRemaining = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}
	amtCaptured := "—"
	if a := p.GetAmountCaptured(); a != nil {
		amtCaptured = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}
	amtChargedBack := "—"
	if a := p.GetAmountChargedBack(); a != nil {
		amtChargedBack = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}
	amtSettlement := "—"
	if a := p.GetSettlementAmount(); a != nil {
		amtSettlement = fmt.Sprintf("%s %s", a.GetValue(), a.GetCurrency())
	}

	isCancelable := "—"
	if v := p.GetIsCancelable(); v != nil {
		if *v {
			isCancelable = "true"
		} else {
			isCancelable = "false"
		}
	}

	method := "—"
	if m := p.GetMethod(); m != nil {
		method = string(*m)
	}

	captureMode := "—"
	if cm := p.GetCaptureMode(); cm != nil {
		captureMode = string(*cm)
	}

	locale := "—"
	if l := p.GetLocale(); l != nil {
		locale = string(*l)
	}

	metaStr := "—"
	if m := p.GetMetadata(); m != nil {
		if b, err := json.Marshal(m); err == nil {
			metaStr = string(b)
		}
	}

	checkoutURL := "—"
	if co := p.Links.GetCheckout(); co != nil {
		checkoutURL = co.GetHref()
	}
	mobileCheckoutURL := "—"
	if mc := p.Links.GetMobileAppCheckout(); mc != nil {
		mobileCheckoutURL = mc.GetHref()
	}

	return [][]string{
		// Identity
		row("ID", p.GetID()),
		row("Mode", string(p.GetMode())),
		row("Status", string(p.GetStatus())),
		row("Is Cancelable", isCancelable),
		// Amounts
		row("Amount", formatAmount(p.Amount)),
		row("Amount Refunded", amtRefunded),
		row("Amount Remaining", amtRemaining),
		row("Amount Captured", amtCaptured),
		row("Amount Charged Back", amtChargedBack),
		row("Settlement Amount", amtSettlement),
		// Payment details
		row("Description", p.GetDescription()),
		row("Method", method),
		row("Sequence Type", dash(string(p.GetSequenceType()))),
		row("Capture Mode", captureMode),
		row("Locale", locale),
		row("Country Code", derefOpt(p.GetCountryCode())),
		// References
		row("Profile ID", p.GetProfileID()),
		row("Customer ID", derefOpt(p.GetCustomerID())),
		row("Mandate ID", derefOpt(p.GetMandateID())),
		row("Subscription ID", derefOpt(p.GetSubscriptionID())),
		row("Order ID", derefOpt(p.GetOrderID())),
		row("Settlement ID", derefOpt(p.GetSettlementID())),
		// URLs
		row("Redirect URL", derefOpt(p.GetRedirectURL())),
		row("Cancel URL", derefOpt(p.GetCancelURL())),
		row("Webhook URL", derefOpt(p.GetWebhookURL())),
		row("Checkout URL", checkoutURL),
		row("Mobile App Checkout URL", mobileCheckoutURL),
		// Timestamps
		row("Created At", p.GetCreatedAt()),
		row("Expires At", derefOpt(p.GetExpiresAt())),
		row("Authorized At", derefOpt(p.GetAuthorizedAt())),
		row("Paid At", derefOpt(p.GetPaidAt())),
		row("Canceled At", derefOpt(p.GetCanceledAt())),
		row("Expired At", derefOpt(p.GetExpiredAt())),
		row("Failed At", derefOpt(p.GetFailedAt())),
		// Metadata (last — can be long)
		row("Metadata", metaStr),
	}
}

// ── convenience helpers: lines & addresses ───────────────────────────────────

// defaultAddr* are the pre-filled values used when --with-billing / --with-shipping
// is passed without an explicit override for that field.
const (
	defaultAddrGivenName  = "John"
	defaultAddrFamilyName = "Doe"
	defaultAddrEmail      = "john.doe@example.com"
	defaultAddrStreet     = "Keizersgracht 126"
	defaultAddrPostalCode = "1015 CW"
	defaultAddrCity       = "Amsterdam"
	defaultAddrCountry    = "NL"
)

// strPtr returns a pointer to s.
func strPtr(s string) *string { return &s }

// overrideOrDefault returns a pointer to override when it is non-empty,
// otherwise a pointer to def.
func overrideOrDefault(def, override string) *string {
	if override != "" {
		return &override
	}
	return &def
}

// buildBillingAddress builds a PaymentRequestBillingAddress with sensible NL
// test-mode defaults. Any --billing-* flag that is non-empty overrides the
// corresponding default.
func buildBillingAddress() *components.PaymentRequestBillingAddress {
	addr := &components.PaymentRequestBillingAddress{
		GivenName:       overrideOrDefault(defaultAddrGivenName, payCreateBillingGivenName),
		FamilyName:      overrideOrDefault(defaultAddrFamilyName, payCreateBillingFamilyName),
		Email:           overrideOrDefault(defaultAddrEmail, payCreateBillingEmail),
		StreetAndNumber: overrideOrDefault(defaultAddrStreet, payCreateBillingStreet),
		PostalCode:      overrideOrDefault(defaultAddrPostalCode, payCreateBillingPostalCode),
		City:            overrideOrDefault(defaultAddrCity, payCreateBillingCity),
		Country:         overrideOrDefault(defaultAddrCountry, payCreateBillingCountry),
	}
	if payCreateBillingPhone != "" {
		addr.Phone = strPtr(payCreateBillingPhone)
	}
	if payCreateBillingOrg != "" {
		addr.OrganizationName = payCreateBillingOrg
	}
	if payCreateBillingStreetAdl != "" {
		addr.StreetAdditional = strPtr(payCreateBillingStreetAdl)
	}
	if payCreateBillingRegion != "" {
		addr.Region = strPtr(payCreateBillingRegion)
	}
	return addr
}

// buildShippingAddress builds a PaymentAddress with the same NL test-mode
// defaults. Any --shipping-* flag that is non-empty overrides the default.
func buildShippingAddress() *components.PaymentAddress {
	addr := &components.PaymentAddress{
		GivenName:       overrideOrDefault(defaultAddrGivenName, payCreateShippingGivenName),
		FamilyName:      overrideOrDefault(defaultAddrFamilyName, payCreateShippingFamilyName),
		Email:           overrideOrDefault(defaultAddrEmail, payCreateShippingEmail),
		StreetAndNumber: overrideOrDefault(defaultAddrStreet, payCreateShippingStreet),
		PostalCode:      overrideOrDefault(defaultAddrPostalCode, payCreateShippingPostalCode),
		City:            overrideOrDefault(defaultAddrCity, payCreateShippingCity),
		Country:         overrideOrDefault(defaultAddrCountry, payCreateShippingCountry),
	}
	if payCreateShippingPhone != "" {
		addr.Phone = strPtr(payCreateShippingPhone)
	}
	if payCreateShippingOrg != "" {
		addr.OrganizationName = strPtr(payCreateShippingOrg)
	}
	if payCreateShippingStreetAdl != "" {
		addr.StreetAdditional = strPtr(payCreateShippingStreetAdl)
	}
	if payCreateShippingRegion != "" {
		addr.Region = strPtr(payCreateShippingRegion)
	}
	return addr
}

// buildPaymentLines auto-generates []PaymentRequestLine values that always sum
// to the payment's total amount.
//
// The result always contains:
//   - 2 physical item lines (description / "Accessories"), split ~60/40
//   - 1 shipping_fee line (4.99 by default, or --lines-shipping-amount)
//   - 1 discount line (≈10% off items) when withDiscount is true
//
// VAT calculation: totalAmount is treated as VAT-inclusive, so
//
//	vatAmount = totalAmount × vatRate / (100 + vatRate)
func buildPaymentLines(
	currency, totalStr, description, vatRateStr, shippingStr string, withDiscount bool,
) ([]components.PaymentRequestLine, error) {
	totalFloat, err := strconv.ParseFloat(totalStr, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse --amount %q: %w", totalStr, err)
	}
	totalCents := int64(math.Round(totalFloat * 100))

	vatRate, err := strconv.ParseFloat(vatRateStr, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse --lines-vat-rate %q: %w", vatRateStr, err)
	}

	// Resolve shipping: use the user-supplied amount, or default to 4.99.
	var shippingCents int64
	if shippingStr != "" {
		shippingFloat, err := strconv.ParseFloat(shippingStr, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse --lines-shipping-amount %q: %w", shippingStr, err)
		}
		shippingCents = int64(math.Round(shippingFloat * 100))
		if shippingCents <= 0 {
			return nil, fmt.Errorf("--lines-shipping-amount must be > 0")
		}
		if shippingCents >= totalCents {
			return nil, fmt.Errorf("--lines-shipping-amount (%s) must be less than --amount (%s)", shippingStr, totalStr)
		}
	} else {
		// Default shipping: 4.99, capped to at most half the total.
		shippingCents = 499
		if shippingCents >= totalCents {
			shippingCents = totalCents / 2
		}
		if shippingCents <= 0 {
			return nil, fmt.Errorf("--amount is too small to generate lines with a shipping line")
		}
	}

	productNetCents := totalCents - shippingCents

	// When a discount is requested, inflate the gross item prices so that the
	// discount line brings the net back to productNetCents.
	// gross = ceil(net / 0.9) using integer arithmetic; discount = gross - net.
	var productGrossCents, discountCents int64
	if withDiscount {
		productGrossCents = (productNetCents*10 + 8) / 9
		discountCents = productGrossCents - productNetCents
	} else {
		productGrossCents = productNetCents
	}

	// Split product gross ~60/40 between two item lines.
	item1Cents := productGrossCents * 6 / 10
	item2Cents := productGrossCents - item1Cents

	// amountOf converts an integer cent value to a components.Amount.
	amountOf := func(cents int64) components.Amount {
		return components.Amount{Currency: currency, Value: fmt.Sprintf("%.2f", float64(cents)/100)}
	}

	// vatAmountOf computes the VAT embedded in a (possibly negative) cent value.
	vatAmountOf := func(cents int64) *components.Amount {
		if vatRate == 0 {
			return nil
		}
		v := int64(math.Round(float64(cents) * vatRate / (100 + vatRate)))
		a := amountOf(v)
		return &a
	}

	var vatRateStrPtr *string
	if vatRate != 0 {
		formatted := fmt.Sprintf("%.2f", vatRate)
		vatRateStrPtr = &formatted
	}

	physType := components.PaymentLineTypePhysical
	shippingType := components.PaymentLineTypeShippingFee
	quantity := int64(1)

	lines := []components.PaymentRequestLine{
		{
			Type:        &physType,
			Description: description,
			Quantity:    quantity,
			UnitPrice:   amountOf(item1Cents),
			TotalAmount: amountOf(item1Cents),
			VatRate:     vatRateStrPtr,
			VatAmount:   vatAmountOf(item1Cents),
		},
		{
			Type:        &physType,
			Description: "Accessories",
			Quantity:    quantity,
			UnitPrice:   amountOf(item2Cents),
			TotalAmount: amountOf(item2Cents),
			VatRate:     vatRateStrPtr,
			VatAmount:   vatAmountOf(item2Cents),
		},
		{
			Type:        &shippingType,
			Description: "Shipping",
			Quantity:    quantity,
			UnitPrice:   amountOf(shippingCents),
			TotalAmount: amountOf(shippingCents),
			VatRate:     vatRateStrPtr,
			VatAmount:   vatAmountOf(shippingCents),
		},
	}

	if withDiscount {
		discountType := components.PaymentLineTypeDiscount
		lines = append(lines, components.PaymentRequestLine{
			Type:        &discountType,
			Description: "Discount (10%)",
			Quantity:    quantity,
			UnitPrice:   amountOf(-discountCents),
			TotalAmount: amountOf(-discountCents),
			VatRate:     vatRateStrPtr,
			VatAmount:   vatAmountOf(-discountCents),
		})
	}

	return lines, nil
}

// parseMetadata converts a raw JSON string into a components.Metadata union.
// If the string is valid JSON object it becomes a MapOfAny; otherwise it is
// stored as a plain string.
func parseMetadata(raw string) (components.Metadata, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err == nil {
		return components.CreateMetadataMapOfAny(m), nil
	}
	// Validate it's at least valid JSON (could be a quoted string or array).
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return components.Metadata{}, fmt.Errorf("must be a valid JSON value: %w", err)
	}
	return components.CreateMetadataStr(raw), nil
}

// seedPassThroughFields copies non-flag-mapped fields from the raw stdin JSON
// map onto req.  Each field is extracted individually so that a type mismatch
// in one field (e.g. vatRate given as a JSON number rather than a quoted
// string) cannot silently prevent other fields — most notably billingAddress —
// from being forwarded.
//
// Fields that have a direct CLI flag (amount, currency, description,
// redirectUrl, webhookUrl, method, sequenceType, captureMode, locale,
// metadata, customerId) are applied separately by the caller and are not
// touched here.
func seedPassThroughFields(req *components.PaymentRequest, m map[string]json.RawMessage) {
	// ── Addresses ────────────────────────────────────────────────────────────
	if raw, ok := m["billingAddress"]; ok {
		var ba components.PaymentRequestBillingAddress
		if err := json.Unmarshal(raw, &ba); err == nil {
			req.BillingAddress = &ba
		}
	}
	if raw, ok := m["shippingAddress"]; ok {
		var sa components.PaymentAddress
		if err := json.Unmarshal(raw, &sa); err == nil {
			req.ShippingAddress = &sa
		}
	}

	// ── Order lines ──────────────────────────────────────────────────────────
	if raw, ok := m["lines"]; ok {
		// Callers sometimes send vatRate as a bare JSON number (e.g. 0) instead
		// of the quoted decimal string that PaymentRequestLine.VatRate (*string)
		// requires.  Normalise before deserialising.
		if normalized, err := normalizeLineVatRates(raw); err == nil {
			var lines []components.PaymentRequestLine
			if err := json.Unmarshal(normalized, &lines); err == nil {
				req.Lines = lines
			}
		}
	}

	// ── Simple string pass-throughs ──────────────────────────────────────────
	strField := func(raw json.RawMessage, dst **string) {
		var v string
		if err := json.Unmarshal(raw, &v); err == nil {
			*dst = &v
		}
	}
	if raw, ok := m["cancelUrl"]; ok {
		strField(raw, &req.CancelURL)
	}
	if raw, ok := m["issuer"]; ok {
		strField(raw, &req.Issuer)
	}
	if raw, ok := m["restrictPaymentMethodsToCountry"]; ok {
		strField(raw, &req.RestrictPaymentMethodsToCountry)
	}
	if raw, ok := m["captureDelay"]; ok {
		strField(raw, &req.CaptureDelay)
	}
	if raw, ok := m["mandateId"]; ok {
		strField(raw, &req.MandateID)
	}
	if raw, ok := m["profileId"]; ok {
		strField(raw, &req.ProfileID)
	}
	if raw, ok := m["dueDate"]; ok {
		strField(raw, &req.DueDate)
	}
	if raw, ok := m["applePayPaymentToken"]; ok {
		strField(raw, &req.ApplePayPaymentToken)
	}
	if raw, ok := m["cardToken"]; ok {
		strField(raw, &req.CardToken)
	}
	if raw, ok := m["voucherNumber"]; ok {
		strField(raw, &req.VoucherNumber)
	}
	if raw, ok := m["voucherPin"]; ok {
		strField(raw, &req.VoucherPin)
	}
	if raw, ok := m["sessionId"]; ok {
		strField(raw, &req.SessionID)
	}
	if raw, ok := m["customerReference"]; ok {
		strField(raw, &req.CustomerReference)
	}
	if raw, ok := m["terminalId"]; ok {
		strField(raw, &req.TerminalID)
	}

	// ── Bool pass-throughs ───────────────────────────────────────────────────
	boolField := func(raw json.RawMessage, dst **bool) {
		var v bool
		if err := json.Unmarshal(raw, &v); err == nil {
			*dst = &v
		}
	}
	if raw, ok := m["testmode"]; ok {
		boolField(raw, &req.Testmode)
	}
	if raw, ok := m["digitalGoods"]; ok {
		boolField(raw, &req.DigitalGoods)
	}

	// ── Structured pass-throughs ─────────────────────────────────────────────
	if raw, ok := m["company"]; ok {
		var v components.Company
		if err := json.Unmarshal(raw, &v); err == nil {
			req.Company = &v
		}
	}
	if raw, ok := m["applicationFee"]; ok {
		var v components.PaymentRequestApplicationFee
		if err := json.Unmarshal(raw, &v); err == nil {
			req.ApplicationFee = &v
		}
	}
	if raw, ok := m["routing"]; ok {
		var v []components.EntityPaymentRoute
		if err := json.Unmarshal(raw, &v); err == nil {
			req.Routing = v
		}
	}
	if raw, ok := m["extraMerchantData"]; ok {
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err == nil {
			req.ExtraMerchantData = v
		}
	}
}

// normalizeLineVatRates rewrites any vatRate that is a bare JSON number (e.g.
// 0 or 21) into a quoted decimal string (e.g. "0.00" or "21.00") so the value
// can be unmarshaled into PaymentRequestLine.VatRate (*string).  The raw
// json.RawMessage is returned unchanged when it cannot be parsed as an array
// of objects.
func normalizeLineVatRates(raw json.RawMessage) (json.RawMessage, error) {
	var lines []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &lines); err != nil {
		return raw, err
	}
	changed := false
	for _, line := range lines {
		vr, ok := line["vatRate"]
		if !ok {
			continue
		}
		var num float64
		if err := json.Unmarshal(vr, &num); err != nil {
			// Not a bare number — already a string or missing; leave it.
			continue
		}
		quoted, err := json.Marshal(fmt.Sprintf("%.2f", num))
		if err != nil {
			continue
		}
		line["vatRate"] = quoted
		changed = true
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(lines)
}
