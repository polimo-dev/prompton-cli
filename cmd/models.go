package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

func newModelsCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "models",
		Aliases: []string{"model"},
		Short:   "The project's model catalog",
		Args:    noArgs,
		RunE:    func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newModelsListCommand(g), newModelsRegisterCommand(g))
	return cmd
}

func newModelsListCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List catalog models",
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
			models, err := client.ListModels(cmd.Context(), org, project)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(map[string]any{"models": models})
			}
			rows := make([][]string, 0, len(models))
			for _, m := range models {
				rows = append(rows, []string{
					m.Model, output.Dash(m.DisplayName), m.Provider,
					pricingCell(m.Pricing), m.Status,
				})
			}
			p.Table([]string{"MODEL", "DISPLAY NAME", "PROVIDER", "PRICE /M", "STATUS"}, rows)
			return nil
		},
	}
}

func newModelsRegisterCommand(g *globals) *cobra.Command {
	var (
		displayName string
		provider    string
	)

	cmd := &cobra.Command{
		Use:   "register <model>",
		Short: "Add a provider model to the catalog",
		Long: `Register a model by its provider-side id, for example
"google/gemini-3.8-flash".

For OpenRouter models the server fills in display name, pricing, context length
and capabilities from the public catalog; registration still succeeds if that
lookup fails.

Deployments use the same provider and model strings.`,
		Example: "  " + meta.Name + " models register google/gemini-3.8-flash --provider openrouter",
		Args:    exactArgs(1, "<model>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			modelName := args[0]
			if strings.TrimSpace(modelName) == "" {
				return usagef("<model> is required")
			}
			if isUUID(modelName) {
				return usagef("<model> must be a provider-native model name, not a UUID")
			}

			model, createErr := client.RegisterModel(cmd.Context(), org, project, api.RegisterModelRequest{
				Model:       modelName,
				Provider:    provider,
				DisplayName: displayName,
			})
			var found api.Model
			wasExisting, err := existing(createErr, &found)
			if err != nil {
				return err
			}
			if wasExisting {
				model = &found
			}

			p := g.printer()
			if g.asJSON {
				if err := p.PrintJSON(model); err != nil {
					return err
				}
			} else {
				p.Fields([][2]string{
					{"Model", model.Model},
					{"Display name", output.Dash(model.DisplayName)},
					{"Provider", model.Provider},
					{"Price /M", pricingCell(model.Pricing)},
					{"Context", output.Int(model.ContextLength)},
					{"Capabilities", output.Dash(output.Join(model.Capabilities))},
				})
			}
			if wasExisting {
				return g.alreadyExists("model", modelName)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&displayName, "display-name", "", "human-facing name (looked up when omitted)")
	cmd.Flags().StringVar(&provider, "provider", "openrouter", "openrouter (default), groq, openai, anthropic, or google")
	return cmd
}

func pricingCell(p *api.Pricing) string {
	if p == nil {
		return "-"
	}
	currency := p.Currency
	if currency == "" {
		currency = "USD"
	}
	return fmt.Sprintf("%g/%g %s", p.InputPerM, p.OutputPerM, currency)
}
