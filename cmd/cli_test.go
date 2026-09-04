package cmd_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/config"
)

// ---- exit codes and credential handling -----------------------------------

func TestWhoamiWithoutALoginFails(t *testing.T) {
	h := newHarness(t)
	got := h.run("whoami", "--host", h.srv.URL)

	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "not logged in") {
		t.Errorf("stderr = %q, want an instruction to log in", got.stderr)
	}
	if len(h.requests) != 0 {
		t.Errorf("no request should be attempted without a token, got %v", h.paths())
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	h := newHarness(t)
	got := h.run("frobnicate")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2 for a usage mistake", got.code)
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	got := h.run("orgs", "list", "--nope")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestWrongArgumentCountIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	got := h.run("use-cases", "get")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "<key>") {
		t.Errorf("stderr = %q, want the expected argument named", got.stderr)
	}
}

func TestMissingOrgIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{})
	got := h.run("projects", "list")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "--org") {
		t.Errorf("stderr = %q, want a pointer to --org", got.stderr)
	}
}

func TestMissingProjectIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	got := h.run("use-cases", "list")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "--project") {
		t.Errorf("stderr = %q, want a pointer to --project", got.stderr)
	}
}

func TestServerErrorExitsOne(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs", 401, `{"error":{"code":"unauthorized","message":"token revoked","details":{}}}`)

	got := h.run("orgs", "list")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "token revoked") {
		t.Errorf("stderr = %q", got.stderr)
	}
}

