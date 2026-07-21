package webhookevents_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"

	"github.com/fjbender/mollie-cli/internal/webhookevents"
)

type fakeLister struct {
	resp   *operations.ListPermissionsResponse
	err    error
	called bool
}

func (f *fakeLister) List(_ context.Context, _ *string, _ ...operations.Option) (*operations.ListPermissionsResponse, error) {
	f.called = true
	return f.resp, f.err
}

func TestResolve_APIKeyReturnsFullListWithoutCallingLister(t *testing.T) {
	lister := &fakeLister{err: errors.New("should never be called")}

	got, err := webhookevents.Resolve(context.Background(), true, lister)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lister.called {
		t.Error("expected Resolve to skip the Permissions API entirely for an API key")
	}
	if len(got) != len(webhookevents.GlobalEventPermissions) {
		t.Errorf("expected all %d global event types, got %d: %v", len(webhookevents.GlobalEventPermissions), len(got), got)
	}
}

func TestResolve_AccessTokenFiltersByGrantedPermissions(t *testing.T) {
	lister := &fakeLister{
		resp: &operations.ListPermissionsResponse{
			Object: &operations.ListPermissionsResponseBody{
				Embedded: operations.ListPermissionsEmbedded{
					Permissions: []components.ListEntityPermission{
						{ID: "payment-links.read", Granted: true},
						{ID: "balances.read", Granted: false},
					},
				},
			},
		},
	}

	got, err := webhookevents.Resolve(context.Background(), false, lister)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"payment-link.paid"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolve_AccessTokenNoGrantedPermissionsReturnsEmpty(t *testing.T) {
	lister := &fakeLister{
		resp: &operations.ListPermissionsResponse{
			Object: &operations.ListPermissionsResponseBody{},
		},
	}

	got, err := webhookevents.Resolve(context.Background(), false, lister)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no event types, got %v", got)
	}
}

func TestResolve_AccessTokenPropagatesListError(t *testing.T) {
	lister := &fakeLister{err: errors.New("boom")}

	_, err := webhookevents.Resolve(context.Background(), false, lister)
	if err == nil {
		t.Fatal("expected an error")
	}
}
