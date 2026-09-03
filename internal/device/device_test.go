package device_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/device"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

// response is one canned reply for the polling endpoint.
type response struct {
	status int
	body   string
}

const (
	pendingBody  = `{"error":{"code":"authorization_pending","message":"waiting","details":{}}}`
	slowDownBody = `{"error":{"code":"slow_down","message":"too fast","details":{}}}`
	expiredBody  = `{"error":{"code":"expired_token","message":"the code expired","details":{}}}`
	deniedBody   = `{"error":{"code":"access_denied","message":"denied in the browser","details":{}}}`
	successBody  = `{"token":"cli-session-token",
	                 "user":{"id":"u1","email":"ada@example.com"},
	                 "organizations":[{"id":"o1","name":"Ada","slug":null,"personal":true}]}`
)

// server answers one device/code request and then walks the given script for
// each device/token poll, repeating the last entry if polled again.
func server(t *testing.T, codeBody string, script []response) (*httptest.Server, *int) {
	t.Helper()
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/device/code":
			io.WriteString(w, codeBody)
		case "/api/v1/device/token":
			i := polls
			polls++
			if i >= len(script) {
				i = len(script) - 1
			}
			w.WriteHeader(script[i].status)
			io.WriteString(w, script[i].body)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &polls
}

const codeBody = `{"device_code":"opaque-device-code","user_code":"ABCD-EFGH",
  "verification_uri":"https://prompton.example/device",
  "verification_uri_complete":"https://prompton.example/device?code=ABCD-EFGH",
  "expires_in":900,"interval":5}`

// recorder captures printed output and the intervals the flow slept for.
type recorder struct {
	out       bytes.Buffer
	errOut    bytes.Buffer
	intervals []time.Duration
	opened    []string
}

func (r *recorder) options(srv *httptest.Server) device.Options {
	return device.Options{
		Client:  api.New(srv.URL, ""),
		Printer: output.New(&r.out, &r.errOut, false, false),
		Open: func(url string) error {
			r.opened = append(r.opened, url)
			return nil
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			r.intervals = append(r.intervals, d)
			return ctx.Err()
		},
	}
}

func TestLoginPendingThenSlowDownThenSuccess(t *testing.T) {
	srv, polls := server(t, codeBody, []response{
		{400, pendingBody},
		{400, slowDownBody},
		{200, successBody},
	})
	rec := &recorder{}

	token, err := device.Login(context.Background(), rec.options(srv))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token.Token != "cli-session-token" {
		t.Errorf("token = %q", token.Token)
	}
	if token.User.Email != "ada@example.com" {
		t.Errorf("user = %+v", token.User)
	}
	if len(token.Orgs) != 1 || token.Orgs[0].Ref() != "personal" {
		t.Errorf("organizations = %+v", token.Orgs)
	}
	if *polls != 3 {
		t.Errorf("polled %d times, want 3", *polls)
	}

	// The first two waits use the server's interval; slow_down adds 5s to the
	// third, as RFC 8628 asks.
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second}
	if len(rec.intervals) != len(want) {
		t.Fatalf("slept %v, want %v", rec.intervals, want)
	}
	for i := range want {
		if rec.intervals[i] != want[i] {
			t.Errorf("wait %d = %s, want %s (slow_down must back off)", i+1, rec.intervals[i], want[i])
		}
	}
}

func TestLoginShowsTheCompleteURIAndTheUserCode(t *testing.T) {
	srv, _ := server(t, codeBody, []response{{200, successBody}})
	rec := &recorder{}

	if _, err := device.Login(context.Background(), rec.options(srv)); err != nil {
		t.Fatalf("Login: %v", err)
	}
	printed := rec.out.String()
	if !strings.Contains(printed, "https://prompton.example/device?code=ABCD-EFGH") {
		t.Errorf("the complete verification URI must be printed, got:\n%s", printed)
	}
	if !strings.Contains(printed, "ABCD-EFGH") {
		t.Errorf("the user code must be printed, got:\n%s", printed)
	}
	if len(rec.opened) != 1 || rec.opened[0] != "https://prompton.example/device?code=ABCD-EFGH" {
		t.Errorf("browser opened with %v", rec.opened)
	}
}

func TestLoginNoBrowserSkipsTheLaunch(t *testing.T) {
	srv, _ := server(t, codeBody, []response{{200, successBody}})
	rec := &recorder{}
	opts := rec.options(srv)
	opts.NoBrowser = true

	if _, err := device.Login(context.Background(), opts); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(rec.opened) != 0 {
		t.Errorf("--no-browser must not launch anything, got %v", rec.opened)
	}
	if !strings.Contains(rec.out.String(), "ABCD-EFGH") {
		t.Error("the code must still be printed when the browser is not opened")
	}
}