func TestJSONErrorsGoToStderrAsAnEnvelope(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs", 404, `{"error":{"code":"not_found","message":"nope","details":{}}}`)

	got := h.run("orgs", "list", "--json")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if strings.TrimSpace(got.stdout) != "" {
		t.Errorf("stdout must stay empty on failure, got %q", got.stdout)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Status  int    `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(got.stderr), &env); err != nil {
		t.Fatalf("stderr is not a JSON envelope: %v\n%s", err, got.stderr)
	}
	if env.Error.Code != "not_found" || env.Error.Status != 404 {
		t.Errorf("envelope = %+v", env.Error)
	}
}

// ---- precedence -----------------------------------------------------------

func TestFlagTokenBeatsEnvironmentAndFile(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Token: "file-token", Org: "personal"})
	t.Setenv("PTN_TOKEN", "env-token")
	h.handle("/api/v1/orgs", 200, `{"organizations":[]}`)

	got := h.run("orgs", "list", "--token", "flag-token")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if auth := h.requests[0].Auth; auth != "Bearer flag-token" {
		t.Errorf("Authorization = %q, want the flag to win", auth)
	}
}

func TestEnvironmentTokenBeatsFile(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Token: "file-token", Org: "personal"})
	t.Setenv("PTN_TOKEN", "env-token")
	h.handle("/api/v1/orgs", 200, `{"organizations":[]}`)

	if got := h.run("orgs", "list"); got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if auth := h.requests[0].Auth; auth != "Bearer env-token" {
		t.Errorf("Authorization = %q, want the environment to win over the file", auth)
	}
}

func TestFlagOrgBeatsTheConfiguredDefault(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/acme/projects", 200, `{"projects":[]}`)

	if got := h.run("projects", "list", "--org", "acme"); got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if h.requests[0].Path != "/api/v1/orgs/acme/projects" {
		t.Errorf("path = %q, want the flag org", h.requests[0].Path)
	}
}

// ---- login ----------------------------------------------------------------

const deviceCodeReply = `{"device_code":"opaque","user_code":"ABCD-EFGH",
  "verification_uri":"https://prompton.example/device",
  "verification_uri_complete":"https://prompton.example/device?code=ABCD-EFGH",
  "expires_in":900,"interval":0}`

func TestLoginStoresTheSessionAndAdoptsTheOnlyOrg(t *testing.T) {
	h := newHarness(t)
	h.handle("/api/v1/device/code", 200, deviceCodeReply)
	h.handle("/api/v1/device/token", 200, `{"token":"cli-session","user":{"id":"u1","email":"ada@example.com"},
	  "organizations":[{"id":"o1","name":"Ada","slug":null,"personal":true}]}`)

	got := h.run("login", "--host", h.srv.URL, "--no-browser")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "ABCD-EFGH") {
		t.Errorf("the user code must be shown:\n%s", got.stdout)
	}
	if !strings.Contains(got.stdout, "ada@example.com") {
		t.Errorf("the signed-in identity must be confirmed:\n%s", got.stdout)
	}

	saved := h.config()
	if saved.Token != "cli-session" {
		t.Errorf("token = %q", saved.Token)
	}
	if saved.Org != "personal" {
		t.Errorf("org = %q, want a single org adopted automatically", saved.Org)
	}
	if saved.User == nil || saved.User.Email != "ada@example.com" {
		t.Errorf("user = %+v", saved.User)
	}
	if len(saved.Orgs) != 1 {
		t.Errorf("organizations = %+v", saved.Orgs)
	}
	if saved.Host != h.srv.URL {
		t.Errorf("host = %q", saved.Host)
	}

	info, err := os.Stat(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}
}

func TestLoginWithSeveralOrgsLeavesTheChoiceOpen(t *testing.T) {
	h := newHarness(t)
	h.handle("/api/v1/device/code", 200, deviceCodeReply)
	h.handle("/api/v1/device/token", 200, `{"token":"cli-session","user":{"id":"u1","email":"ada@example.com"},
	  "organizations":[{"id":"o1","name":"Ada","slug":null,"personal":true},
	                   {"id":"o2","name":"Acme","slug":"acme","personal":false}]}`)

	got := h.run("login", "--host", h.srv.URL, "--no-browser")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if saved := h.config(); saved.Org != "" {
		t.Errorf("org = %q, want no guess when there are two", saved.Org)
	}
	if !strings.Contains(got.stderr, "use --org") {
		t.Errorf("stderr = %q, want advice on picking an org", got.stderr)
	}
}

func TestLoginWithOrgFlagPicksIt(t *testing.T) {
	h := newHarness(t)
	h.handle("/api/v1/device/code", 200, deviceCodeReply)
	h.handle("/api/v1/device/token", 200, `{"token":"cli-session","user":{"id":"u1","email":"ada@example.com"},
	  "organizations":[{"id":"o1","name":"Ada","slug":null,"personal":true},
	                   {"id":"o2","name":"Acme","slug":"acme","personal":false}]}`)

	got := h.run("login", "--host", h.srv.URL, "--no-browser", "--org", "acme")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if saved := h.config(); saved.Org != "acme" {
		t.Errorf("org = %q", saved.Org)
	}
}

func TestLoginWithAnOrgYouAreNotInIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.handle("/api/v1/device/code", 200, deviceCodeReply)
	h.handle("/api/v1/device/token", 200, `{"token":"cli-session","user":{"id":"u1"},
	  "organizations":[{"id":"o1","name":"Ada","slug":null,"personal":true}]}`)

	got := h.run("login", "--host", h.srv.URL, "--no-browser", "--org", "somebody-else")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestLoginJSONDoesNotLeakTheTokenToStdout(t *testing.T) {
	h := newHarness(t)
	h.handle("/api/v1/device/code", 200, deviceCodeReply)
	h.handle("/api/v1/device/token", 200, `{"token":"cli-session","user":{"id":"u1","email":"ada@example.com"},
	  "organizations":[{"id":"o1","name":"Ada","slug":null,"personal":true}]}`)

	got := h.run("login", "--host", h.srv.URL, "--no-browser", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	payload := got.json(t)
	if payload["org"] != "personal" {
		t.Errorf("payload = %v", payload)
	}
	if strings.Contains(got.stdout, "cli-session") {
		t.Error("the session token must not be printed; it lives in the config file")
	}
	if !strings.Contains(got.stderr, "ABCD-EFGH") {
		t.Error("under --json the human prompts belong on stderr")
	}
}

func TestLoginDeniedExitsOne(t *testing.T) {
	h := newHarness(t)
	h.handle("/api/v1/device/code", 200, deviceCodeReply)
	h.handle("/api/v1/device/token", 400, `{"error":{"code":"access_denied","message":"denied","details":{}}}`)

	got := h.run("login", "--host", h.srv.URL, "--no-browser")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "denied") {
		t.Errorf("stderr = %q", got.stderr)
	}
	if _, err := os.Stat(h.path); !os.IsNotExist(err) {
		t.Error("a denied login must not write a config file")
	}
}

func TestLoginExpiredExitsOne(t *testing.T) {
	h := newHarness(t)
	h.handle("/api/v1/device/code", 200, deviceCodeReply)
	h.handle("/api/v1/device/token", 400, `{"error":{"code":"expired_token","message":"gone","details":{}}}`)

	got := h.run("login", "--host", h.srv.URL, "--no-browser")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "expired") {
		t.Errorf("stderr = %q", got.stderr)
	}
}

// ---- logout ---------------------------------------------------------------

func TestLogoutRevokesThenClears(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk", User: &config.User{ID: "u1"}})
	h.handle("/api/v1/sessions/revoke", 204, ``)

	got := h.run("logout")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if len(h.requests) != 1 || h.requests[0].Path != "/api/v1/sessions/revoke" {
		t.Fatalf("requests = %v, want the session revoked server-side", h.paths())
	}
	if h.requests[0].Auth != "Bearer session-token" {
		t.Errorf("revoke must present the token being revoked, got %q", h.requests[0].Auth)
	}

	saved := h.config()
	if saved.Token != "" || saved.User != nil || saved.Org != "" || saved.Project != "" {
		t.Errorf("logout left state behind: %+v", saved)
	}
	if saved.Host != h.srv.URL {
		t.Errorf("host = %q, want the host remembered for the next login", saved.Host)
	}
}

func TestLogoutClearsEvenWhenRevocationFails(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/sessions/revoke", 401, `{"error":{"code":"unauthorized","message":"already gone","details":{}}}`)

	got := h.run("logout")
	if got.code != 0 {
		t.Fatalf("exit = %d, want a token the server already rejects to still be cleared: %s", got.code, got.stderr)
	}
	if saved := h.config(); saved.Token != "" {
		t.Errorf("token = %q, want it cleared locally", saved.Token)
	}
	if !strings.Contains(got.stderr, "already invalid") {
		t.Errorf("stderr = %q, want the user told what happened", got.stderr)
	}
}

func TestLogoutWithoutASessionIsANoop(t *testing.T) {
	h := newHarness(t)
	got := h.run("logout", "--host", h.srv.URL)
	if got.code != 0 {
		t.Errorf("exit = %d, want 0", got.code)
	}
	if len(h.requests) != 0 {
		t.Errorf("nothing to revoke, got %v", h.paths())
	}
}

// ---- whoami and orgs ------------------------------------------------------

func TestWhoamiJSON(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "acme", Project: "helpdesk"})
	h.handle("/api/v1/me", 200, `{"user":{"id":"u1","email":"ada@example.com"},
	  "organizations":[{"id":"o2","name":"Acme","slug":"acme","personal":false}]}`)

	got := h.run("whoami", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	payload := got.json(t)
	if payload["org"] != "acme" || payload["project"] != "helpdesk" {
		t.Errorf("payload = %v, want the active scope reported", payload)
	}
	user := payload["user"].(map[string]any)
	if user["email"] != "ada@example.com" {
		t.Errorf("user = %v", user)
	}
}

func TestOrgsListTable(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{})
	h.handle("/api/v1/orgs", 200, `{"organizations":[
	  {"id":"o1","name":"Ada","slug":null,"personal":true},
	  {"id":"o2","name":"Acme","slug":"acme","personal":false}]}`)

	got := h.run("orgs", "list")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "REF") || !strings.Contains(got.stdout, "personal") {
		t.Errorf("table = %q", got.stdout)
	}
	if !strings.Contains(got.stdout, "acme") {
		t.Errorf("table = %q, want the team slug as its ref", got.stdout)
	}
}

// ---- use ------------------------------------------------------------------

func TestUseVerifiesAndPersists(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{})
	h.handle("/api/v1/orgs/acme", 200, `{"id":"o2","name":"Acme","slug":"acme","personal":false}`)
	h.handle("/api/v1/orgs/acme/projects", 200,
		`{"projects":[{"id":"p1","slug":"helpdesk","name":"Helpdesk","timezone":"Etc/UTC","created_at":"","environments":[]}]}`)

	got := h.run("use", "--org", "acme", "--project", "helpdesk")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	saved := h.config()
	if saved.Org != "acme" || saved.Project != "helpdesk" {
		t.Errorf("config = %+v", saved)
	}
}

func TestUseRejectsAnUnknownProject(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/personal", 200, `{"id":"o1","name":"Ada","slug":null,"personal":true}`)
	h.handle("/api/v1/orgs/personal/projects", 200,
		`{"projects":[{"id":"p1","slug":"helpdesk","name":"Helpdesk","timezone":"Etc/UTC","created_at":"","environments":[]}]}`)

	got := h.run("use", "--org", "personal", "--project", "typo")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "helpdesk") {
		t.Errorf("stderr = %q, want the real project names listed", got.stderr)
	}
	if saved := h.config(); saved.Project != "" {
		t.Errorf("a rejected project must not be stored, got %q", saved.Project)
	}
}

func TestUseRejectsAnInvisibleOrg(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{})
	h.handle("/api/v1/orgs/someone-else", 404, `{"error":{"code":"not_found","message":"nope","details":{}}}`)

	got := h.run("use", "--org", "someone-else")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "visible") {
		t.Errorf("stderr = %q", got.stderr)
	}
}

func TestUseWithNothingToSetIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	if got := h.run("use"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestUseChangingOrgForgetsTheProject(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/acme", 200, `{"id":"o2","name":"Acme","slug":"acme","personal":false}`)

	got := h.run("use", "--org", "acme")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if saved := h.config(); saved.Project != "" {
		t.Errorf("project = %q, want the stale pointer dropped with the org change", saved.Project)
	}
}

// ---- projects -------------------------------------------------------------

const projectBody = `{"id":"p1","slug":"helpdesk","name":"Helpdesk","timezone":"Etc/UTC",
  "created_at":"2026-09-01T10:00:00Z",
  "environments":[{"id":"e1","slug":"production","name":"Production","protected":true},
                  {"id":"e2","slug":"staging","name":"Staging","protected":false}]}`

func TestProjectsCreate(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/personal/projects", 201, projectBody)

	got := h.run("projects", "create", "helpdesk", "--name", "Helpdesk")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	if body["key"] != "helpdesk" || body["name"] != "Helpdesk" {
		t.Errorf("request body = %v", body)
	}
	if !strings.Contains(got.stdout, "production*") {
		t.Errorf("stdout = %q, want the protected environment marked", got.stdout)
	}
}

func TestProjectsListJSON(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/personal/projects", 200, `{"projects":[`+projectBody+`]}`)

	got := h.run("projects", "list", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	payload := got.json(t)
	projects, ok := payload["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("payload = %v", payload)
	}
	first := projects[0].(map[string]any)
	if first["slug"] != "helpdesk" {
		t.Errorf("project = %v", first)
	}
}

func TestProjectsListEmptyStillPrintsTheHeader(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/personal/projects", 200, `{"projects":[]}`)

	got := h.run("projects", "list")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "SLUG") {
		t.Errorf("stdout = %q", got.stdout)
	}
}

// ---- 409 handling ---------------------------------------------------------

func conflictBody(resource, body string) string {
	return `{"error":{"code":"conflict","message":"already exists","details":{"` + resource + `":` + body + `}}}`
}

func TestConflictPrintsTheExistingResourceAndFails(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/personal/projects", 409, conflictBody("project", projectBody))

	got := h.run("projects", "create", "helpdesk")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1 without --idempotent", got.code)
	}
	if !strings.Contains(got.stdout, "helpdesk") {
		t.Errorf("stdout = %q, want the existing project shown so the script can go on", got.stdout)
	}
	if !strings.Contains(got.stderr, "already exists") {
		t.Errorf("stderr = %q", got.stderr)
	}
}

func TestConflictWithIdempotentSucceeds(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/personal/projects", 409, conflictBody("project", projectBody))

	got := h.run("projects", "create", "helpdesk", "--idempotent")
	if got.code != 0 {
		t.Errorf("exit = %d, want 0 with --idempotent: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "helpdesk") {
		t.Errorf("stdout = %q", got.stdout)
	}
}

func TestConflictWithIdempotentAndJSONPrintsTheResource(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/personal/projects", 409, conflictBody("project", projectBody))

	got := h.run("projects", "create", "helpdesk", "--idempotent", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	payload := got.json(t)
	if payload["slug"] != "helpdesk" || payload["id"] != "p1" {
		t.Errorf("payload = %v, want the existing project, ready to be piped onward", payload)
	}
}

func TestConflictOnUseCaseIsAlsoHandled(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases", 409,
		conflictBody("use_case", `{"id":"u1","key":"support_reply","name":"Support reply","kind":"chat",
		  "description":null,"input_schema":[],"default_params":{},"tags":[],"created_at":""}`))

	got := h.run("use-cases", "create", "support_reply", "--idempotent")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "support_reply") {
		t.Errorf("stdout = %q", got.stdout)
	}
}

// ---- use cases ------------------------------------------------------------

func TestUseCasesCreateSendsSchemaAndParams(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases", 201,
		`{"id":"u1","key":"support_reply","name":"Support reply","kind":"chat",
		  "description":null,"input_schema":[],"default_params":{},"tags":[],"created_at":""}`)

	schema := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(schema, []byte(`[{"name":"question","type":"string","required":true}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := h.run("use-cases", "create", "support_reply",
		"--kind", "chat",
		"--input-schema-file", schema,
		"--default-params", `{"temperature":0.5}`,
		"--tags", "support,ko")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}

	body := h.lastBody()
	if body["kind"] != "chat" {
		t.Errorf("kind = %v", body["kind"])
	}
	params := body["default_params"].(map[string]any)
	if params["temperature"] != 0.5 {
		t.Errorf("default_params = %v", params)
	}
	fields := body["input_schema"].([]any)
	if len(fields) != 1 || fields[0].(map[string]any)["name"] != "question" {
		t.Errorf("input_schema = %v", fields)
	}
	tags := body["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("tags = %v", tags)
	}
}

func TestUseCasesCreateRejectsAnUnknownKind(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})

	got := h.run("use-cases", "create", "x", "--kind", "vision")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if len(h.requests) != 0 {
		t.Errorf("a bad kind must be caught before any request, got %v", h.paths())
	}
}

