package verbose_test

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fjbender/mollie-cli/internal/verbose"
)

// backend starts a test server that responds with the given body and status.
func backend(body string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

// scanLines splits s into lines, stripping \r.
func scanLines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("expected output to contain %q\ngot:\n%s", want, got)
	}
}

func mustNotContain(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Errorf("expected output NOT to contain %q\ngot:\n%s", want, got)
	}
}

func TestLevel0Passthrough(t *testing.T) {
	srv := backend(`{"id":"tr_1"}`, http.StatusOK)
	defer srv.Close()

	var buf bytes.Buffer
	tr := &verbose.LoggingTransport{Level: 0, Inner: http.DefaultTransport, Writer: &buf}
	resp, err := (&http.Client{Transport: tr}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	if buf.Len() != 0 {
		t.Errorf("level 0 should write nothing, got: %q", buf.String())
	}
}

func TestLevel1LogsJSONBodies(t *testing.T) {
	srv := backend(`{"id":"tr_1","resource":"payment"}`, http.StatusCreated)
	defer srv.Close()

	var buf bytes.Buffer
	tr := &verbose.LoggingTransport{Level: 1, Inner: http.DefaultTransport, Writer: &buf}

	reqBody := strings.NewReader(`{"amount":{"value":"10.00","currency":"EUR"}}`)
	req, _ := http.NewRequest("POST", srv.URL+"/v2/payments", reqBody)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	out := buf.String()
	mustContain(t, out, "→ POST")
	mustContain(t, out, "/v2/payments")
	mustContain(t, out, `"amount"`)
	mustContain(t, out, "← 201 Created")
	mustContain(t, out, `"id"`)
	mustContain(t, out, `"tr_1"`)
}

func TestLevel1ResponseBodyStillReadable(t *testing.T) {
	srv := backend(`{"id":"tr_1"}`, http.StatusOK)
	defer srv.Close()

	var buf bytes.Buffer
	tr := &verbose.LoggingTransport{Level: 1, Inner: http.DefaultTransport, Writer: &buf}

	resp, err := (&http.Client{Transport: tr}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"id":"tr_1"}` {
		t.Errorf("response body not preserved after level-1 logging: got %q", got)
	}
}

func TestLevel2FullWireFormat(t *testing.T) {
	srv := backend(`{"id":"tr_1"}`, http.StatusOK)
	defer srv.Close()

	var buf bytes.Buffer
	tr := &verbose.LoggingTransport{Level: 2, Inner: http.DefaultTransport, Writer: &buf}

	req, _ := http.NewRequest("GET", srv.URL+"/v2/payments/tr_1", nil)
	req.Header.Set("Authorization", "Bearer test_abc123")

	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	out := buf.String()
	// Every non-blank line must start with "> " or "< "
	for _, l := range scanLines(out) {
		if l == "" {
			continue
		}
		if !strings.HasPrefix(l, "> ") && !strings.HasPrefix(l, "< ") {
			t.Errorf("unexpected line format: %q", l)
		}
	}
	mustContain(t, out, "> GET")
	mustContain(t, out, "< HTTP/1.1 200 OK")
}

func TestLevel2RedactsTestKey(t *testing.T) {
	srv := backend(`{}`, http.StatusOK)
	defer srv.Close()

	var buf bytes.Buffer
	tr := &verbose.LoggingTransport{Level: 2, Inner: http.DefaultTransport, Writer: &buf}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Authorization", "Bearer test_supersecret123")

	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	out := buf.String()
	mustContain(t, out, "Authorization: Bearer test_[REDACTED]")
	mustNotContain(t, out, "supersecret123")
}

func TestLevel2RedactsLiveKey(t *testing.T) {
	srv := backend(`{}`, http.StatusOK)
	defer srv.Close()

	var buf bytes.Buffer
	tr := &verbose.LoggingTransport{Level: 2, Inner: http.DefaultTransport, Writer: &buf}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Authorization", "Bearer live_supersecret456")

	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	out := buf.String()
	mustContain(t, out, "Authorization: Bearer live_[REDACTED]")
	mustNotContain(t, out, "supersecret456")
}

func TestLevel2RedactsOAuthToken(t *testing.T) {
	srv := backend(`{}`, http.StatusOK)
	defer srv.Close()

	var buf bytes.Buffer
	tr := &verbose.LoggingTransport{Level: 2, Inner: http.DefaultTransport, Writer: &buf}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Authorization", "Bearer oauthtokensecret789")

	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	out := buf.String()
	mustContain(t, out, "Authorization: Bearer [REDACTED]")
	mustNotContain(t, out, "oauthtokensecret789")
}

func TestLevel2ResponseBodyStillReadable(t *testing.T) {
	srv := backend(`{"id":"tr_1"}`, http.StatusOK)
	defer srv.Close()

	var buf bytes.Buffer
	tr := &verbose.LoggingTransport{Level: 2, Inner: http.DefaultTransport, Writer: &buf}

	resp, err := (&http.Client{Transport: tr}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"id":"tr_1"}` {
		t.Errorf("response body not preserved after level-2 logging: got %q", got)
	}
}
