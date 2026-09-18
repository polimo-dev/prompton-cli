package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

func newDeployCommand(g *globals) *cobra.Command {
	var (
		environment     string
		model           string
		params          string
		providerOptions string
		version         string
	)

	cmd := &cobra.Command{
		Use:   "deploy <prompt>",
		Short: "Commit a deployment revision",
		Long: `Commit a new revision: one model, its params, and one pinned prompt version.
A revision is a pin, not a router — the moment it is committed it is the live
configuration for that (prompt, environment).

--model takes either a catalog UUID or a provider string like
"openai/gpt-4o-mini"; a provider string that is not in the catalog yet is
registered on the way past.

--version takes a version number, the word "latest", or a version UUID.
Omitting --version pins the latest committed prompt version.

Promoting is the same command against another environment with the same version.`,
		Example: "  " + meta.Name + " deploy support_reply --environment production \\\n" +
			"      --model openai/gpt-4o-mini --params '{\"temperature\":0.3}' \\\n" +
			"      --version 1",
		Args: exactArgs(1, "<prompt>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(model) == "" {
				return usagef("--model is required: a catalog UUID or a provider model string")
			}
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			prompt := args[0]

			paramsMap, err := parseJSONObject("params", params)
			if err != nil {
				return err
			}
			optionsMap, err := parseJSONObject("provider-options", providerOptions)
			if err != nil {
				return err
			}

			promptVersionID := ""
			if version != "" && !strings.EqualFold(version, "latest") {
				if isUUID(version) {
					promptVersionID = version
				} else {
					promptRecord, err := client.GetPrompt(cmd.Context(), org, project, prompt)
					if err != nil {
						return err
					}
					promptVersionID, err = resolveSingleVersion(version, promptRecord)
					if err != nil {
						return err
					}
				}
			}

			req := api.CreateDeploymentRequest{
				Environment:     environment,
				PromptVersionID: promptVersionID,
				Params:          paramsMap,
				ProviderOptions: optionsMap,
			}
			if isUUID(model) {
				req.ModelID = model
			} else {
				req.Model = model
			}

			deployment, err := client.CreateDeployment(cmd.Context(), org, project, prompt, req)
			if err != nil {
				return err
			}

			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(deployment)
			}
			p.Fields([][2]string{
				{"Prompt", prompt},
				{"Environment", deployment.Environment},
				{"Revision", fmt.Sprintf("%d", deployment.Revision)},
				{"Model", deployment.Model},
				{"Catalog id", deployment.ModelID},
				{"Params", output.Dash(output.Compact(deployment.Params))},
				{"Provider options", output.Dash(output.Compact(deployment.ProviderOptions))},
				{"Pins", output.Dash(output.CompactStrings(deployment.TemplatePins))},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "environment slug (server default: production)")
	cmd.Flags().StringVar(&model, "model", "", "catalog UUID or provider model string")
	cmd.Flags().StringVar(&params, "params", "", "JSON object of model params, layered over the prompt defaults")
	cmd.Flags().StringVar(&providerOptions, "provider-options", "", "JSON object of provider options, layered over the model's")
	cmd.Flags().StringVar(&version, "version", "", "prompt version to deploy: number, \"latest\", or UUID (server default: latest)")
	return cmd
}

func newDeploymentsCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deployments",
		Aliases: []string{"deployment"},
		Short:   "Deployment revisions",
		Args:    noArgs,
		RunE:    func(c *cobra.Command, _ []string) error { return c.Help() },
	}

	var environment string
	list := &cobra.Command{
		Use:   "list <prompt>",
		Short: "Show what is live, or one environment's history",
		Long: `Without --environment this lists the live revision of every environment: what
is running right now. With --environment it lists every revision of that
environment, newest first.`,
		Args: exactArgs(1, "<prompt>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			deployments, err := client.ListDeployments(cmd.Context(), org, project, args[0], environment)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(map[string]any{"deployments": deployments})
			}
			p.Table(deploymentHeaders(), deploymentRows(deployments))
			return nil
		},
	}
	list.Flags().StringVar(&environment, "environment", "", "show this environment's full history")

	cmd.AddCommand(list)
	return cmd
}

func newRollbackCommand(g *globals) *cobra.Command {
	var (
		environment string
		revision    int
	)

	cmd := &cobra.Command{
		Use:   "rollback <prompt>",
		Short: "Re-commit a past revision",
		Long: `Roll back by re-committing an earlier revision's pins. History is never
rewound, so this produces a new, higher revision number carrying the old
configuration.`,
		Example: "  " + meta.Name + " rollback support_reply --environment production --revision 2",
		Args:    exactArgs(1, "<prompt>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("revision") {
				return usagef("--revision is required: the past revision number to restore")
			}
			if revision < 1 {
				return usagef("--revision must be 1 or greater")
			}
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}

			deployment, err := client.Rollback(cmd.Context(), org, project, args[0], api.RollbackRequest{
				Environment: environment,
				Revision:    revision,
			})
			if err != nil {
				return err
			}

			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(deployment)
			}
			p.Fields([][2]string{
				{"Prompt", args[0]},
				{"Environment", deployment.Environment},
				{"Revision", fmt.Sprintf("%d (restored from %d)", deployment.Revision, revision)},
				{"Model", deployment.Model},
				{"Params", output.Dash(output.Compact(deployment.Params))},
				{"Pins", output.Dash(output.CompactStrings(deployment.TemplatePins))},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "environment slug (server default: production)")
	cmd.Flags().IntVar(&revision, "revision", 0, "revision number to restore")
	return cmd
}

func deploymentHeaders() []string {
	return []string{"ENVIRONMENT", "REV", "MODEL", "PARAMS", "PINS", "CREATED"}
}

func deploymentRows(deployments []api.Deployment) [][]string {
	rows := make([][]string, 0, len(deployments))
	for _, d := range deployments {
		rows = append(rows, []string{
			d.Environment,
			fmt.Sprintf("%d", d.Revision),
			d.Model,
			output.Dash(output.Compact(d.Params)),
			output.Dash(output.Truncate(output.CompactStrings(d.TemplatePins), 46)),
			output.Date(d.CreatedAt),
		})
	}
	return rows
}