func TestUseCasesCreateRejectsBadJSONParams(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})

	got := h.run("use-cases", "create", "x", "--default-params", "{oops}")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestUseCasesUpdateSendsOnlyWhatChanged(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply", 200,
		`{"id":"u1","key":"support_reply","name":"Renamed","kind":"chat","description":null,
		  "input_schema":[],"default_params":{},"tags":[],"created_at":""}`)

	got := h.run("use-cases", "update", "support_reply", "--name", "Renamed")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if h.requests[0].Method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", h.requests[0].Method)
	}
	body := h.lastBody()
	if len(body) != 1 || body["name"] != "Renamed" {
		t.Errorf("body = %v, want only the changed field", body)
	}
}

func TestUseCasesUpdateWithNoFlagsIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	if got := h.run("use-cases", "update", "support_reply"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestUseCasesGetShowsPromptsAndDeployments(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply", 200,
		`{"id":"u1","key":"support_reply","name":"Support reply","kind":"chat","description":null,
		  "input_schema":[{"name":"question","type":"string","required":true,"description":null}],
		  "default_params":{"temperature":0.5},"tags":[],"created_at":"2026-09-01T10:00:00Z",
		  "prompts":[{"id":"p1","name":"default","description":null,"created_at":"","version_count":2,
		    "versions":[{"id":"v2","number":2,"message":"shorter","detected_variables":[],"created_at":""}]}],
		  "deployments":[{"id":"d1","revision":3,"environment":"production","model_id":"m1",
		    "model":"openai/gpt-4o-mini","params":{"temperature":0.4},"provider_options":{},
		    "prompt_pins":{"default":"v2"},"created_at":"2026-09-02T11:00:00Z"}]}`)

	got := h.run("use-cases", "get", "support_reply")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	for _, want := range []string{"support_reply", "question", "default", "production", "openai/gpt-4o-mini"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, got.stdout)
		}
	}
}

