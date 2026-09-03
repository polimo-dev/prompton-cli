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

// validKinds are the use-case kinds the API accepts.
var validKinds = []string{"chat", "text", "embedding"}

func newUseCasesCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "use-cases",
		Aliases: []string{"use-case", "usecases"},
		Short:   "Use cases — one per LLM call site",
		Args:    noArgs,
		RunE:    func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(
		newUseCasesListCommand(g),
		newUseCasesGetCommand(g),
		newUseCasesCreateCommand(g),
		newUseCasesUpdateCommand(g),
	)
	return cmd
}

func newUseCasesListCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the project's use cases",
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
			useCases, err := client.ListUseCases(cmd.Context(), org, project)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(map[string]any{"use_cases": useCases})
			}
			rows := make([][]string, 0, len(useCases))
			for _, uc := range useCases {
				rows = append(rows, []string{
					uc.Key, uc.Name, uc.Kind,
					output.Dash(variableNames(uc.InputSchema)),
					output.Dash(output.Compact(uc.DefaultParams)),
				})
			}
			p.Table([]string{"KEY", "NAME", "KIND", "INPUTS", "DEFAULT PARAMS"}, rows)
			return nil
		},
	}
}

func newUseCasesGetCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Show a use case with its prompts and live deployments",
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
			uc, err := client.GetUseCase(cmd.Context(), org, project, args[0])
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(uc)
			}

			p.Fields([][2]string{
				{"Key", uc.Key},
				{"Name", uc.Name},
				{"Kind", uc.Kind},
				{"Description", output.Dash(output.Str(uc.Description))},
				{"Tags", output.Dash(output.Join(uc.Tags))},
				{"Default params", output.Dash(output.Compact(uc.DefaultParams))},
				{"Id", uc.ID},
			})

			if len(uc.InputSchema) > 0 {
				p.Info("")
				p.Info("Inputs:")
				rows := make([][]string, 0, len(uc.InputSchema))
				for _, f := range uc.InputSchema {
					rows = append(rows, []string{f.Name, f.Type, output.Bool(f.Required), output.Dash(output.Str(f.Description))})
				}
				p.Table([]string{"NAME", "TYPE", "REQUIRED", "DESCRIPTION"}, rows)
			}

			if len(uc.Prompts) > 0 {
				p.Info("")
				p.Info("Prompts:")
				rows := make([][]string, 0, len(uc.Prompts))
				for _, pr := range uc.Prompts {
					latest := "-"
					if len(pr.Versions) > 0 {
						latest = fmt.Sprintf("v%d", maxVersion(pr.Versions).Number)
					}
					rows = append(rows, []string{pr.Name, output.Int(pr.VersionCount), latest, output.Dash(output.Str(pr.Description))})
				}
				p.Table([]string{"NAME", "VERSIONS", "LATEST", "DESCRIPTION"}, rows)
			}

			if len(uc.Deployments) > 0 {
				p.Info("")
				p.Info("Live deployments:")
				p.Table(deploymentHeaders(), deploymentRows(uc.Deployments))
			}
			return nil
		},
	}
}

