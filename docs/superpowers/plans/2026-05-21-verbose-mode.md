# Verbose Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `-v` / `-vv` persistent flags that log Mollie API HTTP traffic (JSON bodies or full wire format) to stderr.

**Architecture:** A `LoggingTransport` (`http.RoundTripper`) in a new `internal/verbose` package intercepts each SDK request/response and writes to stderr. It is injected into the Mollie SDK client via the existing `WithClient()` SDK option. The flag is a Cobra `CountVarP` persistent flag on the root command, threaded into `mollieclient.New()` and `mollieclient.NewOrganizationClient()` as a new `verboseLevel int` argument.

**Tech Stack:** Go stdlib (`net/http`, `net/http/httputil`, `encoding/json`, `regexp`, `bufio`), Cobra, Mollie Go SDK v0.10.3.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/verbose/transport.go` | Create | `LoggingTransport` type + `RoundTrip` implementation |
| `internal/verbose/transport_test.go` | Create | Tests for all three levels and Authorization redaction |
| `cmd/root.go` | Modify | Add `flagVerbose int`, register `CountVarP` |
| `internal/mollieclient/mollieclient.go` | Modify | Accept `verboseLevel int`, inject `LoggingTransport` |
| `cmd/*.go` (all call sites) | Modify | Pass `flagVerbose` to both `mollieclient.New` and `mollieclient.NewOrganizationClient` |

---

### Task 1: Write failing tests for `LoggingTransport`

**Files:**
- Create: `internal/verbose/transport_test.go`

- [ ] **Step 1: Create the test file**

```go
// internal/verbose/transport_test.go
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
		io.WriteString(w, body)
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
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

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
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

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
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

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
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

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
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

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
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"id":"tr_1"}` {
		t.Errorf("response body not preserved after level-2 logging: got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail (package doesn't exist yet)**

```bash
go test ./internal/verbose/...
```

Expected: `cannot find package` or `no Go files` — the package does not exist yet.

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/verbose/transport_test.go
git commit -m "test: add failing tests for LoggingTransport"
```

---

### Task 2: Implement `LoggingTransport`

**Files:**
- Create: `internal/verbose/transport.go`

- [ ] **Step 1: Create the implementation**

```go
// internal/verbose/transport.go
package verbose

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
)

// LoggingTransport is an http.RoundTripper that logs HTTP traffic to Writer.
// Level 0: no-op pass-through.
// Level 1: pretty-prints JSON request and response bodies.
// Level 2: full HTTP wire format (curl -vv style), with Authorization redacted.
// Writer defaults to os.Stderr when nil.
type LoggingTransport struct {
	Level  int
	Inner  http.RoundTripper
	Writer io.Writer
}

var _ http.RoundTripper = (*LoggingTransport)(nil)

// authRedactRe matches the Authorization Bearer header value, preserving any
// test_ or live_ prefix so the credential type remains visible.
var authRedactRe = regexp.MustCompile(`(?m)^(Authorization:\s+Bearer\s+(?:test_|live_)?)(.+)$`)

func (t *LoggingTransport) inner() http.RoundTripper {
	if t.Inner != nil {
		return t.Inner
	}
	return http.DefaultTransport
}

func (t *LoggingTransport) writer() io.Writer {
	if t.Writer != nil {
		return t.Writer
	}
	return os.Stderr
}

func (t *LoggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch {
	case t.Level == 0:
		return t.inner().RoundTrip(req)
	case t.Level == 1:
		return t.roundTripLevel1(req)
	default:
		return t.roundTripLevel2(req)
	}
}

func (t *LoggingTransport) roundTripLevel1(req *http.Request) (*http.Response, error) {
	w := t.writer()

	var reqBody []byte
	if req.Body != nil {
		var err error
		reqBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	fmt.Fprintf(w, "→ %s %s\n", req.Method, req.URL)
	if len(reqBody) > 0 {
		prettyJSON(w, reqBody)
	}

	resp, err := t.inner().RoundTrip(req)
	if err != nil {
		return nil, err
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	fmt.Fprintf(w, "← %s\n", resp.Status)
	if len(respBody) > 0 {
		prettyJSON(w, respBody)
	}

	return resp, nil
}

func (t *LoggingTransport) roundTripLevel2(req *http.Request) (*http.Response, error) {
	w := t.writer()

	// Snapshot body: DumpRequestOut may consume it.
	var bodyCopy []byte
	if req.Body != nil {
		var err error
		bodyCopy, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyCopy))
	}

	reqDump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return nil, err
	}
	// Restore body for the real transport.
	if bodyCopy != nil {
		req.Body = io.NopCloser(bytes.NewReader(bodyCopy))
	}

	writeLines(w, "> ", redactAuthorization(reqDump))
	fmt.Fprintln(w) // blank line between request and response

	resp, err := t.inner().RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// DumpResponse reads resp.Body and restores it to a new reader.
	respDump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		return resp, err
	}
	writeLines(w, "< ", respDump)

	return resp, nil
}

// writeLines prefixes each line in data with prefix and writes to w.
func writeLines(w io.Writer, prefix string, data []byte) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fmt.Fprintf(w, "%s%s\n", prefix, sc.Text())
	}
}

// prettyJSON pretty-prints JSON data to w. Falls back to raw bytes on parse failure.
func prettyJSON(w io.Writer, data []byte) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		w.Write(data)
	} else {
		w.Write(buf.Bytes())
	}
	fmt.Fprintln(w)
}

// redactAuthorization replaces the secret part of a Bearer token while keeping
// the test_ or live_ prefix visible.
func redactAuthorization(dump []byte) []byte {
	return authRedactRe.ReplaceAll(dump, []byte("${1}[REDACTED]"))
}
```

- [ ] **Step 2: Run tests to verify they pass**

```bash
go test ./internal/verbose/...
```

Expected: all 9 tests pass.

- [ ] **Step 3: Commit the implementation**

```bash
git add internal/verbose/transport.go
git commit -m "feat: add LoggingTransport for verbose HTTP logging"
```

---

### Task 3: Add `--verbose` / `-v` flag to the root command

**Files:**
- Modify: `cmd/root.go`

- [ ] **Step 1: Add the flag variable and registration**

In `cmd/root.go`, add `flagVerbose` alongside the other global flag variables (around line 14):

```go
var (
	flagLive    bool
	flagOutput  string
	flagProfile string
	flagAPIKey  string
	flagNoColor bool
	flagEnv     string
	flagYes     bool
	flagVerbose int // -v = 1 (JSON bodies), -vv = 2 (full wire format)
)
```

In `init()`, add the flag registration after the existing flags (around line 130):

```go
rootCmd.PersistentFlags().CountVarP(
	&flagVerbose, "verbose", "v",
	"Verbose output: -v logs JSON request/response bodies, -vv logs full HTTP wire format",
)
```

- [ ] **Step 2: Verify the binary builds**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/root.go
git commit -m "feat: add -v / -vv verbose flag to root command"
```

---

### Task 4: Thread `verboseLevel` into `mollieclient.New()` and `mollieclient.NewOrganizationClient()`

**Files:**
- Modify: `internal/mollieclient/mollieclient.go`

- [ ] **Step 1: Update `mollieclient.New()` to accept and wire `verboseLevel`**

Add the import for the new package at the top:

```go
import (
	"fmt"
	"net/http"

	mollieapi "github.com/mollie/mollie-api-golang"
	"github.com/mollie/mollie-api-golang/models/components"

	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/verbose"
)
```

Change the `New` signature and body. The full updated `New` function:

```go
func New(cfg *config.Config, apiKeyOverride string, liveMode bool, profileID string, verboseLevel int) (*mollieapi.Client, error) {
	apiKey := cfg.APIKey
	if apiKeyOverride != "" {
		apiKey = apiKeyOverride
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured — run `mollie auth setup` to get started")
	}

	var sec components.Security
	if config.IsAPIKey(apiKey) {
		sec = components.Security{APIKey: &apiKey}
	} else {
		sec = components.Security{OAuth: &apiKey}
	}

	opts := []mollieapi.SDKOption{
		mollieapi.WithSecurity(sec),
		mollieapi.WithCustomUserAgent("Mollie-CLI/1.0.0"),
	}

	if !config.IsAPIKey(apiKey) {
		opts = append(opts, mollieapi.WithTestmode(!liveMode))

		resolvedProfile := cfg.ProfileID
		if profileID != "" {
			resolvedProfile = profileID
		}
		if resolvedProfile != "" {
			opts = append(opts, mollieapi.WithProfileID(resolvedProfile))
		}
	}

	if verboseLevel > 0 {
		opts = append(opts, mollieapi.WithClient(&http.Client{
			Transport: &verbose.LoggingTransport{
				Level: verboseLevel,
				Inner: http.DefaultTransport,
			},
		}))
	}

	return mollieapi.New(opts...), nil
}
```

- [ ] **Step 2: Update `NewOrganizationClient()` to accept and wire `verboseLevel`**

```go
func NewOrganizationClient(cfg *config.Config, apiKeyOverride string, verboseLevel int) (*mollieapi.Client, error) {
	apiKey := cfg.APIKey
	if apiKeyOverride != "" {
		apiKey = apiKeyOverride
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured — run `mollie auth setup` to get started")
	}

	opts := []mollieapi.SDKOption{
		mollieapi.WithSecurity(components.Security{
			OAuth: &apiKey,
		}),
	}

	if verboseLevel > 0 {
		opts = append(opts, mollieapi.WithClient(&http.Client{
			Transport: &verbose.LoggingTransport{
				Level: verboseLevel,
				Inner: http.DefaultTransport,
			},
		}))
	}

	return mollieapi.New(opts...), nil
}
```

- [ ] **Step 3: Verify the build fails at call sites (expected)**

```bash
go build ./...
```

Expected: multiple `too many arguments` errors — every call site in `cmd/` is now broken. This is the signal to proceed to Task 5.

- [ ] **Step 4: Commit the signature update**

```bash
git add internal/mollieclient/mollieclient.go
git commit -m "feat: thread verboseLevel into mollieclient constructors"
```

---

### Task 5: Update all `cmd/` call sites to pass `flagVerbose`

**Files:**
- Modify: all `cmd/*.go` files that call `mollieclient.New` or `mollieclient.NewOrganizationClient`

- [ ] **Step 1: Update the common `mollieclient.New` call sites**

The pattern `mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)` appears in most command files. Use find+sed to update all at once:

```bash
find cmd/ -name "*.go" -exec sed -i '' \
  's/mollieclient\.New(cfg, flagAPIKey, flagLive, flagProfile)/mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)/g' \
  {} +
```

- [ ] **Step 2: Update the common `mollieclient.NewOrganizationClient` call sites**

```bash
find cmd/ -name "*.go" -exec sed -i '' \
  's/mollieclient\.NewOrganizationClient(cfg, flagAPIKey)/mollieclient.NewOrganizationClient(cfg, flagAPIKey, flagVerbose)/g' \
  {} +
```

- [ ] **Step 3: Update the auth.go call sites (different argument patterns)**

`cmd/auth.go` has two non-standard call patterns. Update them manually:

Line with `mollieclient.New(tmpCfg, "", liveMode, "")`:
```go
client, err := mollieclient.New(tmpCfg, "", liveMode, "", flagVerbose)
```

Line with `mollieclient.NewOrganizationClient(tmpCfg, "")`:
```go
client, err := mollieclient.NewOrganizationClient(tmpCfg, "", flagVerbose)
```

- [ ] **Step 4: Verify the build passes**

```bash
go build ./...
```

Expected: clean build with no errors.

- [ ] **Step 5: Run all tests**

```bash
go test ./...
```

Expected: all tests pass (at minimum the transport tests from Task 1-2).

- [ ] **Step 6: Commit**

```bash
git add cmd/
git commit -m "feat: pass flagVerbose to all mollieclient call sites"
```

---

### Task 6: Manual smoke test

- [ ] **Step 1: Build the binary**

```bash
go build -o mollie-cli .
```

- [ ] **Step 2: Test level 1 (-v)**

Run any API command with `-v` and confirm JSON bodies appear on stderr while the normal output is unaffected:

```bash
./mollie-cli payments list -v 2>/tmp/verbose.log
cat /tmp/verbose.log
```

Expected in `/tmp/verbose.log`:
```
→ GET https://api.mollie.com/v2/payments?...
← 200 OK
{
  "_embedded": { ... }
}
```

- [ ] **Step 3: Test level 2 (-vv)**

```bash
./mollie-cli payments list -vv 2>/tmp/verbose2.log
cat /tmp/verbose2.log
```

Expected: lines prefixed with `> ` (request) and `< ` (response), `Authorization` header shows `Bearer test_[REDACTED]` or `Bearer live_[REDACTED]`.

- [ ] **Step 4: Verify piping still works**

```bash
./mollie-cli payments list -v -o json 2>/dev/null | jq '.count'
```

Expected: a number — normal stdout output is unaffected by verbose logging to stderr.

- [ ] **Step 5: Commit binary removal (if accidentally staged)**

```bash
git rm --cached mollie-cli 2>/dev/null || true
```

No commit needed — the binary is already in `.gitignore`.
