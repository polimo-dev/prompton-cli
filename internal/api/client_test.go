package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/api"
)

// call records what the client actually put on the wire.
type call struct {
	Method string
	Path   string
	Raw    string
	Query  string
	Auth   string
	Body   string
}

// stub is a one-response test server. Every test asserts on the recorded call
// and on the decoding of a body copied from docs/management-api.md.
type stub struct {
	t      *testing.T
	status int
	body   string
	calls  []call
	srv    *httptest.Server
}

func newStub(t *testing.T, status int, body string) *stub {
	t.Helper()
	s := &stub{t: t, status: status, body: body}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.calls = append(s.calls, call{
			Method: r.Method,
			Path:   r.URL.Path,
			Raw:    r.RequestURI,
			Query:  r.URL.RawQuery,
			Auth:   r.Header.Get("Authorization"),
			Body:   string(raw),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		io.WriteString(w, s.body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) client(token string) *api.Client { return api.New(s.srv.URL, token) }

func (s *stub) only() call {
	s.t.Helper()
	if len(s.calls) != 1 {
		s.t.Fatalf("expected exactly 1 request, got %d", len(s.calls))
	}
	return s.calls[0]
}

func (s *stub) expect(method, path string) call {
	s.t.Helper()
	c := s.only()
	if c.Method != method || c.Path != path {
		s.t.Fatalf("request = %s %s, want %s %s", c.Method, c.Path, method, path)
	}
	return c
}

// bodyMap decodes a recorded request body.
func (c call) bodyMap(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(c.Body), &m); err != nil {
		t.Fatalf("request body is not a JSON object: %v (%s)", err, c.Body)
	}
	return m
}

func ctx() context.Context { return context.Background() }

// ---- transport basics -----------------------------------------------------

func TestRequestsCarryBearerTokenAndUserAgent(t *testing.T) {
	var gotUA, gotAccept, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		io.WriteString(w, `{"user":{"id":"u1","email":"ada@example.com"},"organizations":[]}`)
	}))
	defer srv.Close()

	c := api.New(srv.URL, "tok-123")
	if _, err := c.Me(ctx()); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if !strings.HasPrefix(gotUA, "prompton-cli/") {
		t.Errorf("User-Agent = %q, want a prompton-cli/… identifier", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotContentType != "" {
		t.Errorf("a GET must not send Content-Type, got %q", gotContentType)
	}
}

func TestURLIsHostPlusAPIV1(t *testing.T) {
	c := api.New("https://prompton.example/", "")
	if got := c.URL("/orgs"); got != "https://prompton.example/api/v1/orgs" {
		t.Errorf("URL = %q", got)
	}
}

func TestPathSegmentsAreEscaped(t *testing.T) {
	s := newStub(t, 200, `{"projects":[]}`)
	if _, err := s.client("t").ListProjects(ctx(), "weird/slug"); err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	got := s.only()
	if got.Raw != "/api/v1/orgs/weird%2Fslug/projects" {
		t.Errorf("request target = %q, want the org segment escaped", got.Raw)
	}
}

// ---- device login ---------------------------------------------------------

func TestRequestDeviceCode(t *testing.T) {
	s := newStub(t, 200, `{
	  "device_code": "7f3c…opaque…",
	  "user_code": "ABCD-EFGH",
	  "verification_uri": "https://prompton.example/device",
	  "verification_uri_complete": "https://prompton.example/device?code=ABCD-EFGH",
	  "expires_in": 900,
	  "interval": 5
	}`)

	got, err := s.client("").RequestDeviceCode(ctx(), api.DeviceCodeRequest{
		Client: "prompton-cli/0.1.0 (darwin/arm64)",
		Name:   "CLI on laptop",
	})
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}

	c := s.expect(http.MethodPost, "/api/v1/device/code")
	if c.Auth != "" {
		t.Errorf("the device-code call must be unauthenticated, got %q", c.Auth)
	}
	body := c.bodyMap(t)
	if body["client"] != "prompton-cli/0.1.0 (darwin/arm64)" || body["name"] != "CLI on laptop" {
		t.Errorf("request body = %v", body)
	}

	if got.UserCode != "ABCD-EFGH" {
		t.Errorf("UserCode = %q", got.UserCode)
	}
	if got.VerificationURIComplete != "https://prompton.example/device?code=ABCD-EFGH" {
		t.Errorf("VerificationURIComplete = %q", got.VerificationURIComplete)
	}
	if got.ExpiresIn != 900 || got.Interval != 5 {
		t.Errorf("ExpiresIn/Interval = %d/%d, want 900/5", got.ExpiresIn, got.Interval)
	}
}

