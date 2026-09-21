package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestInspectPromptContractPreservesUnknownAndNumericFields(t *testing.T) {
	s := newStub(t, 200, `{"prompt_key":"reply","future":{"id":9007199254740993},"draft":{"contract":{"answers":{"custom":{"type":"score"}}}}}`)
	got, err := s.client("t").InspectPromptContract(ctx(), "personal", "helpdesk", "reply", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs/personal/projects/helpdesk/prompts/reply/contract")
	if string(got["future"]) != `{"id":9007199254740993}` {
		t.Fatalf("unknown field lost precision: %s", got["future"])
	}
}

func TestInspectPromptContractCandidateUsesReadOnlyPost(t *testing.T) {
	s := newStub(t, 200, `{"candidate":{"content":{"kind":"chat"}}}`)
	candidate := json.RawMessage(`{"kind":"chat","messages":[{"role":"user","content":"Hi"}]}`)
	if _, err := s.client("t").InspectPromptContract(ctx(), "personal", "helpdesk", "reply", candidate); err != nil {
		t.Fatal(err)
	}
	c := s.expect(http.MethodPost, "/api/v1/orgs/personal/projects/helpdesk/prompts/reply/contract")
	if !strings.Contains(c.Body, `"candidate":{"kind":"chat"`) {
		t.Fatalf("candidate body = %s", c.Body)
	}
}
