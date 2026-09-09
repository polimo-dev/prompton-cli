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

func newPromptsCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "prompts",
		Aliases: []string{"prompt"},
		Short:   "Prompt versions",
		Args:    noArgs,
		RunE:    func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newPromptsCommitCommand(g))
	return cmd
}

func newPromptsCommitCommand(g *globals) *cobra.Command {
	var (
		file    string
		engine  string
		message string
		format  string
	)

	cmd := &cobra.Command{
		Use:   "commit <use-case>",
		Short: "Commit a new immutable prompt version",
		Long: `Commit the contents of a file as the next prompt version for a use case.

Versions are immutable and committing alone changes nothing at runtime — a
version goes live when a deployment revision pins it.

The file is read as chat messages when it holds a JSON array (or an object with
a "messages" array). Any other non-empty file is committed as one user message.
Pass "-" as the file to read stdin.`,
		Example: "  " + meta.Name + " prompts commit support_reply \\\n" +
			"      --file messages.json --message 'migrated from the app'",
		Args: exactArgs(1, "<use-case>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return usagef("--file is required (use \"-\" to read stdin)")
			}
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, project, err := g.scope()
			if err != nil {
				return err
			}
			raw, err := readInput(file, g.in)
			if err != nil {
				return err
			}

			req, err := buildCommit(raw, format, file)
			if err != nil {
				return err
			}
			req.Engine = engine
			req.Message = message

			version, err := client.CommitVersion(cmd.Context(), org, project, args[0], req)
			if err != nil {
				return err
			}

			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(version)
			}
			p.Fields([][2]string{
				{"Use case", args[0]},
				{"Version", fmt.Sprintf("v%d", version.Number)},
				{"Engine", version.Engine},
				{"Variables", output.Dash(output.Join(version.DetectedVariables))},
				{"Message", output.Dash(output.Str(version.Message))},
				{"Id", version.ID},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "messages JSON or plain text to commit (\"-\" for stdin)")
	cmd.Flags().StringVar(&engine, "engine", "", "template engine: liquid (default) or raw")
	cmd.Flags().StringVar(&message, "message", "", "commit message explaining the change")
	cmd.Flags().StringVar(&format, "format", "auto", "how to read the file: auto or messages")
	_ = cmd.Flags().MarkHidden("format")
	return cmd
}

// buildCommit turns file contents into a commit request.
func buildCommit(raw []byte, format, name string) (api.CommitVersionRequest, error) {
	text := string(raw)
	switch format {
	case "messages":
		msgs, err := decodeMessages(raw)
		if err != nil {
			return api.CommitVersionRequest{}, usagef("%s: %v", name, err)
		}
		return api.CommitVersionRequest{Messages: msgs}, nil
	case "auto", "":
		if msgs, err := decodeMessages(raw); err == nil {
			return api.CommitVersionRequest{Messages: msgs}, nil
		}
		if strings.TrimSpace(text) == "" {
			return api.CommitVersionRequest{}, usagef("%s is empty", name)
		}
		return api.CommitVersionRequest{Messages: []api.Message{{Role: "user", Content: text}}}, nil
	default:
		return api.CommitVersionRequest{}, usagef("--format must be auto or messages")
	}
}

// decodeMessages accepts a bare array of messages or an object wrapping one,
// so a file lifted straight out of a GET response also works.
func decodeMessages(raw []byte) ([]api.Message, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty file")
	}
	var msgs []api.Message
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal([]byte(trimmed), &msgs); err != nil {
			return nil, err
		}
	case '{':
		var wrapper struct {
			Messages []api.Message `json:"messages"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
			return nil, err
		}
		msgs = wrapper.Messages
	default:
		return nil, fmt.Errorf("not JSON messages")
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages found")
	}
	for i, m := range msgs {
		if m.Role == "" {
			return nil, fmt.Errorf("message %d has no role", i+1)
		}
	}
	return msgs, nil
}
