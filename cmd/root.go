// Package cmd wires the CLI's command tree.
//
// Every command shares one set of global flags (--host --token --org --project
// --json --quiet), and every value they carry resolves in the same order:
// flag, then environment, then ~/.config/prompton/config.json, then a default.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/config"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

// globals holds the flag values and the lazily-resolved configuration every
// command works from.
type globals struct {
	host    string
	token   string
	org     string
	project string

	asJSON     bool
	quiet      bool
	idempotent bool

	in  io.Reader
	out io.Writer
	err io.Writer

	once   sync.Once
	cfg    *config.Config
	cfgErr error
}

// config resolves flags over environment over file, once per process.
func (g *globals) config() (*config.Config, error) {
	g.once.Do(func() {
		g.cfg, g.cfgErr = config.Load(config.Overrides{
			Host:    g.host,
			Token:   g.token,
			Org:     g.org,
			Project: g.project,
		})
	})
	return g.cfg, g.cfgErr
}

// printer returns the renderer for this invocation.
func (g *globals) printer() *output.Printer {
	return output.New(g.out, g.err, g.asJSON, g.quiet)
}

// client returns an authenticated API client, or an actionable error when
// there is no credential yet.
func (g *globals) client() (*api.Client, *config.Config, error) {
	cfg, err := g.config()
	if err != nil {
		return nil, nil, err
	}
	if cfg.Token == "" {
		return nil, nil, failf("not logged in — run `%s login` (or set %s)", meta.Name, meta.Env("TOKEN"))
	}
	return api.New(cfg.Host, cfg.Token), cfg, nil
}

// orgRef resolves the :org path segment: a team slug, or "personal".
func (g *globals) orgRef() (string, error) {
	cfg, err := g.config()
	if err != nil {
		return "", err
	}
	if cfg.Org == "" {
		return "", usagef("no organization selected — pass --org <slug|personal> or run `%s use --org <slug>`", meta.Name)
	}
	return cfg.Org, nil
}

// projectRef resolves the project slug commands operate inside.
func (g *globals) projectRef() (string, error) {
	cfg, err := g.config()
	if err != nil {
		return "", err
	}
	if cfg.Project == "" {
		return "", usagef("no project selected — pass --project <slug> or run `%s use --project <slug>`", meta.Name)
	}
	return cfg.Project, nil
}

// scope resolves org and project together, which most commands need.
func (g *globals) scope() (org, project string, err error) {
	if org, err = g.orgRef(); err != nil {
		return "", "", err
	}
	project, err = g.projectRef()
	return org, project, err
}

// NewRootCommand builds the command tree. in/out/err are injected so tests can
// drive the CLI exactly as a shell does.
func NewRootCommand(in io.Reader, out, errOut io.Writer) *cobra.Command {
	g := &globals{in: in, out: out, err: errOut}

	root := &cobra.Command{
		Use:   meta.Name,
		Short: "Provision and operate PromptOn from the command line",
		Long: fmt.Sprintf(`%s configures PromptOn: projects, use cases, prompts, models,
deployments and keys.

It signs in as you — `+"`%s login`"+` opens a browser approval and stores a
long-lived, revocable session token. Every call then runs with your own
organization and project membership, so the CLI can do exactly what you can.

Add --json to any command for machine-readable output.`, meta.Name, meta.Name),
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
		Version:           meta.Version,
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)

	pf := root.PersistentFlags()
	pf.StringVar(&g.host, "host", "", "PromptOn host (env "+meta.Env("HOST")+")")
	pf.StringVar(&g.token, "token", "", "CLI session token (env "+meta.Env("TOKEN")+")")
	pf.StringVar(&g.org, "org", "", "organization slug, or \"personal\"")
	pf.StringVar(&g.project, "project", "", "project slug")
	pf.BoolVar(&g.asJSON, "json", false, "print machine-readable JSON on stdout")
	pf.BoolVar(&g.quiet, "quiet", false, "print only essential output")
	pf.BoolVar(&g.idempotent, "idempotent", false, "treat \"already exists\" (HTTP 409) as success")

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &exitError{code: ExitUsage, err: err}
	})

	root.AddCommand(
		newLoginCommand(g),
		newLogoutCommand(g),
		newWhoamiCommand(g),
		newOrgsCommand(g),
		newUseCommand(g),
		newProjectsCommand(g),
		newUseCasesCommand(g),
		newPromptsCommand(g),
		newModelsCommand(g),
		newDeployCommand(g),
		newDeploymentsCommand(g),
		newRollbackCommand(g),
		newAPIKeysCommand(g),
		newProviderKeyCommand(g),
	)
	return root
}

// Run executes the CLI and returns the process exit code.
func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	root := NewRootCommand(in, out, errOut)
	root.SetArgs(args)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(errOut, "Cancelled.")
		return ExitError
	}

	asJSON := hasJSONFlag(args)
	writeError(errOut, err, asJSON)
	return codeFor(err)
}

// hasJSONFlag scans the raw argv, because a parse failure can happen before
// the flag is bound and the error still has to come out in the right shape.
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "--json=true" {
			return true
		}
		if a == "--" {
			break
		}
	}
	return false
}

// Main is the entry point used by main.go.
func Main() int {
	return Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
