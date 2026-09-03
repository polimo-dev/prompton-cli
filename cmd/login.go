package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/config"
	"github.com/polimo-dev/prompton-cli/internal/device"
	"github.com/polimo-dev/prompton-cli/internal/meta"
)

// deviceSleep overrides the wait between device-login polls. It is nil in a
// real run, which leaves device.Login on its own context-aware timer; tests
// replace it so the flow runs without waiting.
var deviceSleep func(context.Context, time.Duration) error

func newLoginCommand(g *globals) *cobra.Command {
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in through the browser and store a CLI session",
		Long: `Sign in to PromptOn.

The CLI asks the server for a short code, opens the approval page in your
browser, and waits. Approving issues a long-lived, revocable session token for
your user — not an organization key — so everything the CLI does afterwards
runs as you, under your existing memberships.

The token is written to the config file with 0600 permissions. ` + "`logout`" + `
revokes it server-side.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := g.config()
			if err != nil {
				return err
			}
			p := g.printer()

			client := api.New(cfg.Host, "")
			p.Info("Signing in to %s", cfg.Host)

			token, err := device.Login(cmd.Context(), device.Options{
				Client:    client,
				Printer:   p,
				NoBrowser: noBrowser,
				Sleep:     deviceSleep,
			})
			if err != nil {
				switch {
				case errors.Is(err, device.ErrDenied):
					return failf("login was denied in the browser")
				case errors.Is(err, device.ErrExpired):
					return failf("the login request expired — run `%s login` again", meta.Name)
				}
				return err
			}

			file := cfg.File
			file.Host = cfg.Host
			file.Token = token.Token
			file.User = &config.User{ID: token.User.ID, Email: token.User.Email, Name: token.User.Name}
			file.Orgs = toConfigOrgs(token.Orgs)

			chosen, err := g.chooseOrg(token.Orgs, file.Org)
			if err != nil {
				return err
			}
			if chosen != "" && chosen != file.Org {
				// A different org means the remembered project no longer applies.
				file.Project = ""
			}
			file.Org = chosen

			if err := config.WriteFile(cfg.Path, file); err != nil {
				return err
			}

			if g.asJSON {
				return p.PrintJSON(map[string]any{
					"host":          cfg.Host,
					"user":          token.User,
					"organizations": token.Orgs,
					"org":           file.Org,
					"config":        cfg.Path,
				})
			}

			p.Info("")
			p.Info("Logged in as %s", displayUser(token.User))
			if file.Org != "" {
				p.Info("Organization: %s", file.Org)
			} else {
				p.Info("No organization selected — run `%s use --org <slug>`", meta.Name)
			}
			p.Info("Session stored in %s", cfg.Path)
			return nil
		},
	}

	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the URL instead of opening a browser")
	return cmd
}

// chooseOrg decides which organization becomes the default after login.
//
// One org needs no question. Several do, and the answer comes from --org, from
// what was already configured, or from an interactive pick; a non-interactive
// run leaves it unset rather than guessing.
func (g *globals) chooseOrg(orgs []api.Org, current string) (string, error) {
	if len(orgs) == 0 {
		return "", nil
	}
	cfgOrgs := toConfigOrgs(orgs)

	if g.org != "" {
		found, ok := config.FindOrg(cfgOrgs, g.org)
		if !ok {
			return "", usagef("you are not a member of an organization called %q", g.org)
		}
		return found.Ref(), nil
	}
	if only, ok := config.DefaultOrg(cfgOrgs); ok {
		return only.Ref(), nil
	}
	if current != "" {
		if found, ok := config.FindOrg(cfgOrgs, current); ok {
			return found.Ref(), nil
		}
	}
	if !g.interactive() {
		g.printer().Warn("You belong to %d organizations — run `%s use --org <slug>` to pick one.", len(orgs), meta.Name)
		return "", nil
	}

	p := g.printer()
	p.Info("")
	p.Info("You belong to several organizations:")
	for i, o := range orgs {
		kind := "team"
		if o.Personal {
			kind = "personal"
		}
		p.Info("  %d) %-24s %s (%s)", i+1, o.Ref(), o.Name, kind)
	}
	answer, err := g.promptLine(fmt.Sprintf("Use which one? [1-%d, blank to skip] ", len(orgs)))
	if err != nil {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", nil
	}
	if n, convErr := strconv.Atoi(answer); convErr == nil {
		if n < 1 || n > len(orgs) {
			return "", usagef("pick a number between 1 and %d", len(orgs))
		}
		return orgs[n-1].Ref(), nil
	}
	found, ok := config.FindOrg(cfgOrgs, answer)
	if !ok {
		return "", usagef("you are not a member of an organization called %q", answer)
	}
	return found.Ref(), nil
}

func displayUser(u api.User) string {
	if u.Email != "" {
		return u.Email
	}
	if u.Name != "" {
		return u.Name
	}
	return u.ID
}
