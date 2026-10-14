package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/config"
)

func TestVariablesFilePreservesDefinitions(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/prompts", 201,
		`{"key":"reply","kind":"chat","variables":[]}`)
	file := filepath.Join(t.TempDir(), "variables.json")
	if err := os.WriteFile(file, []byte(`{"variables":[{"name":"question","type":"string","required":true,"description":"Customer question","example":"Where is my order?"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := h.run("prompt", "create", "reply", "--variables-file", file)
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	fields, ok := body["variables"].([]any)
	if !ok || len(fields) != 1 {
		t.Fatalf("variables = %v", body["variables"])
	}
	field := fields[0].(map[string]any)
	if field["name"] != "question" || field["required"] != true || field["description"] != "Customer question" || field["example"] != "Where is my order?" {
		t.Fatalf("variable metadata = %v", field)
	}
	if _, exists := body["input_schema"]; exists {
		t.Fatal("retired field was sent")
	}
}

func TestVariablesFileClearsDefinitions(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/prompts/reply", 200,
		`{"key":"reply","kind":"chat","variables":[]}`)
	file := filepath.Join(t.TempDir(), "variables.json")
	if err := os.WriteFile(file, []byte(`{"variables":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := h.run("prompt", "update", "reply", "--variables-file", file)
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	fields, ok := body["variables"].([]any)
	if !ok || len(fields) != 0 || len(body) != 1 {
		t.Fatalf("empty replacement = %v", body)
	}
}

func TestCreatePreservesExplicitEmptyVariables(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/prompts", 201,
		`{"key":"reply","kind":"chat","variables":[]}`)
	file := writeTemp(t, "variables.json", `{"variables":[]}`)
	got := h.run("prompt", "create", "reply", "--variables-file", file)
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	fields, ok := h.lastBody()["variables"].([]any)
	if !ok || len(fields) != 0 {
		t.Fatalf("explicit empty variables = %v", h.lastBody())
	}
}

func TestPromptListUsesVariablesHeading(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/prompts", 200,
		`{"prompts":[{"key":"reply","kind":"chat","variables":[{"name":"question","type":"string"}]}]}`)
	got := h.run("prompt", "list")
	if got.code != 0 || !strings.Contains(got.stdout, "VARIABLES") || strings.Contains(got.stdout, "INPUTS") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", got.code, got.stdout, got.stderr)
	}
}

func TestVariablesRejectRetiredFlagAndWrapper(t *testing.T) {
	for _, command := range []string{"create", "update"} {
		t.Run(command, func(t *testing.T) {
			h := newHarness(t)
			h.login(config.File{Org: "personal", Project: "helpdesk"})
			got := h.run("prompt", command, "reply", "--input-schema-file", "unused.json")
			if got.code != 2 || !strings.Contains(got.stderr, "unknown flag") || len(h.requests) != 0 {
				t.Fatalf("retired flag: exit=%d stderr=%s requests=%v", got.code, got.stderr, h.paths())
			}
			for _, input := range []string{`{"input_schema":[]}`, `{"input_schema":[],"variables":[]}`} {
				file := filepath.Join(t.TempDir(), "old.json")
				if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
					t.Fatal(err)
				}
				got = h.run("prompt", command, "reply", "--variables-file", file)
				if got.code != 2 || len(h.requests) != 0 {
					t.Fatalf("retired wrapper: exit=%d requests=%v", got.code, h.paths())
				}
			}
		})
	}
}
