package api_test

import (
	"encoding/json"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/api"
)

func TestResourceResponsesDoNotReemitPrivateIDs(t *testing.T) {
	cases := []struct {
		name, body string
		value      any
	}{
		{"user", `{"id":"private-user","email":"ada@example.com"}`, &api.User{}},
		{"organization", `{"id":"private-org","slug":"acme","name":"Acme"}`, &api.Org{}},
		{"environment", `{"id":"private-env","slug":"staging"}`, &api.Environment{}},
		{"project", `{"id":"private-project","slug":"helpdesk","environments":[{"id":"private-env","slug":"staging"}]}`, &api.Project{}},
		{"prompt", `{"id":"private-prompt","key":"reply","versions":[{"id":"private-version","number":1}]}`, &api.Prompt{}},
		{"template", `{"id":"private-template","name":"default","versions":[{"id":"private-version","number":1}]}`, &api.PromptTemplate{}},
		{"version", `{"id":"private-version","prompt_template_id":"private-template","number":1}`, &api.PromptVersion{}},
		{"model", `{"id":"private-model","provider":"openai","model":"shared/name"}`, &api.Model{}},
		{"deployment", `{"id":"private-deployment","revision_id":"private-revision","source_deployment_id":"private-source","model_id":"private-model","template_pins":{"default":"private-version"},"prompt_version_id":"private-version","revision":"v2026.10.14-1","sequence":3,"prompt_version_number":2,"provider":"openai","model":"shared/name"}`, &api.Deployment{}},
		{"SDK key", `{"id":"private-key","name":"Server","key_prefix":"ptn_helpdesk_1234","key":"secret"}`, &api.APIKey{}},
		{"provider key", `{"id":"private-provider-key","connected":true,"provider":"openrouter","hint":"masked"}`, &api.ProviderKey{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.body), tc.value); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var body any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			assertNoResourceIDs(t, body)
			if tc.name == "model" {
				if body.(map[string]any)["model"] != "shared/name" {
					t.Fatalf("lost readable model: %s", raw)
				}
			}
			if tc.name == "deployment" {
				m := body.(map[string]any)
				if m["sequence"] != float64(3) || m["prompt_version_number"] != float64(2) {
					t.Fatalf("lost readable deployment metadata: %s", raw)
				}
			}
		})
	}
}

func assertNoResourceIDs(t *testing.T, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch key {
			case "id", "model_id", "prompt_template_id", "prompt_version_id", "template_pins", "revision_id", "source_deployment_id":
				t.Errorf("private resource field %q remains", key)
			}
			assertNoResourceIDs(t, child)
		}
	case []any:
		for _, child := range v {
			assertNoResourceIDs(t, child)
		}
	}
}

func TestReadableResponsePreservesOpaqueUserContent(t *testing.T) {
	raw := []byte(`{"number":1,"kind":"chat","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"0192a3b4-1111-7111-8111-111111111111","type":"function","function":{"name":"lookup","arguments":"{\"id\":9007199254740993}"}}]}]}`)
	var version api.PromptVersion
	if err := json.Unmarshal(raw, &version); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(version.Messages)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]json.RawMessage
	_ = json.Unmarshal(raw, &original)
	var expected, actual any
	_ = json.Unmarshal(original["messages"], &expected)
	_ = json.Unmarshal(got, &actual)
	expectedBytes, _ := json.Marshal(expected)
	actualBytes, _ := json.Marshal(actual)
	if string(expectedBytes) != string(actualBytes) {
		t.Fatalf("native content changed: %s", got)
	}
}
