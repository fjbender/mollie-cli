// Package webhookserver implements the local HTTP endpoint that receives
// webhook deliveries forwarded by the cloudflared tunnel, verifying Mollie's
// X-Mollie-Signature header when a signing secret is known.
package webhookserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// maxBodyBytes caps how much of a request body this handler will read.
// Mollie's webhook payloads are small JSON documents, but the local server
// sits behind a public (if unguessable) tunnel URL — this bounds memory use
// against an oversized POST from anyone who stumbles onto that URL.
const maxBodyBytes = 10 << 20 // 10 MiB

// Handler returns an http.Handler that reads each request's raw body,
// verifies it against secret (skipped entirely when secret is ""), invokes
// onEvent exactly once per request regardless of verification outcome, and
// always responds 200 OK. A body over maxBodyBytes gets 413 instead; any
// other read failure gets 400 — both skip the onEvent call entirely, since
// there's no complete body to report.
func Handler(secret string, onEvent func(Event)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
			}
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
