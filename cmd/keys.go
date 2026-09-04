package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

// providerKeyEnv is the environment variable `provider-key set` reads when
// --secret is not given.
var providerKeyEnv = meta.Env("OPENROUTER_KEY")

func newAPIKeysCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "api-keys",
		Aliases: []string{"api-key"},
		Short:   "Runtime API keys for a project",
		Args:    noArgs,
		RunE:    func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newAPIKeysIssueCommand(g), newAPIKeysListCommand(g))
	return cmd
}

func newAPIKeysIssueCommand(g *globals) *cobra.Command {
	var (
		name   string
		scopes []string
	)

	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Issue a runtime key for the app",
		Long: `Issue the key the application puts in its environment. It is scoped to this
project and to deployed use-case reads (read) and monitoring logs (logs) only.

The secret is printed once, here, and never again — the server keeps a hash.
Keys are not tied to an environment: one key reads production and staging, and
the app names the environment in each request.`,
		Example: "  " + meta.Name + " api-keys issue --name 'Helpdesk server' --scopes read,logs",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			key, err := client.IssueAPIKey(cmd.Context(), org, project, api.IssueAPIKeyRequest{
				Name:   name,
				Scopes: scopes,
			})
			if err != nil {
				return err
			}

			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(key)
			}
			if g.quiet {
				// Exactly the secret, for `KEY=$(prompton api-keys issue --quiet)`.
				p.Line(key.Key)
				return nil
			}
			p.Fields([][2]string{
				{"Name", key.Name},
				{"Scopes", output.Join(key.Scopes)},
				{"Key", key.Key},
				{"Id", key.ID},
			})
			p.Info("")
			p.Info("This is the only time the key is shown. Store it now.")
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "label for this key (default \"CLI key\")")
	cmd.Flags().StringSliceVar(&scopes, "scopes", nil, "comma-separated scopes: read, logs (default both)")
	return cmd
}

func newAPIKeysListCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the project's live runtime keys",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			keys, err := client.ListAPIKeys(cmd.Context(), org, project)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(map[string]any{"api_keys": keys})
			}
			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, []string{
					k.Name, k.KeyPrefix + "…", output.Join(k.Scopes),
					output.Dash(output.Date(output.Str(k.LastUsedAt))),
					output.Date(k.CreatedAt),
				})
			}
			p.Table([]string{"NAME", "PREFIX", "SCOPES", "LAST USED", "CREATED"}, rows)
			return nil
		},
	}
}

func newProviderKeyCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider-key",
		Short: "The organization's BYOK provider key",
		Long: `Read or set the organization's OpenRouter key.

PromptOn only uses it where the server itself calls an LLM — the arena, AI
drafts, evaluations. The application's own traffic never passes through
PromptOn, so onboarding finishes without one.`,
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newProviderKeySetCommand(g), newProviderKeyStatusCommand(g))
	return cmd
}

func newProviderKeySetCommand(g *globals) *cobra.Command {
	var (
		secret string
		label  string
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Store the organization's provider key",
		Long: `Store the organization's OpenRouter key.

The secret is taken from --secret, then from ` + providerKeyEnv + `, and
otherwise read from a prompt (hidden when the terminal allows it). It is
encrypted at rest and never returned by any endpoint — only a masked hint is.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, err := g.orgRef()
			if err != nil {
				return err
			}

			value := strings.TrimSpace(secret)
			if value == "" {
				value = strings.TrimSpace(os.Getenv(providerKeyEnv))
			}
			if value == "" {
				value, err = g.promptSecret("OpenRouter key: ")
				if err != nil {
					return err
				}
			}
			if value == "" {
				return usagef("no secret given — pass --secret, set %s, or type it when prompted", providerKeyEnv)
			}

			key, createErr := client.SetProviderKey(cmd.Context(), org, api.SetProviderKeyRequest{
				Secret: value,
				Label:  label,
			})
			var found api.ProviderKey
			wasExisting, err := existing(createErr, &found)
			if err != nil {
				return err
			}
			if wasExisting {
				key = &found
			}

			p := g.printer()
			if g.asJSON {
				if err := p.PrintJSON(key); err != nil {
					return err
				}
			} else {
				p.Fields(providerKeyFields(key))
			}
			if wasExisting {
				name := key.Label
				if name == "" {
					name = "default"
				}
				if !g.idempotent {
					p.Warn("Replacing a provider key is done from the web console: /%s/settings?tab=providers", org)
				}
				return g.alreadyExists("provider key", name)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&secret, "secret", "", "the provider key (prefer "+providerKeyEnv+" or the prompt)")
	cmd.Flags().StringVar(&label, "label", "", "label for this key (default \"default\")")
	return cmd
}

func newProviderKeyStatusCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether a provider key is connected",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, err := g.orgRef()
			if err != nil {
				return err
			}
			key, err := client.GetProviderKey(cmd.Context(), org)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(key)
			}
			p.Fields(providerKeyFields(key))
			return nil
		},
	}
}

func providerKeyFields(k *api.ProviderKey) [][2]string {
	if !k.Connected {
		return [][2]string{
			{"Connected", "no"},
			{"Provider", output.Dash(k.Provider)},
		}
	}
	return [][2]string{
		{"Connected", "yes"},
		{"Provider", k.Provider},
		{"Label", output.Dash(k.Label)},
		{"Hint", output.Dash(k.Hint)},
		{"Last used", output.Dash(output.Date(output.Str(k.LastUsedAt)))},
		{"Created", output.Dash(output.Date(k.CreatedAt))},
	}
}