func TestPollDeviceTokenSuccess(t *testing.T) {
	s := newStub(t, 200, `{
	  "token": "user-jwt",
	  "user": {"id": "0192a3b4-user", "email": "ada@example.com"},
	  "organizations": [
	    {"id": "0192-personal", "name": "Ada", "slug": null, "personal": true},
	    {"id": "0192-acme", "name": "Acme", "slug": "acme", "personal": false}
	  ]
	}`)

	got, err := s.client("").PollDeviceToken(ctx(), "device-code-value")
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	c := s.expect(http.MethodPost, "/api/v1/device/token")
	if c.Auth != "" {
		t.Errorf("the device-token call must be unauthenticated, got %q", c.Auth)
	}
	if body := c.bodyMap(t); body["device_code"] != "device-code-value" {
		t.Errorf("request body = %v", body)
	}

	if got.Token != "user-jwt" {
		t.Errorf("Token = %q", got.Token)
	}
	if got.User.Email != "ada@example.com" {
		t.Errorf("User.Email = %q", got.User.Email)
	}
	if len(got.Orgs) != 2 {
		t.Fatalf("Orgs = %d, want 2", len(got.Orgs))
	}
	if ref := got.Orgs[0].Ref(); ref != "personal" {
		t.Errorf("a null-slug personal org must address as %q, got %q", "personal", ref)
	}
	if ref := got.Orgs[1].Ref(); ref != "acme" {
		t.Errorf("a team org must address as its slug, got %q", ref)
	}
}

func TestPollDeviceTokenPendingIsATypedError(t *testing.T) {
	s := newStub(t, 400, `{"error":{"code":"authorization_pending","message":"not approved yet","details":{}}}`)
	_, err := s.client("").PollDeviceToken(ctx(), "dc")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !api.Is(err, api.CodeAuthorizationPending) {
		t.Fatalf("err = %v, want authorization_pending", err)
	}
	if !api.IsStatus(err, 400) {
		t.Errorf("status was not preserved: %v", err)
	}
}

// ---- session --------------------------------------------------------------

func TestMe(t *testing.T) {
	s := newStub(t, 200, `{
	  "user": {"id": "u1", "email": "ada@example.com"},
	  "organizations": [{"id": "o1", "name": "Ada", "slug": null, "personal": true}]
	}`)
	got, err := s.client("tok").Me(ctx())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	c := s.expect(http.MethodGet, "/api/v1/me")
	if c.Auth != "Bearer tok" {
		t.Errorf("Authorization = %q", c.Auth)
	}
	if got.User.ID != "u1" || len(got.Orgs) != 1 {
		t.Errorf("Me = %+v", got)
	}
}

func TestRevokeSession(t *testing.T) {
	s := newStub(t, 204, ``)
	if err := s.client("tok").RevokeSession(ctx()); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	c := s.expect(http.MethodPost, "/api/v1/sessions/revoke")
	if c.Auth != "Bearer tok" {
		t.Errorf("revoke must present the token it is revoking, got %q", c.Auth)
	}
}

// ---- organizations --------------------------------------------------------

