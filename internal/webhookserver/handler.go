// Package webhookserver implements the local HTTP endpoint that receives
// webhook deliveries forwarded by the cloudflared tunnel, verifying Mollie's
// X-Mollie-Signature header when a signing secret is known.
package webhookserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"
)

// Event is a single received webhook delivery.
type Event struct {
	ReceivedAt time.Time
	Body       []byte
	Verified   bool
}

// Handler returns an http.Handler that reads each request's raw body,
// verifies it against secret (skipped entirely when secret is ""), invokes
// onEvent exactly once per request regardless of verification outcome, and
// always responds 200 OK.
func Handler(secret string, onEvent func(Event)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}

		verified := secret != "" && verifySignature(secret, body, r.Header.Get("X-Mollie-Signature"))

		onEvent(Event{
			ReceivedAt: time.Now(),
			Body:       body,
			Verified:   verified,
		})

		w.WriteHeader(http.StatusOK)
	})
}

// verifySignature reports whether sig is a valid hex-encoded HMAC-SHA256
// digest of body using secret, compared in constant time.
func verifySignature(secret string, body []byte, sig string) bool {
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}
