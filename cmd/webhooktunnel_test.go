package cmd

import "testing"

func TestResolveSubscriptionAction_FreeSlotCreatesFresh(t *testing.T) {
	cases := [][]whWebhook{
		nil,
		{{ID: "wh_1"}},
	}
	for _, existing := range cases {
		got := resolveSubscriptionAction(existing, "")
		if got.Kind != actionCreateFresh {
			t.Errorf("with %d existing subscriptions: Kind = %v, want actionCreateFresh", len(existing), got.Kind)
		}
	}
}

func TestResolveSubscriptionAction_FullSlotsRecreatesOwned(t *testing.T) {
	existing := []whWebhook{
		{ID: "wh_1", Name: "mollie-cli webhook-tunnel"},
		{ID: "wh_2", Name: "Someone else's backend"},
	}

	got := resolveSubscriptionAction(existing, "wh_1")

	if got.Kind != actionRecreateOwned {
		t.Fatalf("Kind = %v, want actionRecreateOwned", got.Kind)
	}
	if got.Existing == nil || got.Existing.ID != "wh_1" {
		t.Errorf("Existing = %+v, want the subscription with ID wh_1", got.Existing)
	}
}

func TestResolveSubscriptionAction_FullSlotsNoOwnedMatchPicksForeign(t *testing.T) {
	existing := []whWebhook{
		{ID: "wh_1", Name: "Someone's backend"},
		{ID: "wh_2", Name: "Someone else's backend"},
	}

	got := resolveSubscriptionAction(existing, "")
	if got.Kind != actionPickForeign {
		t.Errorf("Kind = %v, want actionPickForeign", got.Kind)
	}

	got = resolveSubscriptionAction(existing, "wh_does_not_exist")
	if got.Kind != actionPickForeign {
		t.Errorf("with an owned ID that matches neither subscription: Kind = %v, want actionPickForeign", got.Kind)
	}
}
