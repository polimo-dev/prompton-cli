package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

var revisionLabelPattern = regexp.MustCompile(`^v\d{4}\.\d{2}\.\d{2}-[1-9]\d*$`)

func newDeployCommand(g *globals) *cobra.Command {
	var (
		environment     string
		model           string
		provider        string
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

--provider and --model identify the provider model to pin. The model is the
provider-native name, for example "google/gemini-3.8-flash".

--version takes a positive prompt version number or the word "latest".
Omitting --version pins the latest committed prompt version.

Use prompton promote to copy a readable source revision into another environment.`,
		Example: "  " + meta.Name + " deploy support_reply --environment production \\\n" +
			"      --provider openrouter --model google/gemini-3.8-flash --params '{\"temperature\":0.3}' \\\n" +
			"      --version 1",
		Args: exactArgs(1, "<prompt>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(model) == "" {
				return usagef("--model is required: a provider-native model name")
			}
			if isUUID(model) {
				return usagef("--model must be a provider-native model name, not a UUID")
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

			promptVersion := 0
			if version != "" && !strings.EqualFold(version, "latest") {
				if isUUID(version) {
					return usagef("--version must be a positive version number or \"latest\", not a UUID")
				}
				n, err := strconv.Atoi(version)
				if err != nil || n < 1 {
					return usagef("--version must be a positive version number or \"latest\"")
				}
				promptVersion = n
			}

			req := api.CreateDeploymentRequest{
				Environment:     environment,
				Provider:        provider,
				Model:           model,
				PromptVersion:   promptVersion,
				Params:          paramsMap,
				ProviderOptions: optionsMap,
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
				{"Revision", deployment.Revision},
				{"Provider", deployment.Provider},
				{"Model", deployment.Model},
				{"Prompt version", promptVersionCell(deployment.PromptVersion)},
				{"Params", output.Dash(output.Compact(deployment.Params))},
				{"Provider options", output.Dash(output.Compact(deployment.ProviderOptions))},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "environment slug (server default: production)")
	cmd.Flags().StringVar(&provider, "provider", "openrouter", "model provider")
	cmd.Flags().StringVar(&model, "model", "", "provider-native model name")
	cmd.Flags().StringVar(&params, "params", "", "JSON object of model params, layered over the prompt defaults")
	cmd.Flags().StringVar(&providerOptions, "provider-options", "", "JSON object of provider options, layered over the model's")
	cmd.Flags().StringVar(&version, "version", "", "prompt version to deploy: positive number or \"latest\" (server default: latest)")
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
		revision    string
	)

	cmd := &cobra.Command{
		Use:   "rollback <prompt>",
		Short: "Re-commit a past revision",
		Long: `Roll back by re-committing an earlier revision's pins. History is never
rewound, so this produces a new revision label carrying the old
configuration.`,
		Example: "  " + meta.Name + " rollback support_reply --environment production --revision v2026.09.30-2",
		Args:    exactArgs(1, "<prompt>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("revision") {
				return usagef("--revision is required: the past revision label to restore")
			}
			if !revisionLabelPattern.MatchString(revision) {
				return usagef("--revision must look like v2026.09.30-1")
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
				{"Revision", fmt.Sprintf("%s (restored from %s)", deployment.Revision, revision)},
				{"Provider", deployment.Provider},
				{"Model", deployment.Model},
				{"Prompt version", promptVersionCell(deployment.PromptVersion)},
				{"Params", output.Dash(output.Compact(deployment.Params))},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "environment slug (server default: production)")
	cmd.Flags().StringVar(&revision, "revision", "", "revision label to restore")
	return cmd
}

func newPromoteCommand(g *globals) *cobra.Command {
	var (
		environment       string
		sourceEnvironment string
		revision          string
	)

	cmd := &cobra.Command{
		Use:   "promote <prompt>",
		Short: "Promote a source environment revision",
		Long: `Promote commits a source environment's current revision into a target
environment. It uses readable revision labels, never UUID aliases.`,
		Example: "  " + meta.Name + " promote support_reply --environment production --source-environment staging --revision v2026.09.30-2",
		Args:    exactArgs(1, "<prompt>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(environment) == "" {
				return usagef("--environment is required: the target environment slug")
			}
			if strings.TrimSpace(sourceEnvironment) == "" {
				return usagef("--source-environment is required: the source environment slug")
			}
			if !cmd.Flags().Changed("revision") {
				return usagef("--revision is required: the source revision label to promote")
			}
			if isUUID(revision) {
				return usagef("--revision must be a revision label like v2026.09.30-1, not a UUID")
			}
			if !revisionLabelPattern.MatchString(revision) {
				return usagef("--revision must look like v2026.09.30-1")
			}
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}

			deployment, err := client.Promote(cmd.Context(), org, project, args[0], api.PromoteRequest{
				Environment:       environment,
				SourceEnvironment: sourceEnvironment,
				Revision:          revision,
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
				{"Revision", fmt.Sprintf("%s (promoted from %s %s)", deployment.Revision, sourceEnvironment, revision)},
				{"Provider", deployment.Provider},
				{"Model", deployment.Model},
				{"Prompt version", promptVersionCell(deployment.PromptVersion)},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "target environment slug")
	cmd.Flags().StringVar(&sourceEnvironment, "source-environment", "", "source environment slug")
	cmd.Flags().StringVar(&revision, "revision", "", "source revision label")
	return cmd
}

func deploymentHeaders() []string {
	return []string{"ENVIRONMENT", "REV", "PROVIDER", "MODEL", "PROMPT VERSION", "PARAMS", "CREATED"}
}

func deploymentRows(deployments []api.Deployment) [][]string {
	rows := make([][]string, 0, len(deployments))
	for _, d := range deployments {
		rows = append(rows, []string{
			d.Environment,
			d.Revision,
			d.Provider,
			d.Model,
			promptVersionCell(d.PromptVersion),
			output.Dash(output.Compact(d.Params)),
			output.Date(d.CreatedAt),
		})
	}
	return rows
}

func promptVersionCell(n int) string {
	if n == 0 {
		return "-"
	}
	return output.Int(n)
}
