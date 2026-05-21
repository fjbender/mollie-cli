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
		req.Body.Close()
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
