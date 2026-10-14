package cmd_test

import (
	"strings"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/config"
)

func TestPromptGetReadsCanonicalTemplateVersionsWithoutIDs(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/prompts/reply", 200, `{"key":"reply","name":"Reply","kind":"chat","templates":[{"name":"default","versions":[{"number":4,"message":"Shorter answer","detected_variables":["question"]}]}],"deployments":[{"revision":"v2026.10.14-1","sequence":2,"environment":"staging","provider":"openrouter","model":"shared/model","prompt_version_number":4}]}`)
	got := h.run("prompt", "get", "reply")
	if got.code != 0 {
		t.Fatalf("exit=%d: %s", got.code, got.stderr)
	}
	for _, want := range []string{"v4", "Shorter answer", "shared/model", "v2026.10.14-1"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("missing %q: %s", want, got.stdout)
		}
	}
	got = h.run("prompt", "get", "reply", "--json")
	body := got.json(t)
	templates, ok := body["templates"].([]any)
	if !ok || len(templates) != 1 {
		t.Fatalf("lost templates: %v", body)
	}
	version := templates[0].(map[string]any)["versions"].([]any)[0].(map[string]any)
	if version["number"] != float64(4) {
		t.Fatalf("lost prompt version: %v", version)
	}
	if _, ok := version["id"]; ok {
		t.Errorf("private version id emitted: %v", version)
	}
}
