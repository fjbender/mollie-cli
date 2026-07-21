// Package webhookevents resolves which Mollie Next-gen Webhook event types a
// credential can subscribe to, without using the "*" wildcard — which
// silently misbehaves for tokens lacking some of the underlying permissions.
package webhookevents

import (
	"context"
	"fmt"
	"sort"

	"github.com/mollie/mollie-api-golang/models/operations"
)

// GlobalEventPermissions maps every documented "global" Next-gen Webhook
// event type to the permission required to receive it. Beta event types
// (dispute.*, file.*, unmatched-credit-transfer.*, connect-balance-transfer.*)
// are deliberately excluded — see the design doc.
var GlobalEventPermissions = map[string]string{
	"payment-link.paid":                        "payment-links.read",
	"balance-transaction.created":              "balances.read",
	"sales-invoice.created":                    "invoices.read",
	"sales-invoice.issued":                     "invoices.read",
	"sales-invoice.canceled":                   "invoices.read",
	"sales-invoice.paid":                       "invoices.read",
	"business-account-transfer.requested":      "business-account-transfers.read",
	"business-account-transfer.initiated":      "business-account-transfers.read",
	"business-account-transfer.pending-review": "business-account-transfers.read",
	"business-account-transfer.processed":      "business-account-transfers.read",
	"business-account-transfer.failed":         "business-account-transfers.read",
	"business-account-transfer.blocked":        "business-account-transfers.read",
	"business-account-transfer.returned":       "business-account-transfers.read",
	"payout.initiated":                         "payouts.read",
	"payout.completed":                         "payouts.read",
	"payout.processing-at-bank":                "payouts.read",
	"payout.canceled":                          "payouts.read",
	"payout.failed":                            "payouts.read",
}

// PermissionLister is the subset of the Mollie SDK's Permissions client that
// Resolve needs. The SDK's real client (`(*mollieapi.Client).Permissions`)
// satisfies this interface, so production callers pass it directly; tests
// pass a fake.
type PermissionLister interface {
	List(ctx context.Context, idempotencyKey *string, opts ...operations.Option) (*operations.ListPermissionsResponse, error)
}

// Resolve returns every event type the given credential can subscribe to.
//
// API keys (test_/live_) are fully privileged and the Permissions API
// rejects them outright, so lister is never used when isAPIKey is true —
// nil is a valid argument in that case.
func Resolve(ctx context.Context, isAPIKey bool, lister PermissionLister) ([]string, error) {
	if isAPIKey {
		return allEventTypes(), nil
	}

	resp, err := lister.List(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing permissions: %w", err)
	}

	granted := map[string]bool{}
	if resp.Object != nil {
		for _, p := range resp.Object.Embedded.Permissions {
			if p.Granted {
				granted[p.ID] = true
			}
		}
	}

	var types []string
	for eventType, permission := range GlobalEventPermissions {
		if granted[permission] {
			types = append(types, eventType)
		}
	}
	sort.Strings(types)
	return types, nil
}

func allEventTypes() []string {
	types := make([]string, 0, len(GlobalEventPermissions))
	for t := range GlobalEventPermissions {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