// ---- prompts --------------------------------------------------------------

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPromptsCommitDetectsChatMessages(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/prompts/default/versions", 201,
		`{"id":"v1","prompt_id":"p1","number":1,"engine":"liquid","messages":[],"text_template":null,
		  "detected_variables":["question"],"message":"migrated","content_sha256":"x","created_at":""}`)

	file := writeTemp(t, "messages.json",
		`[{"role":"system","content":"You are a friendly support agent for Acme. Answer in two or three sentences; if you are not sure, say so and offer to escalate."},{"role":"user","content":"{{ t }}"}]`)

	got := h.run("prompts", "commit", "support_reply", "default", "--file", file, "--message", "migrated")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages = %v", body["messages"])
	}
	if _, ok := body["text_template"]; ok {
		t.Errorf("a chat commit must not send text_template: %v", body)
	}
	if body["message"] != "migrated" {
		t.Errorf("commit message = %v", body["message"])
	}
	if !strings.Contains(got.stdout, "v1") {
		t.Errorf("stdout = %q, want the new version number", got.stdout)
	}
}

func TestPromptsCommitDetectsATextTemplate(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/keywords/prompts/default/versions", 201,
		`{"id":"v1","prompt_id":"p1","number":1,"engine":"liquid","messages":null,
		  "text_template":"billing, refund","detected_variables":[],"message":null,"content_sha256":"x","created_at":""}`)

	file := writeTemp(t, "template.txt", "billing, refund, invoice")

	got := h.run("prompts", "commit", "keywords", "default", "--file", file)
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	if body["text_template"] != "billing, refund, invoice" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["messages"]; ok {
		t.Errorf("a text commit must not send messages: %v", body)
	}
}

