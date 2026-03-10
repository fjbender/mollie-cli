package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/charmbracelet/huh"
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
	Use:         "create",
	Short:       "Create a new payment",
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
	paymentsCreateCmd.Flags().BoolVar(&payCreateWithLines, "with-lines", false, "Auto-generate order lines summing to --amount")
	paymentsCreateCmd.Flags().StringVar(&payCreateLinesVatRate, "lines-vat-rate", "0.00", "VAT rate for generated lines, e.g. 21.00 (default 0.00)")
	paymentsCreateCmd.Flags().StringVar(&payCreateLinesShipping, "lines-shipping-amount", "", "Split a shipping line off the total (e.g. 4.99); must be smaller than --amount")

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

	req := &components.PaymentRequest{
		Description: payCreateDescription,
		Amount: components.Amount{
			Currency: payCreateCurrency,
			Value:    payCreateAmount,
		},
		RedirectURL: &payCreateRedirectURL,
	}

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
	if payCreateWithLines {
		lines, err := buildPaymentLines(payCreateCurrency, payCreateAmount, payCreateDescription, payCreateLinesVatRate, payCreateLinesShipping)
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

func runPaymentsUpdate(_ *cobra.Command, args []string) error {
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

// buildPaymentLines auto-generates []PaymentRequestLine values that sum to the
// payment's total amount, so the caller does not have to replicate that math.
//
// Parameters:
//   - currency:    ISO 4217 code, e.g. "EUR"
//   - totalStr:    payment amount string, e.g. "15.00"
//   - description: used as the description for the product line
//   - vatRateStr:  VAT rate, e.g. "21.00" ("0.00" → no VAT)
//   - shippingStr: when non-empty, a second "Shipping" line is split off the
//     total for this sub-amount; must be less than totalStr
//
// VAT calculation: totalAmount is treated as VAT-inclusive, so
//
//	vatAmount = totalAmount × vatRate / (100 + vatRate)
func buildPaymentLines(
	currency, totalStr, description, vatRateStr, shippingStr string,
) ([]components.PaymentRequestLine, error) {
	total, err := strconv.ParseFloat(totalStr, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse --amount %q: %w", totalStr, err)
	}
	vatRate, err := strconv.ParseFloat(vatRateStr, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse --lines-vat-rate %q: %w", vatRateStr, err)
	}
	shipping := 0.0
	if shippingStr != "" {
		shipping, err = strconv.ParseFloat(shippingStr, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse --lines-shipping-amount %q: %w", shippingStr, err)
		}
		if shipping <= 0 {
			return nil, fmt.Errorf("--lines-shipping-amount must be > 0")
		}
		if shipping >= total {
			return nil, fmt.Errorf("--lines-shipping-amount (%s) must be less than --amount (%s)", shippingStr, totalStr)
		}
	}

	// Helper: format a float as a 2-decimal amount object.
	amountOf := func(v float64) components.Amount {
		return components.Amount{Currency: currency, Value: fmt.Sprintf("%.2f", v)}
	}

	// Helper: compute VAT amount (VAT-inclusive: vatAmount = total × r/(100+r)).
	vatAmountOf := func(lineTotal float64) *components.Amount {
		if vatRate == 0 {
			return nil
		}
		v := lineTotal * vatRate / (100 + vatRate)
		a := amountOf(v)
		return &a
	}

	var vatRateStrPtr *string
	if vatRate != 0 {
		formatted := fmt.Sprintf("%.2f", vatRate)
		vatRateStrPtr = &formatted
	}

	physType := components.PaymentLineTypePhysical
	quantity := int64(1)

	productTotal := total - shipping
	productLine := components.PaymentRequestLine{
		Type:        &physType,
		Description: description,
		Quantity:    quantity,
		UnitPrice:   amountOf(productTotal),
		TotalAmount: amountOf(productTotal),
		VatRate:     vatRateStrPtr,
		VatAmount:   vatAmountOf(productTotal),
	}

	if shipping == 0 {
		return []components.PaymentRequestLine{productLine}, nil
	}

	shippingLine := components.PaymentRequestLine{
		Type:        &physType,
		Description: "Shipping",
		Quantity:    quantity,
		UnitPrice:   amountOf(shipping),
		TotalAmount: amountOf(shipping),
		VatRate:     vatRateStrPtr,
		VatAmount:   vatAmountOf(shipping),
	}

	return []components.PaymentRequestLine{productLine, shippingLine}, nil
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
