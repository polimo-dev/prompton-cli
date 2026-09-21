package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/meta"
)

func newPromptContractCommand(g *globals) *cobra.Command {
	var candidateFile string
	cmd := &cobra.Command{
		Use:   "contract <key>",
		Short: "Inspect live, draft, and proposed response contracts without changing them",
		Long: `Show the full live content and response contract for every deployed environment,
the current draft, and provider response formats as JSON.

Use --candidate-file to also inspect an unapplied conversion or proposed edit.
The file must contain a JSON object with kind (chat or decision), optional engine
(liquid or raw), and messages for chat or decision: {state, questions} for decision.
Use "-" to read stdin. This command never saves a draft, deploys, or calls an LLM.`,
		Example: "  " + meta.Name + " prompt contract support_reply --json\n" +
			"  " + meta.Name + " prompt contract support_reply --candidate-file migration-candidate.json --json",
		Args: exactArgs(1, "<key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			var candidate json.RawMessage
			if cmd.Flags().Changed("candidate-file") {
				if candidateFile == "" {
					return usagef("--candidate-file must be a file path or \"-\" for stdin")
				}
				raw, err := readInput(candidateFile, g.in)
				if err != nil {
					return err
				}
				if err := validateContractCandidate(raw, candidateFile); err != nil {
					return err
				}
				candidate = raw
			}
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			contract, err := client.InspectPromptContract(cmd.Context(), org, project, args[0], candidate)
			if err != nil {
				return err
			}
			return g.printer().PrintJSON(contract)
		},
	}
	cmd.Flags().StringVar(&candidateFile, "candidate-file", "", "proposed content JSON to inspect without saving (\"-\" for stdin)")
	return cmd
}

func validateContractCandidate(raw []byte, name string) error {
	var content map[string]json.RawMessage
	if err := json.Unmarshal(raw, &content); err != nil || content == nil {
		return usagef("%s: candidate must be a JSON object", name)
	}
	var kind string
	if err := json.Unmarshal(content["kind"], &kind); err != nil || (kind != "chat" && kind != "decision") {
		return usagef("%s: candidate kind must be chat or decision", name)
	}
	if kind == "decision" {
		var decision map[string]json.RawMessage
		if err := json.Unmarshal(content["decision"], &decision); err != nil || decision == nil {
			return usagef("%s: decision candidate requires a decision object with state and questions", name)
		}
	} else {
		var messages []json.RawMessage
		if err := json.Unmarshal(content["messages"], &messages); err != nil || len(messages) == 0 {
			return usagef("%s: chat candidate requires a non-empty messages array", name)
		}
	}
	return nil
}
