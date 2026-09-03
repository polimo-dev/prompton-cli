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
		pins            []string
	)

	cmd := &cobra.Command{
		Use:   "deploy <use-case>",
		Short: "Commit a deployment revision",
		Long: `Commit a new revision: one model, its params, and one pinned prompt version
per prompt name. A revision is a pin, not a router — the moment it is committed
it is the live configuration for that (use case, environment).

--model takes either a catalog UUID or a provider string like
"anthropic/claude-sonnet-4"; a provider string that is not in the catalog yet is
registered on the way past.

--pin takes name=version, where version is a version number, the word "latest",
or a version UUID. Omitting --pin entirely pins the newest committed version of
every prompt. If the use case has a "default" prompt it must end up pinned —
that is what the app gets when it names no prompt.

Promoting is the same command against another environment with the same pins.`,
		Example: "  " + meta.Name + " deploy diary_generation --environment production \\\n" +
			"      --model anthropic/claude-sonnet-4 --params '{\"temperature\":0.4}' \\\n" +
			"      --pin default=1 --pin ko=latest",
		Args: exactArgs(1, "<use-case>"),
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
			useCase := args[0]

			paramsMap, err := parseJSONObject("params", params)
			if err != nil {
				return err
			}
			optionsMap, err := parseJSONObject("provider-options", providerOptions)
			if err != nil {
				return err
			}

			// Version numbers and "latest" have to become version UUIDs, and
			// only the use case knows the mapping. Fetch it only when a pin
			// actually needs resolving.
			var prompts []api.Prompt
			if needsPromptLookup(pins) {
				uc, err := client.GetUseCase(cmd.Context(), org, project, useCase)
				if err != nil {
					return err
				}
				prompts = uc.Prompts
			}
			pinMap, err := parsePins(pins, prompts)
			if err != nil {
				return err
			}

			req := api.CreateDeploymentRequest{
				Environment:     environment,
				PromptPins:      pinMap,
				Params:          paramsMap,
				ProviderOptions: optionsMap,
			}
			if isUUID(model) {
				req.ModelID = model
			} else {
				req.Model = model
			}

			deployment, err := client.CreateDeployment(cmd.Context(), org, project, useCase, req)
			if err != nil {
				return err
			}

			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(deployment)
			}
			p.Fields([][2]string{
				{"Use case", useCase},
				{"Environment", deployment.Environment},
				{"Revision", fmt.Sprintf("%d", deployment.Revision)},
				{"Model", deployment.Model},
				{"Catalog id", deployment.ModelID},
				{"Params", output.Dash(output.Compact(deployment.Params))},
				{"Provider options", output.Dash(output.Compact(deployment.ProviderOptions))},
				{"Pins", output.Dash(output.CompactStrings(deployment.PromptPins))},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "environment slug (server default: production)")
	cmd.Flags().StringVar(&model, "model", "", "catalog UUID or provider model string")
	cmd.Flags().StringVar(&params, "params", "", "JSON object of model params, layered over the use case defaults")
	cmd.Flags().StringVar(&providerOptions, "provider-options", "", "JSON object of provider options, layered over the model's")
	cmd.Flags().StringArrayVar(&pins, "pin", nil, "name=version (repeatable); version is a number, \"latest\", or a UUID")
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
		Use:   "list <use-case>",
		Short: "Show what is live, or one environment's history",
		Long: `Without --environment this lists the live revision of every environment: what
is running right now. With --environment it lists every revision of that
environment, newest first.`,
		Args: exactArgs(1, "<use-case>"),
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
		Use:   "rollback <use-case>",
		Short: "Re-commit a past revision",
		Long: `Roll back by re-committing an earlier revision's pins. History is never
rewound, so this produces a new, higher revision number carrying the old
configuration.`,
		Example: "  " + meta.Name + " rollback diary_generation --environment production --revision 2",
		Args:    exactArgs(1, "<use-case>"),
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
				{"Use case", args[0]},
				{"Environment", deployment.Environment},
				{"Revision", fmt.Sprintf("%d (restored from %d)", deployment.Revision, revision)},
				{"Model", deployment.Model},
				{"Params", output.Dash(output.Compact(deployment.Params))},
				{"Pins", output.Dash(output.CompactStrings(deployment.PromptPins))},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "environment slug (server default: production)")
	cmd.Flags().IntVar(&revision, "revision", 0, "revision number to restore")
	return cmd
}

// needsPromptLookup reports whether any pin is expressed as something other
// than a version UUID, which is the only case that needs the use case fetched.
func needsPromptLookup(pins []string) bool {
	for _, pin := range pins {
		_, value, ok := strings.Cut(pin, "=")
		if !ok {
			return true
		}
		if !isUUID(strings.TrimSpace(value)) {
			return true
		}
	}
	return false
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
			output.Dash(output.Truncate(output.CompactStrings(d.PromptPins), 46)),
			output.Date(d.CreatedAt),
		})
	}
	return rows
}
