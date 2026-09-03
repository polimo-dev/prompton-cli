package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/config"
)

// uuidRE matches a canonical UUID. It is how the CLI tells a catalog model
// UUID from a provider model string, and a version UUID from a version number.
var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isUUID(s string) bool { return uuidRE.MatchString(s) }

// exactArgs is cobra.ExactArgs with a usage exit code and a clearer message.
func exactArgs(n int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return usagef("%s takes %d argument(s): %s", cmd.CommandPath(), n, usage)
		}
		return nil
	}
}

// noArgs rejects stray positional arguments.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usagef("%s takes no arguments (got %q)", cmd.CommandPath(), strings.Join(args, " "))
	}
	return nil
}

// parseJSONObject decodes a JSON object supplied on the command line.
func parseJSONObject(flag, raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, usagef("--%s must be a JSON object: %v", flag, err)
	}
	return m, nil
}

// readInput reads a file, or stdin when path is "-".
func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return nil, failf("read stdin: %v", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, failf("read %s: %v", path, err)
	}
	return raw, nil
}

// parsePins turns repeated --pin name=version flags into the prompt_pins map.
// A value may be a version UUID, a version number, or "latest"; the numeric
// and "latest" forms are resolved against the use case, which is why this
// needs the prompts.
func parsePins(pins []string, prompts []api.Prompt) (map[string]string, error) {
	if len(pins) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pins))
	for _, pin := range pins {
		name, value, ok := strings.Cut(pin, "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" || value == "" {
			return nil, usagef("--pin expects name=version (got %q)", pin)
		}
		if isUUID(value) {
			out[name] = value
			continue
		}
		id, err := resolveVersion(name, value, prompts)
		if err != nil {
			return nil, err
		}
		out[name] = id
	}
	return out, nil
}

// resolveVersion maps a prompt name plus a version number (or "latest") to the
// version UUID a deployment pin needs.
func resolveVersion(name, value string, prompts []api.Prompt) (string, error) {
	var prompt *api.Prompt
	for i := range prompts {
		if prompts[i].Name == name {
			prompt = &prompts[i]
			break
		}
	}
	if prompt == nil {
		available := make([]string, 0, len(prompts))
		for _, p := range prompts {
			available = append(available, p.Name)
		}
		if len(available) == 0 {
			return "", usagef("this use case has no prompt named %q", name)
		}
		return "", usagef("this use case has no prompt named %q (available: %s)", name, strings.Join(available, ", "))
	}
	if len(prompt.Versions) == 0 {
		return "", usagef("prompt %q has no committed versions yet — commit one first", name)
	}

	if strings.EqualFold(value, "latest") {
		best := prompt.Versions[0]
		for _, v := range prompt.Versions {
			if v.Number > best.Number {
				best = v
			}
		}
		return best.ID, nil
	}

	number, err := strconv.Atoi(value)
	if err != nil {
		return "", usagef("--pin %s=%s: expected a version number, \"latest\", or a version UUID", name, value)
	}
	for _, v := range prompt.Versions {
		if v.Number == number {
			return v.ID, nil
		}
	}
	// GET /use-cases/:key carries only the most recent 20 versions, so an
	// older number needs its UUID spelled out.
	return "", usagef("prompt %q has no version %d in the %d most recent versions — pin it by version UUID instead",
		name, number, len(prompt.Versions))
}

// existing decodes the resource a 409 carries into v. It reports whether the
// error was that kind of conflict; any other error passes through untouched.
func existing[T any](err error, v *T) (bool, error) {
	if err == nil {
		return false, nil
	}
	apiErr, ok := api.AsError(err)
	if !ok || apiErr.Code != api.CodeConflict {
		return false, err
	}
	_, raw, ok := apiErr.Conflict()
	if !ok {
		return false, err
	}
	if decodeErr := json.Unmarshal(raw, v); decodeErr != nil {
		return false, err
	}
	return true, nil
}

// alreadyExists is the verdict after a 409 whose resource has been printed:
// success under --idempotent, a plain failure otherwise.
func (g *globals) alreadyExists(kind, name string) error {
	if g.idempotent {
		g.printer().Info("%s %q already exists — nothing to do.", kind, name)
		return nil
	}
	return failf("%s %q already exists", kind, name)
}

// promptLine reads one line from stdin, for interactive choices. Closed input
// counts as a blank answer, so a command that is piped nothing reports what it
// is missing rather than complaining about EOF.
func (g *globals) promptLine(question string) (string, error) {
	fmt.Fprint(g.err, question)
	reader := bufio.NewReader(g.in)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" && !errors.Is(err, io.EOF) {
		return "", failf("could not read a reply: %v", err)
	}
	return strings.TrimSpace(line), nil
}

// promptSecret reads a secret without echoing it when stdin is a terminal, and
// reads one plain line when it is a pipe (which is how scripts feed it).
func (g *globals) promptSecret(question string) (string, error) {
	if f, ok := g.in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(g.err, question)
		raw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(g.err)
		if err != nil {
			return "", failf("could not read the secret: %v", err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	line, err := g.promptLine("")
	if err != nil {
		return "", err
	}
	return line, nil
}

// interactive reports whether it is worth asking the user a question.
func (g *globals) interactive() bool {
	if g.quiet || g.asJSON {
		return false
	}
	f, ok := g.in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// orgRow renders one org for a table.
func orgRow(o api.Org) []string {
	kind := "team"
	if o.Personal {
		kind = "personal"
	}
	return []string{o.Ref(), o.Name, kind, o.ID}
}

// toConfigOrgs converts API orgs into their stored form.
func toConfigOrgs(orgs []api.Org) []config.Org {
	out := make([]config.Org, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, config.Org{ID: o.ID, Name: o.Name, Slug: o.Slug, Personal: o.Personal})
	}
	return out
}
