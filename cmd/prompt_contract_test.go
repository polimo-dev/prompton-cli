package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/config"
)

const contractPath = "/api/v1/orgs/personal/projects/helpdesk/prompts/support_reply/contract"
const contractReply = `{"prompt_key":"support_reply","draft":{"content":{"kind":"decision","decision":{"state":{"id":9007199254740993},"questions":{"mood":{"type":"choice","criteria":{"-3":null,"+3":null}}}}},"contract":{"kind":"decision"}},"deployed":[{"environment":"production","revision":"v2026.09.30-2","api":"chat_completions","request_path":"/api/v1/chat/completions","content":{"kind":"chat"}}],"response_formats":{"decisions":{"choice":{"choice":"string"}}}}`

func TestPromptContractInspectsWithoutMutation(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle(contractPath, 200, contractReply)
	got := h.run("prompt", "contract", "support_reply", "--json")
	if got.code != 0 {
		t.Fatalf("code = %d: %s", got.code, got.stderr)
	}
	if got.json(t)["prompt_key"] != "support_reply" || !strings.Contains(got.stdout, "9007199254740993") {
		t.Fatalf("contract fields or numeric precision lost: %s", got.stdout)
	}
	if paths := h.paths(); len(paths) != 1 || paths[0] != "GET "+contractPath {
		t.Fatalf("requests = %v", paths)
	}
	if h.requests[0].Body != "" {
		t.Fatalf("GET body = %s", h.requests[0].Body)
	}
}

func TestPromptContractInspectsCandidateFromFileOrStdin(t *testing.T) {
	for _, stdin := range []bool{false, true} {
		t.Run(map[bool]string{true: "stdin", false: "file"}[stdin], func(t *testing.T) {
			h := newHarness(t)
			h.login(config.File{Org: "personal", Project: "helpdesk"})
			h.handle(contractPath, 200, contractReply)
			candidate := `{"kind":"decision","engine":"liquid","decision":{"state":{"id":9007199254740993},"questions":{"mood":{"type":"choice","instructions":"Pick","criteria":{"-3":null,"+3":null}}}}}`
			file := "-"
			if stdin {
				h.stdin = strings.NewReader(candidate)
			} else {
				file = filepath.Join(t.TempDir(), "candidate.json")
				if err := os.WriteFile(file, []byte(candidate), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got := h.run("prompt", "contract", "support_reply", "--candidate-file", file, "--json")
			if got.code != 0 {
				t.Fatalf("code = %d: %s", got.code, got.stderr)
			}
			if paths := h.paths(); len(paths) != 1 || paths[0] != "POST "+contractPath {
				t.Fatalf("requests = %v", paths)
			}
			if !strings.Contains(h.requests[0].Body, "9007199254740993") {
				t.Fatalf("candidate changed numeric precision: %s", h.requests[0].Body)
			}
			if h.lastBody()["candidate"].(map[string]any)["kind"] != "decision" {
				t.Fatalf("missing candidate wrapper: %s", h.requests[0].Body)
			}
		})
	}
}

func TestPromptContractRejectsMalformedCandidateWithoutRequest(t *testing.T) {
	for _, content := range []string{"", "null", "[]", "{", "{}", `{"kind":"audio"}`, `{"kind":"decision"}`, `{"kind":"decision","decision":[]}`, `{"kind":"chat","messages":null}`, `{"kind":"chat","messages":[]}`} {
		t.Run(content, func(t *testing.T) {
			h := newHarness(t)
			h.login(config.File{Org: "personal", Project: "helpdesk"})
			h.stdin = strings.NewReader(content)
			got := h.run("prompt", "contract", "support_reply", "--candidate-file", "-", "--json")
			if got.code != 2 || len(h.requests) != 0 || got.stdout != "" {
				t.Fatalf("result = %+v, requests = %v", got, h.paths())
			}
			if !json.Valid([]byte(got.stderr)) {
				t.Fatalf("error is not JSON: %s", got.stderr)
			}
		})
	}
}

func TestPromptContractMissingFileAndDeniedAccess(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	got := h.run("prompt", "contract", "support_reply", "--candidate-file", filepath.Join(t.TempDir(), "missing.json"))
	if got.code == 0 || len(h.requests) != 0 {
		t.Fatalf("missing file result = %+v, requests = %v", got, h.paths())
	}
	h.handle(contractPath, 404, `{"error":{"code":"not_found","message":"unknown project","details":{}}}`)
	got = h.run("prompt", "contract", "support_reply", "--json")
	if got.code != 1 || got.stdout != "" || !strings.Contains(got.stderr, "not_found") {
		t.Fatalf("denied result = %+v", got)
	}
}