func TestPromptsCommitTreatsLiquidAsText(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/kw/prompts/default/versions", 201,
		`{"id":"v1","prompt_id":"p1","number":1,"engine":"liquid","messages":null,
		  "text_template":"x","detected_variables":[],"message":null,"content_sha256":"x","created_at":""}`)

	// A liquid template opens with "{%", which must not be mistaken for JSON.
	file := writeTemp(t, "template.txt", "{% for t in question %}{{ t }}\n{% endfor %}")

	got := h.run("prompts", "commit", "kw", "default", "--file", file)
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if _, ok := h.lastBody()["text_template"]; !ok {
		t.Errorf("body = %v, want a text template", h.lastBody())
	}
}

func TestPromptsCommitFormatOverride(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/kw/prompts/default/versions", 201,
		`{"id":"v1","prompt_id":"p1","number":1,"engine":"raw","messages":null,
		  "text_template":"[]","detected_variables":[],"message":null,"content_sha256":"x","created_at":""}`)

	// Valid JSON that is meant as a text template.
	file := writeTemp(t, "odd.txt", `[{"role":"system","content":"hi"}]`)

	got := h.run("prompts", "commit", "kw", "default", "--file", file, "--format", "text", "--engine", "raw")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	if _, ok := body["text_template"]; !ok {
		t.Errorf("--format text must win over content sniffing: %v", body)
	}
	if body["engine"] != "raw" {
		t.Errorf("engine = %v", body["engine"])
	}
}

func TestPromptsCommitWithoutAFileIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	if got := h.run("prompts", "commit", "uc", "default"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestPromptsCommitMissingFileFails(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	got := h.run("prompts", "commit", "uc", "default", "--file", "/nope/absent.json")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
}

func TestPromptsOpen(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/prompts", 201,
		`{"id":"p2","name":"ko","description":"Korean","created_at":""}`)

	got := h.run("prompts", "open", "support_reply", "ko", "--description", "Korean")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	if body["name"] != "ko" || body["description"] != "Korean" {
		t.Errorf("body = %v", body)
	}
}

