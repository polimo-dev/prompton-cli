package cmd

import (
	"fmt"

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
					m.ModelID, output.Dash(m.DisplayName), m.Provider,
					pricingCell(m.Pricing), m.Status, m.ID,
				})
			}
			p.Table([]string{"MODEL", "DISPLAY NAME", "PROVIDER", "PRICE /M", "STATUS", "CATALOG ID"}, rows)
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
		Use:   "register <model-id>",
		Short: "Add a provider model to the catalog",
		Long: `Register a model by its provider-side id, for example
"openai/gpt-4o-mini".

For OpenRouter models the server fills in display name, pricing, context length
and capabilities from the public catalog; registration still succeeds if that
lookup fails.

The catalog UUID this prints is what a deployment pins — ` + "`deploy --model`" +
			` accepts either that UUID or the provider string.`,
		Example: "  " + meta.Name + " models register openai/gpt-4o-mini",
		Args:    exactArgs(1, "<model-id>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			modelID := args[0]

			model, createErr := client.RegisterModel(cmd.Context(), org, project, api.RegisterModelRequest{
				ModelID:     modelID,
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
					{"Model", model.ModelID},
					{"Display name", output.Dash(model.DisplayName)},
					{"Provider", model.Provider},
					{"Price /M", pricingCell(model.Pricing)},
					{"Context", output.Int(model.ContextLength)},
					{"Capabilities", output.Dash(output.Join(model.Capabilities))},
					{"Catalog id", model.ID},
				})
			}
			if wasExisting {
				return g.alreadyExists("model", modelID)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&displayName, "display-name", "", "human-facing name (looked up when omitted)")
	cmd.Flags().StringVar(&provider, "provider", "", "openrouter (default), groq, openai, anthropic, or google")
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