func newUseCasesCreateCommand(g *globals) *cobra.Command {
	var (
		kind            string
		name            string
		description     string
		inputSchemaFile string
		defaultParams   string
		tags            []string
	)

	cmd := &cobra.Command{
		Use:   "create <key>",
		Short: "Create a use case",
		Long: `Create a use case: one per place the app calls an LLM.

The key is the app's contract — lowercase [a-z0-9_], starting with a letter —
and cannot be changed later. For kind chat and text a "default" prompt is
created alongside, ready for its first version.`,
		Example: "  " + meta.Name + " use-cases create diary_generation --kind chat \\\n" +
			"      --name 'Diary generation' --default-params '{\"temperature\":0.5}'",
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
			if !validKind(kind) {
				return usagef("--kind must be one of %s", strings.Join(validKinds, ", "))
			}
			params, err := parseJSONObject("default-params", defaultParams)
			if err != nil {
				return err
			}
			schema, err := g.readInputSchema(inputSchemaFile)
			if err != nil {
				return err
			}

			key := args[0]
			uc, createErr := client.CreateUseCase(cmd.Context(), org, project, api.CreateUseCaseRequest{
				Key:           key,
				Name:          name,
				Kind:          kind,
				Description:   description,
				InputSchema:   schema,
				DefaultParams: params,
				Tags:          tags,
			})
			var found api.UseCase
			wasExisting, err := existing(createErr, &found)
			if err != nil {
				return err
			}
			if wasExisting {
				uc = &found
			}

			p := g.printer()
			if g.asJSON {
				if err := p.PrintJSON(uc); err != nil {
					return err
				}
			} else {
				p.Fields([][2]string{
					{"Key", uc.Key},
					{"Name", uc.Name},
					{"Kind", uc.Kind},
					{"Id", uc.ID},
				})
			}
			if wasExisting {
				return g.alreadyExists("use case", key)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "chat", "chat, text, or embedding")
	cmd.Flags().StringVar(&name, "name", "", "display name (defaults to the key)")
	cmd.Flags().StringVar(&description, "description", "", "what this call site does")
	cmd.Flags().StringVar(&inputSchemaFile, "input-schema-file", "", "JSON file declaring input variables (\"-\" for stdin)")
	cmd.Flags().StringVar(&defaultParams, "default-params", "", "JSON object of default model params")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated tags")
	return cmd
}

func newUseCasesUpdateCommand(g *globals) *cobra.Command {
	var (
		name            string
		description     string
		inputSchemaFile string
		defaultParams   string
		tags            []string
	)

	cmd := &cobra.Command{
		Use:   "update <key>",
		Short: "Change a use case's describable fields",
		Long: `Update only the fields you pass. Input schema and default params are replaced
wholesale, not merged. The key and kind are the app's contract and cannot
change — make a new use case instead.`,
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

			var req api.UpdateUseCaseRequest
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
			if flags.Changed("input-schema-file") {
				schema, err := g.readInputSchema(inputSchemaFile)
				if err != nil {
					return err
				}
				if schema == nil {
					schema = []api.InputField{}
				}
				req.InputSchema = &schema
			}
			if req.Empty() {
				return usagef("nothing to update — pass at least one of --name, --description, --tags, --input-schema-file, --default-params")
			}

			uc, err := client.UpdateUseCase(cmd.Context(), org, project, args[0], req)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(uc)
			}
			p.Fields([][2]string{
				{"Key", uc.Key},
				{"Name", uc.Name},
				{"Kind", uc.Kind},
				{"Description", output.Dash(output.Str(uc.Description))},
				{"Tags", output.Dash(output.Join(uc.Tags))},
				{"Default params", output.Dash(output.Compact(uc.DefaultParams))},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&description, "description", "", "what this call site does")
	cmd.Flags().StringVar(&inputSchemaFile, "input-schema-file", "", "JSON file declaring input variables (\"-\" for stdin)")
	cmd.Flags().StringVar(&defaultParams, "default-params", "", "JSON object of default model params (replaces)")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated tags (replaces)")
	return cmd
}

// readInputSchema accepts either a bare array of field objects or an object
// with an "input_schema" key, so a file copied out of a GET response works
// without editing.
func (g *globals) readInputSchema(path string) ([]api.InputField, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := readInput(path, g.in)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, usagef("--input-schema-file %s is empty", path)
	}

	if trimmed[0] == '[' {
		var fields []api.InputField
		if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
			return nil, usagef("--input-schema-file %s: %v", path, err)
		}
		return fields, nil
	}
	var wrapper struct {
		InputSchema []api.InputField `json:"input_schema"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
		return nil, usagef("--input-schema-file %s: %v", path, err)
	}
	if wrapper.InputSchema == nil {
		return nil, usagef("--input-schema-file %s must hold a JSON array of fields, or an object with an \"input_schema\" array", path)
	}
	return wrapper.InputSchema, nil
}

func validKind(kind string) bool {
	for _, k := range validKinds {
		if kind == k {
			return true
		}
	}
	return false
}

func variableNames(fields []api.InputField) string {
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