// ---- models ---------------------------------------------------------------

func TestModelsRegister(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/models", 201,
		`{"id":"m1","provider":"openrouter","model_id":"openai/gpt-4o-mini",
		  "display_name":"GPT-4o-mini","metadata":{},"provider_options":{},
		  "pricing":{"input_per_m":0.15,"output_per_m":0.6,"currency":"USD","unit":"token"},
		  "context_length":128000,"capabilities":["tools"],"status":"active","created_at":""}`)

	got := h.run("models", "register", "openai/gpt-4o-mini")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	if len(body) != 1 || body["model_id"] != "openai/gpt-4o-mini" {
		t.Errorf("body = %v, want only model_id so the server can enrich it", body)
	}
	if !strings.Contains(got.stdout, "m1") {
		t.Errorf("stdout = %q, want the catalog id a deployment will pin", got.stdout)
	}
}

func TestModelsListJSON(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/models", 200,
		`{"models":[{"id":"m1","provider":"openrouter","model_id":"openai/gpt-4o-mini",
		  "display_name":"GPT-4o-mini","metadata":{},"provider_options":{},"pricing":null,
		  "context_length":0,"capabilities":[],"status":"active","created_at":""}]}`)

	got := h.run("models", "list", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	models := got.json(t)["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %v", models)
	}
}

// ---- deploy ---------------------------------------------------------------

const deployReply = `{"id":"d1","revision":4,"environment":"production","model_id":"11111111-1111-4111-8111-111111111111",
  "model":"openai/gpt-4o-mini","params":{"temperature":0.4},"provider_options":{},
  "prompt_pins":{"default":"22222222-2222-4222-8222-222222222222"},"created_at":"2026-09-02T12:00:00Z"}`

func TestDeployWithAProviderStringSendsModel(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments", 201, deployReply)

	got := h.run("deploy", "support_reply",
		"--environment", "production",
		"--model", "openai/gpt-4o-mini",
		"--params", `{"temperature":0.4}`)
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	if body["model"] != "openai/gpt-4o-mini" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["model_id"]; ok {
		t.Errorf("a provider string must not be sent as model_id: %v", body)
	}
	if body["params"].(map[string]any)["temperature"] != 0.4 {
		t.Errorf("params = %v", body["params"])
	}
}

func TestDeployWithACatalogUUIDSendsModelID(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments", 201, deployReply)

	got := h.run("deploy", "support_reply", "--model", "11111111-1111-4111-8111-111111111111")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	if body["model_id"] != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["model"]; ok {
		t.Errorf("a UUID must not also be sent as a provider string: %v", body)
	}
}

func TestDeployResolvesVersionNumbersToUUIDs(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply", 200,
		`{"id":"u1","key":"support_reply","name":"d","kind":"chat","description":null,
		  "input_schema":[],"default_params":{},"tags":[],"created_at":"",
		  "prompts":[
		    {"id":"p1","name":"default","description":null,"created_at":"","version_count":2,
		     "versions":[{"id":"v-two","number":2,"message":null,"detected_variables":[],"created_at":""},
		                 {"id":"v-one","number":1,"message":null,"detected_variables":[],"created_at":""}]},
		    {"id":"p2","name":"ko","description":null,"created_at":"","version_count":1,
		     "versions":[{"id":"ko-three","number":3,"message":null,"detected_variables":[],"created_at":""}]}]}`)
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments", 201, deployReply)

	got := h.run("deploy", "support_reply",
		"--model", "openai/gpt-4o-mini",
		"--pin", "default=1",
		"--pin", "ko=latest")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	pins := h.lastBody()["prompt_pins"].(map[string]any)
	if pins["default"] != "v-one" {
		t.Errorf("default pin = %v, want version 1 resolved to its UUID", pins["default"])
	}
	if pins["ko"] != "ko-three" {
		t.Errorf("ko pin = %v, want the newest version", pins["ko"])
	}
}

func TestDeployWithUUIDPinsSkipsTheLookup(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments", 201, deployReply)

	got := h.run("deploy", "support_reply",
		"--model", "openai/gpt-4o-mini",
		"--pin", "default=22222222-2222-4222-8222-222222222222")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if len(h.requests) != 1 {
		t.Errorf("requests = %v, want no use-case fetch when pins are already UUIDs", h.paths())
	}
}

func TestDeployRejectsAMalformedPin(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply", 200,
		`{"id":"u1","key":"support_reply","name":"d","kind":"chat","description":null,
		  "input_schema":[],"default_params":{},"tags":[],"created_at":"","prompts":[]}`)

	got := h.run("deploy", "support_reply", "--model", "m", "--pin", "default")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "name=version") {
		t.Errorf("stderr = %q", got.stderr)
	}
}

