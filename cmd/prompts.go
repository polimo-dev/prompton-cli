package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

func newPromptCommitCommand(g *globals) *cobra.Command {
	var (
		file    string
		engine  string
		message string
		format  string
	)

	cmd := &cobra.Command{
		Use:   "commit <prompt>",
		Short: "Commit a new immutable prompt version",
		Long: `Commit the contents of a file as the next prompt version for a prompt.

Versions are immutable and committing alone changes nothing at runtime — a
version goes live when a deployment revision pins it.

The file is read as Chat messages when it holds a JSON array (or an object with
a "messages" array). An object can also include tools, output and engine.
Native tool-call fields are preserved. Decision files use a JSON object with
kind "decision" and decision: {state, questions}. Conversation history belongs
in your app's provider request, not in PromptOn messages.

Decision prompts are committed from an object with kind "decision" and a
decision object containing state and questions. Any other non-empty file is
committed as one user message.
Pass "-" as the file to read stdin.`,
		Example: "  " + meta.Name + " prompt commit support_reply \\\n" +
			"      --file messages.json --message 'migrated from the app'",
		Args: exactArgs(1, "<prompt>"),
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
			if engine != "" {
				req.Engine = engine
			}
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
				{"Prompt", args[0]},
				{"Version", fmt.Sprintf("v%d", version.Number)},
				{"Engine", version.Engine},
				{"Variables", output.Dash(output.Join(version.DetectedVariables))},
				{"Message", output.Dash(output.Str(version.Message))},
				{"Id", version.ID},
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Chat/Decision JSON or plain text to commit (\"-\" for stdin)")
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
		return commitWithMetadata(raw, msgs, name)
	case "auto", "":
		if req, ok, err := decodeDecisionCommit(raw, name); ok || err != nil {
			return req, err
		}
		if msgs, err := decodeMessages(raw); err == nil {
			return commitWithMetadata(raw, msgs, name)
		} else {
			var slotErr *messageSlotError
			if errors.As(err, &slotErr) {
				return api.CommitVersionRequest{}, usagef("%s: %v", name, err)
			}
			if looksLikeJSONDocument(raw) {
				return api.CommitVersionRequest{}, usagef("%s: %v", name, err)
			}
		}
		if strings.TrimSpace(text) == "" {
			return api.CommitVersionRequest{}, usagef("%s is empty", name)
		}
		return api.CommitVersionRequest{Messages: []api.Message{{Role: "user", Content: text}}}, nil
	default:
		return api.CommitVersionRequest{}, usagef("--format must be auto or messages")
	}
}

func looksLikeJSONDocument(raw []byte) bool {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		return true
	}
	if strings.HasPrefix(trimmed, "{") {
		afterBrace := strings.TrimSpace(strings.TrimPrefix(trimmed, "{"))
		return strings.HasPrefix(afterBrace, "\"")
	}
	return false
}

type messageSlotError struct {
	message string
}

func (e *messageSlotError) Error() string {
	return e.message
}

func commitWithMetadata(raw []byte, messages []api.Message, name string) (api.CommitVersionRequest, error) {
	req := api.CommitVersionRequest{Messages: messages}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		if err := json.Unmarshal(raw, &req); err != nil {
			return api.CommitVersionRequest{}, usagef("%s: %v", name, err)
		}
		if len(req.Tools) > 0 && string(req.Tools) != "null" {
			var tools map[string]json.RawMessage
			if err := json.Unmarshal(req.Tools, &tools); err != nil || tools == nil {
				return api.CommitVersionRequest{}, usagef("%s: tools must be an object", name)
			}
		}
	}
	return req, nil
}

func decodeDecisionCommit(raw []byte, name string) (api.CommitVersionRequest, bool, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed[0] != '{' {
		return api.CommitVersionRequest{}, false, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &fields); err != nil || fields == nil {
		if looksLikeJSONDocument(raw) {
			return api.CommitVersionRequest{}, true, usagef("%s: malformed JSON document", name)
		}
		return api.CommitVersionRequest{}, false, nil
	}

	rawKind, hasKind := fields["kind"]
	rawDecision, hasDecision := fields["decision"]
	if !hasKind && !hasDecision {
		return api.CommitVersionRequest{}, false, nil
	}
	if !hasKind {
		return api.CommitVersionRequest{}, true, usagef("%s: decision commits require kind %q", name, "decision")
	}

	var kind string
	if err := json.Unmarshal(rawKind, &kind); err != nil {
		return api.CommitVersionRequest{}, true, usagef("%s: kind must be chat or decision", name)
	}
	switch kind {
	case "decision":
		if _, hasMessages := fields["messages"]; hasMessages {
			return api.CommitVersionRequest{}, true, usagef("%s: decision commits must not include messages", name)
		}
		if _, hasTools := fields["tools"]; hasTools {
			return api.CommitVersionRequest{}, true, usagef("%s: decision commits must not include tools", name)
		}
		if _, hasOutput := fields["output"]; hasOutput {
			return api.CommitVersionRequest{}, true, usagef("%s: decision commits must not include output", name)
		}
		decision, err := validateDecisionObject(rawDecision)
		if err != nil {
			return api.CommitVersionRequest{}, true, usagef("%s: %v", name, err)
		}
		var req api.CommitVersionRequest
		if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
			return api.CommitVersionRequest{}, true, usagef("%s: %v", name, err)
		}
		req.Kind = "decision"
		req.Decision = decision
		req.Messages = nil
		req.Tools = nil
		req.Output = nil
		return req, true, nil
	case "chat":
		if hasDecision {
			return api.CommitVersionRequest{}, true, usagef("%s: chat commits must not include decision", name)
		}
		return api.CommitVersionRequest{}, false, nil
	default:
		return api.CommitVersionRequest{}, true, usagef("%s: kind must be chat or decision", name)
	}
}

func validateDecisionObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("decision commits require a decision object with state and questions")
	}
	var decision map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decision); err != nil || decision == nil {
		return nil, fmt.Errorf("decision commits require a decision object with state and questions")
	}
	if _, ok := decision["state"]; !ok {
		return nil, fmt.Errorf("decision object requires state")
	}
	var questions map[string]json.RawMessage
	if err := json.Unmarshal(decision["questions"], &questions); err != nil || len(questions) == 0 {
		return nil, fmt.Errorf("decision object requires non-empty questions")
	}
	return raw, nil
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
			Kind     string          `json:"kind"`
			Messages []api.Message   `json:"messages"`
			Decision json.RawMessage `json:"decision"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
			return nil, err
		}
		if wrapper.Kind == "decision" || len(wrapper.Decision) > 0 {
			return nil, fmt.Errorf("decision commits require kind %q and a decision object with state and questions", "decision")
		}
		msgs = wrapper.Messages
	default:
		return nil, fmt.Errorf("not JSON messages")
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages found")
	}
	for i, m := range msgs {
		if m.Type == "slot" {
			return nil, &messageSlotError{
				message: fmt.Sprintf("message %d uses type %q: Message slots are not supported; compose conversation history in app code.", i+1, m.Type),
			}
		}
		if m.Role == "" {
			return nil, fmt.Errorf("message %d has no role", i+1)
		}
	}
	return msgs, nil
}