func TestListOrgs(t *testing.T) {
	s := newStub(t, 200, `{"organizations":[
	  {"id":"o1","name":"Ada","slug":null,"personal":true,"created_at":"2026-09-01T09:12:03.123456Z"},
	  {"id":"o2","name":"Acme","slug":"acme","personal":false}
	]}`)
	got, err := s.client("tok").ListOrgs(ctx())
	if err != nil {
		t.Fatalf("ListOrgs: %v", err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs")
	if len(got) != 2 || got[1].Slug != "acme" {
		t.Errorf("ListOrgs = %+v", got)
	}
}

func TestListOrgsAcceptsShortEnvelopeKey(t *testing.T) {
	s := newStub(t, 200, `{"orgs":[{"id":"o1","name":"Ada","personal":true}]}`)
	got, err := s.client("tok").ListOrgs(ctx())
	if err != nil {
		t.Fatalf("ListOrgs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListOrgs = %+v, want the \"orgs\" key to be understood too", got)
	}
}

func TestGetOrg(t *testing.T) {
	s := newStub(t, 200, `{"id":"0192a3b4-…","name":"Ada","slug":null,"personal":true,
	  "created_at":"2026-09-01T09:12:03.123456Z"}`)
	got, err := s.client("tok").GetOrg(ctx(), "personal")
	if err != nil {
		t.Fatalf("GetOrg: %v", err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs/personal")
	if !got.Personal || got.Name != "Ada" {
		t.Errorf("GetOrg = %+v", got)
	}
}

// ---- projects -------------------------------------------------------------

const projectJSON = `{"id":"0192p","slug":"helpdesk","name":"Helpdesk","timezone":"Etc/UTC",
  "created_at":"2026-09-01T10:00:00Z",
  "environments":[{"id":"e1","slug":"production","name":"Production","protected":true},
                  {"id":"e2","slug":"staging","name":"Staging","protected":false}]}`

func TestListProjects(t *testing.T) {
	s := newStub(t, 200, `{"projects":[`+projectJSON+`]}`)
	got, err := s.client("tok").ListProjects(ctx(), "acme")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs/acme/projects")
	if len(got) != 1 {
		t.Fatalf("ListProjects = %+v", got)
	}
	if got[0].Slug != "helpdesk" || len(got[0].Environments) != 2 {
		t.Errorf("project = %+v", got[0])
	}
	if !got[0].Environments[0].Protected {
		t.Error("production must decode as protected")
	}
}

func TestCreateProject(t *testing.T) {
	s := newStub(t, 201, projectJSON)
	got, err := s.client("tok").CreateProject(ctx(), "personal", api.CreateProjectRequest{
		Key: "helpdesk", Name: "Helpdesk", Timezone: "Etc/UTC",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	c := s.expect(http.MethodPost, "/api/v1/orgs/personal/projects")
	body := c.bodyMap(t)
	if body["key"] != "helpdesk" || body["name"] != "Helpdesk" || body["timezone"] != "Etc/UTC" {
		t.Errorf("request body = %v", body)
	}
	if got.ID != "0192p" {
		t.Errorf("project id = %q", got.ID)
	}
}

func TestCreateProjectOmitsUnsetOptionalFields(t *testing.T) {
	s := newStub(t, 201, projectJSON)
	if _, err := s.client("tok").CreateProject(ctx(), "personal", api.CreateProjectRequest{Key: "helpdesk"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	body := s.only().bodyMap(t)
	if _, ok := body["name"]; ok {
		t.Errorf("an unset name must not be sent, body = %v", body)
	}
	if _, ok := body["timezone"]; ok {
		t.Errorf("an unset timezone must not be sent, body = %v", body)
	}
}

// ---- use cases ------------------------------------------------------------

const useCaseJSON = `{"id":"0192u","key":"support_reply","name":"Support reply",
  "description":null,"kind":"chat",
  "input_schema":[{"name":"question","type":"string","required":true,"description":null,"example":null}],
  "default_params":{"temperature":0.5},"tags":[],"created_at":"2026-09-01T10:05:00Z"}`

func TestListUseCases(t *testing.T) {
	s := newStub(t, 200, `{"use_cases":[`+useCaseJSON+`]}`)
	got, err := s.client("tok").ListUseCases(ctx(), "personal", "helpdesk")
	if err != nil {
		t.Fatalf("ListUseCases: %v", err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs/personal/projects/helpdesk/use-cases")
	if len(got) != 1 || got[0].Key != "support_reply" {
		t.Fatalf("ListUseCases = %+v", got)
	}
	if got[0].Description != nil {
		t.Error("a null description must decode as nil, not as an empty string")
	}
	if got[0].InputSchema[0].Name != "question" || !got[0].InputSchema[0].Required {
		t.Errorf("input schema = %+v", got[0].InputSchema)
	}
	if got[0].DefaultParams["temperature"] != 0.5 {
		t.Errorf("default params = %v", got[0].DefaultParams)
	}
}

func TestGetUseCaseCarriesPromptsAndDeployments(t *testing.T) {
	s := newStub(t, 200, `{"id":"0192u","key":"support_reply","name":"Support reply",
	  "kind":"chat","input_schema":[],"default_params":{},"tags":[],"created_at":"2026-09-01T10:05:00Z",
	  "description":null,
	  "prompts":[
	    {"id":"p1","name":"default","description":null,"created_at":"2026-09-01T10:06:00Z",
	     "version_count":2,
	     "versions":[{"id":"v2","number":2,"message":"shorter","detected_variables":["question"],"created_at":"2026-09-02T…"},
	                 {"id":"v1","number":1,"message":"migrated from the app","detected_variables":["question"],"created_at":"2026-09-01T…"}]},
	    {"id":"p2","name":"ko","description":"Korean","created_at":"2026-09-01T10:07:00Z","version_count":0,"versions":[]}
	  ],
	  "deployments":[
	    {"id":"d1","revision":3,"environment":"production","model_id":"m-uuid",
	     "model":"openai/gpt-4o-mini","params":{"temperature":0.4},
	     "provider_options":{"only":["OpenAI"]},
	     "prompt_pins":{"default":"v2","ko":"v9"},"created_at":"2026-09-02T…"}
	  ]}`)

	got, err := s.client("tok").GetUseCase(ctx(), "personal", "helpdesk", "support_reply")
	if err != nil {
		t.Fatalf("GetUseCase: %v", err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply")
	if len(got.Prompts) != 2 {
		t.Fatalf("prompts = %d, want 2", len(got.Prompts))
	}
	if got.Prompts[0].VersionCount != 2 || got.Prompts[0].Versions[0].Number != 2 {
		t.Errorf("prompt = %+v", got.Prompts[0])
	}
	if len(got.Deployments) != 1 || got.Deployments[0].PromptPins["default"] != "v2" {
		t.Errorf("deployments = %+v", got.Deployments)
	}
}

func TestCreateUseCase(t *testing.T) {
	s := newStub(t, 201, useCaseJSON)
	_, err := s.client("tok").CreateUseCase(ctx(), "personal", "helpdesk", api.CreateUseCaseRequest{
		Key:  "support_reply",
		Kind: "chat",
		Name: "Support reply",
		InputSchema: []api.InputField{
			{Name: "question", Type: "string", Required: true},
		},
		DefaultParams: map[string]any{"temperature": 0.5},
	})
	if err != nil {
		t.Fatalf("CreateUseCase: %v", err)
	}
	c := s.expect(http.MethodPost, "/api/v1/orgs/personal/projects/helpdesk/use-cases")
	body := c.bodyMap(t)
	if body["key"] != "support_reply" || body["kind"] != "chat" {
		t.Errorf("request body = %v", body)
	}
	schema, ok := body["input_schema"].([]any)
	if !ok || len(schema) != 1 {
		t.Fatalf("input_schema = %v", body["input_schema"])
	}
	field := schema[0].(map[string]any)
	if field["name"] != "question" || field["required"] != true {
		t.Errorf("input_schema field = %v", field)
	}
}

func TestUpdateUseCaseSendsOnlyTheFieldsGiven(t *testing.T) {
	s := newStub(t, 200, useCaseJSON)
	name := "Renamed"
	_, err := s.client("tok").UpdateUseCase(ctx(), "personal", "helpdesk", "support_reply",
		api.UpdateUseCaseRequest{Name: &name})
	if err != nil {
		t.Fatalf("UpdateUseCase: %v", err)
	}
	c := s.expect(http.MethodPatch, "/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply")
	body := c.bodyMap(t)
	if len(body) != 1 || body["name"] != "Renamed" {
		t.Errorf("a PATCH must carry only the changed fields, got %v", body)
	}
}

func TestUpdateUseCaseCanClearAField(t *testing.T) {
	s := newStub(t, 200, useCaseJSON)
	empty := []string{}
	_, err := s.client("tok").UpdateUseCase(ctx(), "personal", "helpdesk", "k",
		api.UpdateUseCaseRequest{Tags: &empty})
	if err != nil {
		t.Fatalf("UpdateUseCase: %v", err)
	}
	body := s.only().bodyMap(t)
	tags, ok := body["tags"].([]any)
	if !ok || len(tags) != 0 {
		t.Errorf("clearing tags must send an empty array, got %v", body)
	}
}

func TestUpdateUseCaseEmpty(t *testing.T) {
	if !(api.UpdateUseCaseRequest{}).Empty() {
		t.Error("a request with no fields must report Empty")
	}
	name := "x"
	if (api.UpdateUseCaseRequest{Name: &name}).Empty() {
		t.Error("a request with a name must not report Empty")
	}
}

// ---- prompts --------------------------------------------------------------

func TestCreatePrompt(t *testing.T) {
	s := newStub(t, 201, `{"id":"p2","name":"ko","description":"Korean","created_at":"2026-09-01T…"}`)
	got, err := s.client("tok").CreatePrompt(ctx(), "personal", "helpdesk", "support_reply",
		api.CreatePromptRequest{Name: "ko", Description: "Korean"})
	if err != nil {
		t.Fatalf("CreatePrompt: %v", err)
	}
	s.expect(http.MethodPost, "/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/prompts")
	if got.Name != "ko" {
		t.Errorf("prompt = %+v", got)
	}
}

func TestCommitVersionChat(t *testing.T) {
	s := newStub(t, 201, `{"id":"v1","prompt_id":"p1","number":1,"engine":"liquid",
	  "messages":[{"role":"system","content":"You are a friendly support agent for Acme. Answer in two or three sentences; if you are not sure, say so and offer to escalate."},
	              {"role":"user","content":"{{ t }}"}],
	  "text_template":null,"detected_variables":["question"],
	  "message":"migrated from the app's hardcoded prompt",
	  "content_sha256":"abc","created_at":"2026-09-01T…"}`)

	got, err := s.client("tok").CommitVersion(ctx(), "personal", "helpdesk", "support_reply", "default",
		api.CommitVersionRequest{
			Messages: []api.Message{{Role: "system", Content: "You are a friendly support agent for Acme. Answer in two or three sentences; if you are not sure, say so and offer to escalate."}},
			Message:  "migrated",
		})
	if err != nil {
		t.Fatalf("CommitVersion: %v", err)
	}
	c := s.expect(http.MethodPost,
		"/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/prompts/default/versions")
	body := c.bodyMap(t)
	if _, ok := body["text_template"]; ok {
		t.Errorf("a chat commit must not send text_template, body = %v", body)
	}
	if got.Number != 1 || got.TextTemplate != nil {
		t.Errorf("version = %+v", got)
	}
	if len(got.DetectedVariables) != 1 {
		t.Errorf("detected_variables = %v", got.DetectedVariables)
	}
}

func TestCommitVersionText(t *testing.T) {
	s := newStub(t, 201, `{"id":"v1","prompt_id":"p1","number":1,"engine":"liquid",
	  "messages":null,"text_template":"billing, refund","detected_variables":[],
	  "message":null,"content_sha256":"abc","created_at":"2026-09-01T…"}`)

	got, err := s.client("tok").CommitVersion(ctx(), "personal", "helpdesk", "kw", "default",
		api.CommitVersionRequest{TextTemplate: "billing, refund"})
	if err != nil {
		t.Fatalf("CommitVersion: %v", err)
	}
	body := s.only().bodyMap(t)
	if body["text_template"] != "billing, refund" {
		t.Errorf("request body = %v", body)
	}
	if _, ok := body["messages"]; ok {
		t.Errorf("a text commit must not send messages, body = %v", body)
	}
	if got.TextTemplate == nil || *got.TextTemplate != "billing, refund" {
		t.Errorf("text_template = %v", got.TextTemplate)
	}
	if got.Message != nil {
		t.Error("a null commit message must decode as nil")
	}
}

// ---- models ---------------------------------------------------------------

const modelJSON = `{"id":"0192m","provider":"openrouter","model_id":"openai/gpt-4o-mini",
  "display_name":"GPT-4o-mini","metadata":{},"provider_options":{"only":["OpenAI"]},
  "pricing":{"input_per_m":0.15,"output_per_m":0.6,"currency":"USD","unit":"token"},
  "context_length":128000,"capabilities":["tools","streaming"],"status":"active",
  "created_at":"2026-09-01T…"}`

func TestListModels(t *testing.T) {
	s := newStub(t, 200, `{"models":[`+modelJSON+`]}`)
	got, err := s.client("tok").ListModels(ctx(), "personal", "helpdesk")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs/personal/projects/helpdesk/models")
	if len(got) != 1 {
		t.Fatalf("models = %+v", got)
	}
	m := got[0]
	if m.ID != "0192m" || m.ModelID != "openai/gpt-4o-mini" {
		t.Errorf("the catalog UUID and the provider string must not be confused: %+v", m)
	}
	if m.Pricing == nil || m.Pricing.InputPerM != 0.15 || m.Pricing.OutputPerM != 0.6 {
		t.Errorf("pricing = %+v", m.Pricing)
	}
	if m.ContextLength != 128000 || len(m.Capabilities) != 2 {
		t.Errorf("model = %+v", m)
	}
}

func TestRegisterModelSendsOnlyModelIDWhenNothingElseIsGiven(t *testing.T) {
	s := newStub(t, 201, modelJSON)
	got, err := s.client("tok").RegisterModel(ctx(), "personal", "helpdesk",
		api.RegisterModelRequest{ModelID: "openai/gpt-4o-mini"})
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}
	c := s.expect(http.MethodPost, "/api/v1/orgs/personal/projects/helpdesk/models")
	body := c.bodyMap(t)
	if len(body) != 1 || body["model_id"] != "openai/gpt-4o-mini" {
		t.Errorf("request body = %v, want only model_id so the server can fill the rest", body)
	}
	if got.DisplayName != "GPT-4o-mini" {
		t.Errorf("display name = %q", got.DisplayName)
	}
}

// ---- deployments ----------------------------------------------------------

const deploymentJSON = `{"id":"0192d","revision":3,"environment":"production",
  "model_id":"0192m","model":"openai/gpt-4o-mini","params":{"temperature":0.4},
  "provider_options":{"allow_fallbacks":false},
  "prompt_pins":{"default":"v2","ko":"v7"},"created_at":"2026-09-02T11:00:00Z"}`

func TestListDeploymentsLive(t *testing.T) {
	s := newStub(t, 200, `{"deployments":[`+deploymentJSON+`]}`)
	got, err := s.client("tok").ListDeployments(ctx(), "personal", "helpdesk", "support_reply", "")
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	c := s.expect(http.MethodGet, "/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments")
	if c.Query != "" {
		t.Errorf("without --environment there must be no query string, got %q", c.Query)
	}
	if len(got) != 1 || got[0].Revision != 3 || got[0].PromptPins["ko"] != "v7" {
		t.Errorf("deployments = %+v", got)
	}
}

func TestListDeploymentsHistoryPassesEnvironment(t *testing.T) {
	s := newStub(t, 200, `{"deployments":[]}`)
	if _, err := s.client("tok").ListDeployments(ctx(), "personal", "helpdesk", "uc", "staging"); err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if q := s.only().Query; q != "environment=staging" {
		t.Errorf("query = %q, want environment=staging", q)
	}
}

func TestCreateDeploymentWithCatalogUUID(t *testing.T) {
	s := newStub(t, 201, deploymentJSON)
	_, err := s.client("tok").CreateDeployment(ctx(), "personal", "helpdesk", "support_reply",
		api.CreateDeploymentRequest{
			Environment: "production",
			ModelID:     "0192m",
			PromptPins:  map[string]string{"default": "v2"},
			Params:      map[string]any{"temperature": 0.4},
		})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	c := s.expect(http.MethodPost, "/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments")
	body := c.bodyMap(t)
	if body["model_id"] != "0192m" {
		t.Errorf("request body = %v", body)
	}
	if _, ok := body["model"]; ok {
		t.Errorf("only one of model_id/model may be sent, body = %v", body)
	}
}

func TestCreateDeploymentWithProviderString(t *testing.T) {
	s := newStub(t, 201, deploymentJSON)
	_, err := s.client("tok").CreateDeployment(ctx(), "personal", "helpdesk", "uc",
		api.CreateDeploymentRequest{Model: "openai/gpt-4o-mini"})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	body := s.only().bodyMap(t)
	if body["model"] != "openai/gpt-4o-mini" {
		t.Errorf("request body = %v", body)
	}
	if _, ok := body["prompt_pins"]; ok {
		t.Errorf("omitted pins must stay omitted so the server pins the newest versions, body = %v", body)
	}
}

func TestRollback(t *testing.T) {
	s := newStub(t, 200, `{"id":"0192d","revision":4,"environment":"production","model_id":"0192m",
	  "model":"openai/gpt-4o-mini","params":{},"provider_options":{},
	  "prompt_pins":{"default":"v1"},"created_at":"2026-09-02T12:00:00Z"}`)

	got, err := s.client("tok").Rollback(ctx(), "personal", "helpdesk", "support_reply",
		api.RollbackRequest{Environment: "production", Revision: 1})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	c := s.expect(http.MethodPost,
		"/api/v1/orgs/personal/projects/helpdesk/use-cases/support_reply/deployments/rollback")
	body := c.bodyMap(t)
	if body["revision"] != float64(1) {
		t.Errorf("request body = %v", body)
	}
	if got.Revision != 4 {
		t.Errorf("rolling back must produce a new higher revision, got %d", got.Revision)
	}
}

// ---- keys -----------------------------------------------------------------

func TestIssueAPIKeyReturnsTheSecretOnce(t *testing.T) {
	s := newStub(t, 201, `{"id":"k1","name":"Helpdesk server","key_prefix":"ptn_helpdesk_a",
	  "scopes":["resolve","logs"],"last_used_at":null,"created_at":"2026-09-01T…",
	  "key":"ptn_helpdesk_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6"}`)

	got, err := s.client("tok").IssueAPIKey(ctx(), "personal", "helpdesk",
		api.IssueAPIKeyRequest{Name: "Helpdesk server", Scopes: []string{"resolve", "logs"}})
	if err != nil {
		t.Fatalf("IssueAPIKey: %v", err)
	}
	s.expect(http.MethodPost, "/api/v1/orgs/personal/projects/helpdesk/api-keys")
	if got.Key == "" {
		t.Fatal("the raw key must be decoded from the create response")
	}
	if got.LastUsedAt != nil {
		t.Error("a null last_used_at must decode as nil")
	}
}

func TestListAPIKeysHasNoSecret(t *testing.T) {
	s := newStub(t, 200, `{"api_keys":[{"id":"k1","name":"Helpdesk server","key_prefix":"ptn_helpdesk_a",
	  "scopes":["resolve","logs"],"last_used_at":"2026-09-02T09:00:00Z","created_at":"2026-09-01T…"}]}`)
	got, err := s.client("tok").ListAPIKeys(ctx(), "personal", "helpdesk")
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs/personal/projects/helpdesk/api-keys")
	if len(got) != 1 || got[0].Key != "" {
		t.Errorf("api keys = %+v", got)
	}
	if got[0].LastUsedAt == nil || *got[0].LastUsedAt == "" {
		t.Errorf("last_used_at = %v", got[0].LastUsedAt)
	}
}

func TestGetProviderKeyDisconnected(t *testing.T) {
	s := newStub(t, 200, `{"connected":false,"provider":"openrouter"}`)
	got, err := s.client("tok").GetProviderKey(ctx(), "acme")
	if err != nil {
		t.Fatalf("GetProviderKey: %v", err)
	}
	s.expect(http.MethodGet, "/api/v1/orgs/acme/provider-key")
	if got.Connected {
		t.Errorf("provider key = %+v", got)
	}
}

func TestSetProviderKey(t *testing.T) {
	s := newStub(t, 201, `{"connected":true,"id":"pk1","provider":"openrouter","label":"default",
	  "hint":"sk-or-v1-••••4Xa2","last_used_at":null,"created_at":"2026-09-02T…"}`)
	got, err := s.client("tok").SetProviderKey(ctx(), "acme",
		api.SetProviderKeyRequest{Secret: "sk-or-v1-secret", Label: "default"})
	if err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
	c := s.expect(http.MethodPost, "/api/v1/orgs/acme/provider-key")
	if body := c.bodyMap(t); body["secret"] != "sk-or-v1-secret" {
		t.Errorf("request body = %v", body)
	}
	if !got.Connected || got.Hint != "sk-or-v1-••••4Xa2" {
		t.Errorf("provider key = %+v", got)
	}
}

// ---- error envelope -------------------------------------------------------

func TestUnauthorizedEnvelope(t *testing.T) {
	s := newStub(t, 401, `{"error":{"code":"unauthorized","message":"token is revoked or expired","details":{}}}`)
	_, err := s.client("stale").Me(ctx())
	if !api.Is(err, api.CodeUnauthorized) {
		t.Fatalf("err = %v, want unauthorized", err)
	}
	if got := err.Error(); got != "unauthorized: token is revoked or expired" {
		t.Errorf("Error() = %q", got)
	}
}

func TestNotFoundHidesOtherOrgs(t *testing.T) {
	s := newStub(t, 404, `{"error":{"code":"not_found","message":"no such organization","details":{}}}`)
	_, err := s.client("tok").ListProjects(ctx(), "someone-elses-org")
	if !api.Is(err, api.CodeNotFound) {
		t.Fatalf("cross-org access must surface as not_found, got %v", err)
	}
}

func TestInvalidRequestHintListsFieldErrors(t *testing.T) {
	s := newStub(t, 400, `{"error":{"code":"invalid_request","message":"key is invalid",
	  "details":{"errors":[{"field":"key","message":"must be lowercase"},
	                       {"field":"kind","message":"is not supported"}]}}}`)
	_, err := s.client("tok").CreateProject(ctx(), "personal", api.CreateProjectRequest{Key: "Bad Key"})
	apiErr, ok := api.AsError(err)
	if !ok {
		t.Fatalf("err = %v", err)
	}
	hint := apiErr.Hint()
	if !strings.Contains(hint, "key: must be lowercase") || !strings.Contains(hint, "kind: is not supported") {
		t.Errorf("Hint() = %q", hint)
	}
}

func TestNotFoundHintListsAvailableRevisions(t *testing.T) {
	s := newStub(t, 404, `{"error":{"code":"not_found","message":"no such revision",
	  "details":{"available_revisions":[1,2,3]}}}`)
	_, err := s.client("tok").Rollback(ctx(), "personal", "helpdesk", "uc", api.RollbackRequest{Revision: 9})
	apiErr, _ := api.AsError(err)
	if hint := apiErr.Hint(); !strings.Contains(hint, "1, 2, 3") {
		t.Errorf("Hint() = %q, want the available revisions", hint)
	}
}

func TestNotFoundHintListsAvailablePrompts(t *testing.T) {
	s := newStub(t, 404, `{"error":{"code":"not_found","message":"no such prompt",
	  "details":{"available_prompts":["default","ko"]}}}`)
	_, err := s.client("tok").CommitVersion(ctx(), "personal", "helpdesk", "uc", "jp",
		api.CommitVersionRequest{TextTemplate: "x"})
	apiErr, _ := api.AsError(err)
	if hint := apiErr.Hint(); !strings.Contains(hint, "default, ko") {
		t.Errorf("Hint() = %q, want the prompt names", hint)
	}
}

func TestConflictCarriesTheExistingResource(t *testing.T) {
	s := newStub(t, 409, `{"error":{"code":"conflict",
	  "message":"a project with key helpdesk already exists",
	  "details":{"project":`+projectJSON+`}}}`)

	_, err := s.client("tok").CreateProject(ctx(), "personal", api.CreateProjectRequest{Key: "helpdesk"})
	apiErr, ok := api.AsError(err)
	if !ok || apiErr.Code != api.CodeConflict {
		t.Fatalf("err = %v, want a conflict", err)
	}
	name, raw, ok := apiErr.Conflict()
	if !ok {
		t.Fatal("the conflict must expose the existing resource")
	}
	if name != "project" {
		t.Errorf("resource key = %q", name)
	}
	var project api.Project
	if err := json.Unmarshal(raw, &project); err != nil {
		t.Fatalf("existing resource does not decode: %v", err)
	}
	if project.Slug != "helpdesk" || len(project.Environments) != 2 {
		t.Errorf("existing project = %+v", project)
	}
}

func TestConflictWithoutDetailsIsStillAConflict(t *testing.T) {
	s := newStub(t, 409, `{"error":{"code":"conflict","message":"already exists","details":{}}}`)
	_, err := s.client("tok").CreateProject(ctx(), "personal", api.CreateProjectRequest{Key: "x"})
	apiErr, _ := api.AsError(err)
	if _, _, ok := apiErr.Conflict(); ok {
		t.Error("an empty details object carries no resource")
	}
	if apiErr.Code != api.CodeConflict {
		t.Errorf("code = %q", apiErr.Code)
	}
}

func TestNonJSONErrorBodyIsPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		io.WriteString(w, "<html>bad gateway</html>")
	}))
	defer srv.Close()

	_, err := api.New(srv.URL, "tok").Me(ctx())
	apiErr, ok := api.AsError(err)
	if !ok {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Status != 502 || !strings.Contains(apiErr.Message, "bad gateway") {
		t.Errorf("error = %+v", apiErr)
	}
	if got := apiErr.Error(); !strings.HasPrefix(got, "http_502") {
		t.Errorf("Error() = %q, want a synthesised code", got)
	}
}

func TestNetworkFailureIsNotAnAPIError(t *testing.T) {
	c := api.New("http://127.0.0.1:1", "tok")
	_, err := c.Me(ctx())
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if _, ok := api.AsError(err); ok {
		t.Error("a connection failure must not masquerade as an API error envelope")
	}
}