func TestDeployRejectsAnUnknownPromptName(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply", 200,
		`{"id":"u1","key":"support_reply","name":"d","kind":"chat","description":null,
		  "input_schema":[],"default_params":{},"tags":[],"created_at":"",
		  "prompts":[{"id":"p1","name":"default","description":null,"created_at":"","version_count":1,
		    "versions":[{"id":"v1","number":1,"message":null,"detected_variables":[],"created_at":""}]}]}`)

	got := h.run("deploy", "support_reply", "--model", "m", "--pin", "jp=1")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "default") {
		t.Errorf("stderr = %q, want the available prompt names", got.stderr)
	}
}

func TestDeployWithoutAModelIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	got := h.run("deploy", "support_reply")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if len(h.requests) != 0 {
		t.Errorf("nothing should reach the server, got %v", h.paths())
	}
}

func TestDeployJSON(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments", 201, deployReply)

	got := h.run("deploy", "support_reply", "--model", "openai/gpt-4o-mini", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	payload := got.json(t)
	if payload["revision"] != float64(4) {
		t.Errorf("payload = %v", payload)
	}
}

// ---- deployments and rollback ---------------------------------------------

func TestDeploymentsListLive(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments", 200,
		`{"deployments":[`+deployReply+`]}`)

	got := h.run("deployments", "list", "support_reply")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if h.requests[0].Query != "" {
		t.Errorf("query = %q, want none without --environment", h.requests[0].Query)
	}
	if !strings.Contains(got.stdout, "production") {
		t.Errorf("stdout = %q", got.stdout)
	}
}

func TestDeploymentsListHistory(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments", 200,
		`{"deployments":[]}`)

	got := h.run("deployments", "list", "support_reply", "--environment", "staging")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if h.requests[0].Query != "environment=staging" {
		t.Errorf("query = %q", h.requests[0].Query)
	}
}

func TestRollback(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments/rollback", 200, deployReply)

	got := h.run("rollback", "support_reply", "--environment", "production", "--revision", "2")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	body := h.lastBody()
	if body["revision"] != float64(2) || body["environment"] != "production" {
		t.Errorf("body = %v", body)
	}
	if !strings.Contains(got.stdout, "restored from 2") {
		t.Errorf("stdout = %q, want it clear that a new revision was made", got.stdout)
	}
}

func TestRollbackWithoutRevisionIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	if got := h.run("rollback", "support_reply"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestRollbackSurfacesAvailableRevisions(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments/rollback", 404,
		`{"error":{"code":"not_found","message":"no such revision","details":{"available_revisions":[1,2,3]}}}`)

	got := h.run("rollback", "support_reply", "--revision", "9")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "1, 2, 3") {
		t.Errorf("stderr = %q, want the revisions that do exist", got.stderr)
	}
}

// ---- keys -----------------------------------------------------------------

const apiKeyReply = `{"id":"k1","name":"Helpdesk server","key_prefix":"ptn_helpdesk_a",
  "scopes":["read","logs"],"last_used_at":null,"created_at":"2026-09-01T10:00:00Z",
  "key":"ptn_helpdesk_a1b2c3d4"}`

func TestAPIKeysIssueShowsTheSecretOnce(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/api-keys", 201, apiKeyReply)

	got := h.run("api-keys", "issue", "--name", "Helpdesk server", "--scopes", "read,logs")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "ptn_helpdesk_a1b2c3d4") {
		t.Errorf("stdout = %q, want the raw key", got.stdout)
	}
	if !strings.Contains(got.stdout, "only time") {
		t.Errorf("stdout = %q, want the one-shot warning", got.stdout)
	}
	body := h.lastBody()
	scopes := body["scopes"].([]any)
	if len(scopes) != 2 {
		t.Errorf("scopes = %v", scopes)
	}
}

func TestAPIKeysIssueQuietPrintsOnlyTheKey(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/api-keys", 201, apiKeyReply)

	got := h.run("api-keys", "issue", "--quiet")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if strings.TrimSpace(got.stdout) != "ptn_helpdesk_a1b2c3d4" {
		t.Errorf("stdout = %q, want exactly the key for KEY=$(…)", got.stdout)
	}
}

func TestAPIKeysList(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal", Project: "helpdesk"})
	h.handle("/api/v1/orgs/personal/projects/helpdesk/api-keys", 200,
		`{"api_keys":[{"id":"k1","name":"Helpdesk server","key_prefix":"ptn_helpdesk_a",
		  "scopes":["read","logs"],"last_used_at":null,"created_at":"2026-09-01T10:00:00Z"}]}`)

	got := h.run("api-keys", "list")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "ptn_helpdesk_a…") {
		t.Errorf("stdout = %q, want the prefix marked as truncated", got.stdout)
	}
}

// ---- provider key ---------------------------------------------------------

