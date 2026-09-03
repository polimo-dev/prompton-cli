package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polimo-dev/prompton-cli/cmd"
	"github.com/polimo-dev/prompton-cli/internal/config"
)

// request is one call the CLI made to the fake server.
type request struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Body   string
}

// harness runs the CLI end to end against a fake API and an isolated config
// file, which is as close to "what a user sees" as a test can get.
type harness struct {
	t        *testing.T
	mux      *http.ServeMux
	srv      *httptest.Server
	path     string
	requests []request
	stdin    io.Reader
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, mux: http.NewServeMux(), stdin: strings.NewReader("")}
	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"code":"not_found","message":"no route","details":{}}}`)
	})
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.requests = append(h.requests, request{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			Auth: r.Header.Get("Authorization"), Body: string(raw),
		})
		r.Body = io.NopCloser(bytes.NewReader(raw))
		h.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(h.srv.Close)

	// Device login polls on a timer; tests have no patience for it.
	t.Cleanup(cmd.SetDeviceSleep(func(ctx context.Context, _ time.Duration) error { return ctx.Err() }))

	h.path = filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PTN_CONFIG", h.path)
	for _, key := range []string{"PTN_HOST", "PTN_TOKEN", "PTN_ORG", "PTN_PROJECT", "PTN_OPENROUTER_KEY"} {
		t.Setenv(key, "")
	}
	return h
}

// handle registers a JSON reply for one route.
func (h *harness) handle(pattern string, status int, body string) {
	h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
}

// handleFunc registers a handler for routes that need to inspect the request.
func (h *harness) handleFunc(pattern string, fn http.HandlerFunc) {
	h.mux.HandleFunc(pattern, fn)
}

// login writes a config file that looks like a completed login.
func (h *harness) login(f config.File) {
	h.t.Helper()
	f.Host = h.srv.URL
	if f.Token == "" {
		f.Token = "session-token"
	}
	if err := config.WriteFile(h.path, f); err != nil {
		h.t.Fatalf("seed config: %v", err)
	}
}

type result struct {
	code   int
	stdout string
	stderr string
}

// run executes the CLI exactly as a shell would.
func (h *harness) run(args ...string) result {
	h.t.Helper()
	var out, errOut bytes.Buffer
	code := cmd.Run(context.Background(), args, h.stdin, &out, &errOut)
	return result{code: code, stdout: out.String(), stderr: errOut.String()}
}

func (r result) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n---\n%s", err, r.stdout)
	}
	return m
}

// config reads the config file back, to assert on what a command persisted.
func (h *harness) config() config.File {
	h.t.Helper()
	raw, err := os.ReadFile(h.path)
	if err != nil {
		h.t.Fatalf("read config: %v", err)
	}
	var f config.File
	if err := json.Unmarshal(raw, &f); err != nil {
		h.t.Fatalf("parse config: %v", err)
	}
	return f
}

// lastBody decodes the body of the most recent request.
func (h *harness) lastBody() map[string]any {
	h.t.Helper()
	if len(h.requests) == 0 {
		h.t.Fatal("no requests were made")
	}
	var m map[string]any
	body := h.requests[len(h.requests)-1].Body
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		h.t.Fatalf("last request body is not JSON: %v (%s)", err, body)
	}
	return m
}

func (h *harness) paths() []string {
	out := make([]string, 0, len(h.requests))
	for _, r := range h.requests {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}
