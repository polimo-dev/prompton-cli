// Package device runs the browser-approval login flow (RFC 8628 shaped).
//
// The CLI asks the server for a short user code, points the human at a URL,
// and polls until they approve. What comes back is a long-lived, revocable
// user token — a "CLI session" — not an org key: every later management call
// runs as that user, so the server's existing membership policies decide what
// the CLI may do.
package device

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

// slowDownStep is how much a slow_down answer adds to the poll interval,
// per RFC 8628 §3.5.
const slowDownStep = 5 * time.Second

// defaultInterval is used when the server omits (or zeroes) "interval".
const defaultInterval = 5 * time.Second

// Errors the caller may want to distinguish.
var (
	// ErrDenied means the human pressed Deny.
	ErrDenied = errors.New("login was denied in the browser")
	// ErrExpired means the code timed out before anyone approved it.
	ErrExpired = errors.New("the login request expired before it was approved")
)

// Options configures a login run. The function fields exist so tests can drive
// the flow without sleeping or launching a browser.
type Options struct {
	Client  *api.Client
	Printer *output.Printer

	// ClientName and DeviceName identify this CLI on the approval screen.
	ClientName string
	DeviceName string

	// NoBrowser suppresses the automatic browser launch.
	NoBrowser bool

	// Open launches a URL. Defaults to the platform opener.
	Open func(url string) error
	// Sleep waits between polls. Defaults to a context-aware sleep.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Login performs the whole flow and returns the issued session token.
func Login(ctx context.Context, o Options) (*api.DeviceToken, error) {
	if o.Client == nil {
		return nil, errors.New("device login: no API client")
	}
	if o.ClientName == "" {
		o.ClientName = meta.Client()
	}
	if o.DeviceName == "" {
		o.DeviceName = meta.DeviceName()
	}
	if o.Open == nil {
		o.Open = OpenBrowser
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	p := o.Printer

	code, err := o.Client.RequestDeviceCode(ctx, api.DeviceCodeRequest{
		Client: o.ClientName,
		Name:   o.DeviceName,
	})
	if err != nil {
		return nil, err
	}

	uri := code.VerificationURIComplete
	if uri == "" {
		uri = code.VerificationURI
	}
	if p != nil {
		p.Info("")
		p.Info("  Open this URL to approve the login:")
		p.Info("    %s", uri)
		p.Info("")
		p.Info("  Your code: %s", code.UserCode)
		p.Info("")
	}
	if !o.NoBrowser {
		if err := o.Open(uri); err != nil && p != nil {
			p.Info("  (could not open a browser automatically — open the URL above)")
		}
	}
	if p != nil {
		p.Info("Waiting for approval…")
	}

	interval := time.Duration(code.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval
	}
	expiresIn := time.Duration(code.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 15 * time.Minute
	}

	pollCtx, cancel := context.WithTimeout(ctx, expiresIn)
	defer cancel()

	for {
		if err := o.Sleep(pollCtx, interval); err != nil {
			return nil, expiryError(ctx, pollCtx, err)
		}

		token, err := o.Client.PollDeviceToken(pollCtx, code.DeviceCode)
		if err == nil {
			if token.Token == "" {
				return nil, errors.New("the server approved the login but returned no token")
			}
			return token, nil
		}

		switch {
		case api.Is(err, api.CodeAuthorizationPending):
			// Keep waiting at the current interval.
		case api.Is(err, api.CodeSlowDown):
			interval += slowDownStep
			if p != nil {
				p.Info("Polling more slowly (every %s)…", interval)
			}
		case api.Is(err, api.CodeExpiredToken):
			return nil, ErrExpired
		case api.Is(err, api.CodeAccessDenied):
			return nil, ErrDenied
		default:
			if pollCtx.Err() != nil {
				return nil, expiryError(ctx, pollCtx, err)
			}
			return nil, err
		}
	}
}

// expiryError distinguishes "the user's code ran out" from "the user hit
// Ctrl-C", which look alike through a cancelled context.
func expiryError(parent, poll context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if poll.Err() != nil {
		return ErrExpired
	}
	return err
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// OpenBrowser makes a best-effort attempt to open url in the user's browser.
// A failure is never fatal: the URL has already been printed.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	// The opener exits immediately; reaping it keeps no zombie behind.
	go func() { _ = cmd.Wait() }()
	return nil
}
