package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

const (
	promptKindChat     = "chat"
	promptKindDecision = "decision"
)

func newPromptCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "prompt",
		Aliases: []string{"prompts"},
		Short:   "Prompts — one per LLM call site",
		Args:    noArgs,
		RunE:    func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(
		newPromptListCommand(g),
		newPromptGetCommand(g),
		newPromptContractCommand(g),
		newPromptCreateCommand(g),
		newPromptUpdateCommand(g),
		newPromptCommitCommand(g),
	)
	return cmd
}

func newPromptListCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the project's prompts",
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
			prompts, err := client.ListPrompts(cmd.Context(), org, project)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(map[string]any{"prompts": prompts})
			}
			rows := make([][]string, 0, len(prompts))
			for _, prompt := range prompts {
				rows = append(rows, []string{
					prompt.Key, prompt.Name, prompt.Kind,
					output.Dash(variableNames(prompt.Variables)),
					output.Dash(output.Compact(prompt.DefaultParams)),
				})
			}
			p.Table([]string{"KEY", "NAME", "KIND", "VARIABLES", "DEFAULT PARAMS"}, rows)
			return nil
		},
	}
}

func newPromptGetCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Show a prompt with recent versions and live deployments",
		Args:  exactArgs(1, "<key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			prompt, err := client.GetPrompt(cmd.Context(), org, project, args[0])
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(prompt)
			}

			p.Fields([][2]string{
				{"Key", prompt.Key},
				{"Name", prompt.Name},
				{"Kind", prompt.Kind},
				{"Description", output.Dash(output.Str(prompt.Description))},
				{"Tags", output.Dash(output.Join(prompt.Tags))},
				{"Default params", output.Dash(output.Compact(prompt.DefaultParams))},
			})

			if len(prompt.Variables) > 0 {
				p.Info("")
				p.Info("Variables:")
				rows := make([][]string, 0, len(prompt.Variables))
				for _, f := range prompt.Variables {
					rows = append(rows, []string{f.Name, f.Type, output.Bool(f.Required), output.Dash(output.Str(f.Description))})
				}
				p.Table([]string{"NAME", "TYPE", "REQUIRED", "DESCRIPTION"}, rows)
			}

			if versions := promptVersions(prompt); len(versions) > 0 {
				p.Info("")
				p.Info("Versions:")
				rows := make([][]string, 0, len(versions))
				for _, version := range versions {
					rows = append(rows, []string{
						fmt.Sprintf("v%d", version.Number),
						output.Dash(output.Str(version.Message)),
						output.Dash(output.Join(version.DetectedVariables)),
						output.Date(version.CreatedAt),
					})
				}
				p.Table([]string{"VERSION", "MESSAGE", "VARIABLES", "CREATED"}, rows)
			}

			if len(prompt.Deployments) > 0 {
				p.Info("")
				p.Info("Live deployments:")
				p.Table(deploymentHeaders(), deploymentRows(prompt.Deployments))
			}
			return nil
		},
	}
}