func TestLoginFallsBackToTheBareVerificationURI(t *testing.T) {
	bare := `{"device_code":"dc","user_code":"ABCD-EFGH",
	  "verification_uri":"https://prompton.example/device","expires_in":900,"interval":5}`
	srv, _ := server(t, bare, []response{{200, successBody}})
	rec := &recorder{}

	if _, err := device.Login(context.Background(), rec.options(srv)); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(rec.opened) != 1 || rec.opened[0] != "https://prompton.example/device" {
		t.Errorf("without a complete URI the plain one must be used, got %v", rec.opened)
	}
}

func TestLoginDenied(t *testing.T) {
	srv, _ := server(t, codeBody, []response{{400, pendingBody}, {400, deniedBody}})
	rec := &recorder{}

	_, err := device.Login(context.Background(), rec.options(srv))
	if !errors.Is(err, device.ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
}

func TestLoginExpiredToken(t *testing.T) {
	srv, _ := server(t, codeBody, []response{{400, expiredBody}})
	rec := &recorder{}

	_, err := device.Login(context.Background(), rec.options(srv))
	if !errors.Is(err, device.ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

func TestLoginExpiresWhenTheCodeTimesOut(t *testing.T) {
	// expires_in of 1 second with a 5 second poll interval: the deadline is
	// reached while waiting, which must read as expiry, not as a crash.
	short := `{"device_code":"dc","user_code":"ABCD-EFGH",
	  "verification_uri":"https://prompton.example/device",
	  "verification_uri_complete":"https://prompton.example/device?code=ABCD-EFGH",
	  "expires_in":1,"interval":5}`
	srv, polls := server(t, short, []response{{400, pendingBody}})

	rec := &recorder{}
	opts := rec.options(srv)
	opts.Sleep = nil // use the real, context-aware wait

	start := time.Now()
	_, err := device.Login(context.Background(), opts)
	if !errors.Is(err, device.ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("waited %s, expected to give up at the 1s deadline", elapsed)
	}
	if *polls != 0 {
		t.Errorf("polled %d times, want 0 — the deadline hit during the first wait", *polls)
	}
}

func TestLoginCancelledByTheUser(t *testing.T) {
	srv, _ := server(t, codeBody, []response{{400, pendingBody}})
	rec := &recorder{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := device.Login(ctx, rec.options(srv))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled — Ctrl-C is not an expiry", err)
	}
}

func TestLoginRejectsAnEmptyToken(t *testing.T) {
	srv, _ := server(t, codeBody, []response{{200, `{"token":"","user":{"id":"u1"},"organizations":[]}`}})
	rec := &recorder{}

	_, err := device.Login(context.Background(), rec.options(srv))
	if err == nil {
		t.Fatal("an approval that carries no token must be reported")
	}
	if errors.Is(err, device.ErrDenied) || errors.Is(err, device.ErrExpired) {
		t.Errorf("err = %v, want a distinct failure", err)
	}
}

func TestLoginPropagatesAnUnexpectedServerError(t *testing.T) {
	srv, _ := server(t, codeBody, []response{
		{500, `{"error":{"code":"internal_error","message":"boom","details":{}}}`},
	})
	rec := &recorder{}

	_, err := device.Login(context.Background(), rec.options(srv))
	if !api.Is(err, api.CodeInternalError) {
		t.Fatalf("err = %v, want the server error to surface", err)
	}
}

func TestLoginDefaultsIntervalWhenTheServerOmitsIt(t *testing.T) {
	noInterval := `{"device_code":"dc","user_code":"ABCD-EFGH",
	  "verification_uri":"https://prompton.example/device","expires_in":900}`
	srv, _ := server(t, noInterval, []response{{200, successBody}})
	rec := &recorder{}

	if _, err := device.Login(context.Background(), rec.options(srv)); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(rec.intervals) != 1 || rec.intervals[0] != 5*time.Second {
		t.Errorf("intervals = %v, want a 5s default", rec.intervals)
	}
}

func TestLoginFailsFastWhenTheCodeRequestFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		io.WriteString(w, `{"error":{"code":"internal_error","message":"down","details":{}}}`)
	}))
	defer srv.Close()

	rec := &recorder{}
	opts := rec.options(srv)
	_, err := device.Login(context.Background(), opts)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(rec.opened) != 0 {
		t.Error("no browser should be opened when there is no code to approve")
	}
}

func TestBrowserOpenFailureIsNotFatal(t *testing.T) {
	srv, _ := server(t, codeBody, []response{{200, successBody}})
	rec := &recorder{}
	opts := rec.options(srv)
	opts.Open = func(string) error { return errors.New("no browser here") }

	if _, err := device.Login(context.Background(), opts); err != nil {
		t.Fatalf("a browser that will not open must not fail the login: %v", err)
	}
}
