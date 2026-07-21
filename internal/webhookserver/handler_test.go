package webhookserver_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fjbender/mollie-cli/internal/webhookserver"
)

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestHandler_VerifiesCorrectSignature(t *testing.T) {
	const secret = "whsec_test"
	body := `{"id":"event_1","type":"payment-link.paid","entityId":"pl_1"}`

	var got webhookserver.Event
	handler := webhookserver.Handler(secret, func(e webhookserver.Event) { got = e })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Mollie-Signature", sign(secret, body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !got.Verified {
		t.Error("expected event to be marked verified")
	}
	if string(got.Body) != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

func TestHandler_RejectsWrongSignature(t *testing.T) {
	const secret = "whsec_test"
	body := `{"id":"event_1"}`

	var got webhookserver.Event
	called := false
	handler := webhookserver.Handler(secret, func(e webhookserver.Event) { got = e; called = true })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Mollie-Signature", "not-the-right-signature")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (we still ack Mollie even on a bad signature)", rec.Code)
	}
	if !called {
		t.Fatal("expected onEvent to still be called for a logged-but-unverified event")
	}
	if got.Verified {
		t.Error("expected event to be marked unverified")
	}
}

func TestHandler_MissingSignatureHeaderIsUnverified(t *testing.T) {
	const secret = "whsec_test"
	body := `{"id":"event_1"}`

	var got webhookserver.Event
	handler := webhookserver.Handler(secret, func(e webhookserver.Event) { got = e })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got.Verified {
		t.Error("expected event with no signature header to be unverified")
	}
}

func TestHandler_EmptySecretAlwaysUnverified(t *testing.T) {
	body := `{"id":"event_1"}`

	var got webhookserver.Event
	handler := webhookserver.Handler("", func(e webhookserver.Event) { got = e })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Mollie-Signature", sign("some-secret-we-dont-have", body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got.Verified {
		t.Error("expected event to be unverified when no secret is known (foreign-subscription case)")
	}
}