func newPromptCreateCommand(g *globals) *cobra.Command {
	var (
		kind          string
		name          string
		description   string
		variablesFile string
		defaultParams string
		tags          []string
	)

	cmd := &cobra.Command{
		Use:   "create <key>",
		Short: "Create a prompt",
		Long: `Create a prompt: one per place the app calls an LLM.

The key is the app's contract — 1–40 lowercase letters, digits or underscores,
starting with a letter —
and cannot be changed later.`,
		Example: "  " + meta.Name + " prompt create support_reply \\\n" +
			"      --name 'Support reply' --default-params '{\"temperature\":0.3}'",
		Args: exactArgs(1, "<key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			if kind != promptKindChat && kind != promptKindDecision {
				return usagef("--kind must be chat or decision")
			}
			params, err := parseJSONObject("default-params", defaultParams)
			if err != nil {
				return err
			}
			schema, err := g.readVariables(variablesFile)
			if err != nil {
				return err
			}
			var variables *[]api.Variable
			if cmd.Flags().Changed("variables-file") {
				variables = &schema
			}

			key := args[0]
			prompt, createErr := client.CreatePrompt(cmd.Context(), org, project, api.CreatePromptRequest{
				Key:           key,
				Name:          name,
				Kind:          kind,
				Description:   description,
				Variables:     variables,
				DefaultParams: params,
				Tags:          tags,
			})
			var found api.Prompt
			wasExisting, err := existing(createErr, &found)
			if err != nil {
				return err
			}
			if wasExisting {
				prompt = &found
			}

			p := g.printer()
			if g.asJSON {
				if err := p.PrintJSON(prompt); err != nil {
					return err
				}
			} else {
				p.Fields([][2]string{
					{"Key", prompt.Key},
					{"Name", prompt.Name},
					{"Kind", prompt.Kind},
				})
			}
			if wasExisting {
				return g.alreadyExists("prompt", key)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", promptKindChat, "prompt kind: chat or decision")
	cmd.Flags().StringVar(&name, "name", "", "display name (defaults to the key)")
	cmd.Flags().StringVar(&description, "description", "", "what this call site does")
	cmd.Flags().StringVar(&variablesFile, "variables-file", "", "JSON file declaring input variables (\"-\" for stdin)")
	cmd.Flags().StringVar(&defaultParams, "default-params", "", "JSON object of default model params")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated tags")
	return cmd
}

func newPromptUpdateCommand(g *globals) *cobra.Command {
	var (
		name          string
		description   string
		variablesFile string
		defaultParams string
		tags          []string
	)

	cmd := &cobra.Command{
		Use:   "update <key>",
		Short: "Change a prompt's describable fields",
		Long: `Update only the fields you pass. Variables and default params are replaced
wholesale, not merged. The key and kind are the app's contract and cannot
change — make a new prompt instead.`,
		Args: exactArgs(1, "<key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}

			var req api.UpdatePromptRequest
			flags := cmd.Flags()
			if flags.Changed("name") {
				req.Name = &name
			}
			if flags.Changed("description") {
				req.Description = &description
			}
			if flags.Changed("tags") {
				t := tags
				if t == nil {
					t = []string{}
				}
				req.Tags = &t
			}
			if flags.Changed("default-params") {
				params, err := parseJSONObject("default-params", defaultParams)
				if err != nil {
					return err
				}
				if params == nil {
					params = map[string]any{}
				}
				req.DefaultParams = &params
			}
			if flags.Changed("variables-file") {
				schema, err := g.readVariables(variablesFile)
				if err != nil {
					return err
				}
				if schema == nil {
					schema = []api.Variable{}
				}
				req.Variables = &schema
			}
			if req.Empty() {
				return usagef("nothing to update — pass at least one of --name, --description, --tags, --variables-file, --default-params")
			}

			prompt, err := client.UpdatePrompt(cmd.Context(), org, project, args[0], req)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(prompt)
			}
			p.Fields([][2]string{
				{"Key", prompt.Key},
				{"Name", prompt.Name},
				{"Kind", prompt.Kind},
				{"Description", output.Dash(output.Str(prompt.Description))},
				{"Tags", output.Dash(output.Join(prompt.Tags))},
				{"Default params", output.Dash(output.Compact(prompt.DefaultParams))},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&description, "description", "", "what this call site does")
	cmd.Flags().StringVar(&variablesFile, "variables-file", "", "JSON file declaring input variables (\"-\" for stdin)")
	cmd.Flags().StringVar(&defaultParams, "default-params", "", "JSON object of default model params (replaces)")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated tags (replaces)")
	return cmd
}

// readVariables accepts either a bare array of field objects or an object
// with an "variables" key, so a file copied out of a GET response works
// without editing.
func (g *globals) readVariables(path string) ([]api.Variable, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := readInput(path, g.in)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, usagef("--variables-file %s is empty", path)
	}

	if trimmed[0] == '[' {
		var fields []api.Variable
		if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
			return nil, usagef("--variables-file %s: %v", path, err)
		}
		return fields, nil
	}
	var wrapper struct {
		Variables    []api.Variable  `json:"variables"`
		RetiredField json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
		return nil, usagef("--variables-file %s: %v", path, err)
	}
	if wrapper.RetiredField != nil {
		return nil, usagef("--variables-file %s must use variables", path)
	}
	if wrapper.Variables == nil {
		return nil, usagef("--variables-file %s must hold a JSON array of fields, or an object with an \"variables\" array", path)
	}
	return wrapper.Variables, nil
}

func variableNames(fields []api.Variable) string {
	if len(fields) == 0 {
		return ""
	}
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.Required {
			names = append(names, f.Name+"*")
			continue
		}
		names = append(names, f.Name)
	}
	return strings.Join(names, ",")
}

func maxVersion(versions []api.VersionSummary) api.VersionSummary {
	best := versions[0]
	for _, v := range versions {
		if v.Number > best.Number {
			best = v
		}
	}
	return best
}