func TestProviderKeyStatusDisconnected(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "acme"})
	h.handle("/api/v1/orgs/acme/provider-key", 200, `{"connected":false,"provider":"openrouter"}`)

	got := h.run("provider-key", "status")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "no") {
		t.Errorf("stdout = %q", got.stdout)
	}
}

func TestProviderKeySetFromFlag(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "acme"})
	h.handle("/api/v1/orgs/acme/provider-key", 201,
		`{"connected":true,"id":"pk1","provider":"openrouter","label":"default",
		  "hint":"sk-or-v1-••••4Xa2","last_used_at":null,"created_at":""}`)

	got := h.run("provider-key", "set", "--secret", "sk-or-v1-secret")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if body := h.lastBody(); body["secret"] != "sk-or-v1-secret" {
		t.Errorf("body = %v", body)
	}
	if strings.Contains(got.stdout, "sk-or-v1-secret") {
		t.Error("the secret must never be echoed back")
	}
}

func TestProviderKeySetFromEnvironment(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "acme"})
	t.Setenv("PTN_OPENROUTER_KEY", "sk-or-v1-from-env")
	h.handle("/api/v1/orgs/acme/provider-key", 201,
		`{"connected":true,"id":"pk1","provider":"openrouter","label":"default","hint":"x","created_at":""}`)

	got := h.run("provider-key", "set")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if body := h.lastBody(); body["secret"] != "sk-or-v1-from-env" {
		t.Errorf("body = %v", body)
	}
}

func TestProviderKeySetFromStdin(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "acme"})
	h.stdin = strings.NewReader("sk-or-v1-typed\n")
	h.handle("/api/v1/orgs/acme/provider-key", 201,
		`{"connected":true,"id":"pk1","provider":"openrouter","label":"default","hint":"x","created_at":""}`)

	got := h.run("provider-key", "set")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if body := h.lastBody(); body["secret"] != "sk-or-v1-typed" {
		t.Errorf("body = %v", body)
	}
}

func TestProviderKeySetWithNoSecretAnywhereIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "acme"})
	h.stdin = strings.NewReader("\n")

	got := h.run("provider-key", "set")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if len(h.requests) != 0 {
		t.Errorf("an empty secret must not be sent, got %v", h.paths())
	}
}

func TestProviderKeyConflictPointsAtTheConsole(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "acme"})
	h.handle("/api/v1/orgs/acme/provider-key", 409, conflictBody("provider_key",
		`{"connected":true,"id":"pk1","provider":"openrouter","label":"default","hint":"sk-or-v1-••••4Xa2","created_at":""}`))

	got := h.run("provider-key", "set", "--secret", "sk-or-v1-new")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "settings?tab=providers") {
		t.Errorf("stderr = %q, want the replacement path spelled out", got.stderr)
	}
}

// ---- global behaviour -----------------------------------------------------

func TestJSONOutputIsExactlyOneDocumentOnStdout(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "personal"})
	h.handle("/api/v1/orgs/personal/projects", 200, `{"projects":[`+projectBody+`]}`)

	got := h.run("projects", "list", "--json")
	dec := json.NewDecoder(strings.NewReader(got.stdout))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Errorf("stdout carries more than one document (err = %v)", err)
	}
}

func TestVersionFlag(t *testing.T) {
	h := newHarness(t)
	got := h.run("--version")
	if got.code != 0 {
		t.Errorf("exit = %d", got.code)
	}
	if !strings.Contains(got.stdout, "0.1.0") {
		t.Errorf("stdout = %q", got.stdout)
	}
}

func TestHelpExitsZero(t *testing.T) {
	h := newHarness(t)
	got := h.run("--help")
	if got.code != 0 {
		t.Errorf("exit = %d", got.code)
	}
	for _, want := range []string{"login", "deploy", "provider-key", "--json"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("help is missing %q", want)
		}
	}
}

func TestUseProjectAloneRespectsAnEnvironmentOrg(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{})
	t.Setenv("PTN_ORG", "acme")
	h.handle("/api/v1/orgs/acme/projects", 200,
		`{"projects":[{"id":"p1","slug":"helpdesk","name":"Helpdesk","timezone":"Etc/UTC","created_at":"","environments":[]}]}`)

	got := h.run("use", "--project", "helpdesk")
	if got.code != 0 {
		t.Fatalf("exit = %d: %s", got.code, got.stderr)
	}
	if saved := h.config(); saved.Project != "helpdesk" {
		t.Errorf("project = %q", saved.Project)
	}
}

func TestProviderKeySetWithClosedStdinIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.login(config.File{Org: "acme"})
	h.stdin = strings.NewReader("") // nothing piped in at all

	got := h.run("provider-key", "set")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2 — closed input is a missing value, not a read failure", got.code)
	}
	if !strings.Contains(got.stderr, "PTN_OPENROUTER_KEY") {
		t.Errorf("stderr = %q, want the ways to supply it", got.stderr)
	}
}
