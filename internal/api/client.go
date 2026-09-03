// Package api is the typed client for the PromptOn management API.
//
// Authentication is a user token ("CLI session"): every call runs with the
// signed-in user as the actor, so authorization is exactly that user's
// existing org and project membership. Management routes are scoped by org:
// /api/v1/orgs/:org/… where :org is a team slug or the literal "personal".
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/polimo-dev/prompton-cli/internal/meta"
)

// maxErrorBody caps how much of a non-JSON error body is kept in a message,
// so a proxy's HTML page cannot flood the terminal.
const maxErrorBody = 512

// Client talks to one PromptOn host.
type Client struct {
	Host      string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

// New returns a client for host, authenticating with token. An empty token is
// fine for the two unauthenticated device endpoints.
func New(host, token string) *Client {
	return &Client{
		Host:      strings.TrimRight(host, "/"),
		Token:     token,
		UserAgent: meta.UserAgent(),
		HTTP:      &http.Client{Timeout: 30 * time.Second},
	}
}

// URL builds an absolute API URL from already-escaped path segments.
func (c *Client) URL(path string) string { return c.Host + "/api/v1" + path }

// seg escapes one path segment.
func seg(s string) string { return url.PathEscape(s) }

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, c.URL(path), err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return decodeError(resp.StatusCode, raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response from %s %s: %w", method, path, err)
	}
	return nil
}

func decodeError(status int, raw []byte) error {
	var envelope struct {
		Error struct {
			Code    string                     `json:"code"`
			Message string                     `json:"message"`
			Details map[string]json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Error.Code != "" {
		return &Error{
			Status:  status,
			Code:    envelope.Error.Code,
			Message: envelope.Error.Message,
			Details: envelope.Error.Details,
		}
	}
	msg := strings.TrimSpace(string(raw))
	if len(msg) > maxErrorBody {
		msg = msg[:maxErrorBody] + "…"
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &Error{Status: status, Message: msg}
}

// ---- device login (unauthenticated) ---------------------------------------

// RequestDeviceCode starts a device authorization.
func (c *Client) RequestDeviceCode(ctx context.Context, req DeviceCodeRequest) (*DeviceCode, error) {
	var out DeviceCode
	if err := c.do(ctx, http.MethodPost, "/device/code", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PollDeviceToken exchanges a device code for a CLI session token. While the
// user has not decided yet the server answers 400 authorization_pending, and
// 400 slow_down when polled too fast; both come back as *Error.
func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (*DeviceToken, error) {
	var out DeviceToken
	body := map[string]string{"device_code": deviceCode}
	if err := c.do(ctx, http.MethodPost, "/device/token", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- session --------------------------------------------------------------

// Me returns the caller and the orgs they belong to.
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var out Me
	if err := c.do(ctx, http.MethodGet, "/me", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeSession revokes the token this client is using. It is the logout half
// of the device flow: the credential stops working server-side, not just here.
func (c *Client) RevokeSession(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/sessions/revoke", struct{}{}, nil)
}

// ---- organizations --------------------------------------------------------

// ListOrgs returns every org the caller belongs to.
func (c *Client) ListOrgs(ctx context.Context) ([]Org, error) {
	// The list envelope key is "organizations", matching the device-token and
	// /me payloads; "orgs" is accepted too so a server that shortens it does
	// not break the CLI.
	var out struct {
		Organizations []Org `json:"organizations"`
		Orgs          []Org `json:"orgs"`
	}
	if err := c.do(ctx, http.MethodGet, "/orgs", nil, &out); err != nil {
		return nil, err
	}
	if out.Organizations != nil {
		return out.Organizations, nil
	}
	return out.Orgs, nil
}

// GetOrg returns one org by slug (or the literal "personal").
func (c *Client) GetOrg(ctx context.Context, org string) (*Org, error) {
	var out Org
	if err := c.do(ctx, http.MethodGet, "/orgs/"+seg(org), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- projects -------------------------------------------------------------

// ListProjects returns the org's non-archived projects.
func (c *Client) ListProjects(ctx context.Context, org string) ([]Project, error) {
	var out projectsEnvelope
	if err := c.do(ctx, http.MethodGet, "/orgs/"+seg(org)+"/projects", nil, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

// CreateProject creates a project and its production/staging environments.
func (c *Client) CreateProject(ctx context.Context, org string, req CreateProjectRequest) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodPost, "/orgs/"+seg(org)+"/projects", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- use cases ------------------------------------------------------------

func useCasesPath(org, project string) string {
	return "/orgs/" + seg(org) + "/projects/" + seg(project) + "/use-cases"
}

// ListUseCases returns the project's use cases.
func (c *Client) ListUseCases(ctx context.Context, org, project string) ([]UseCase, error) {
	var out useCasesEnvelope
	if err := c.do(ctx, http.MethodGet, useCasesPath(org, project), nil, &out); err != nil {
		return nil, err
	}
	return out.UseCases, nil
}

// GetUseCase returns one use case with its prompts and live deployments.
func (c *Client) GetUseCase(ctx context.Context, org, project, key string) (*UseCase, error) {
	var out UseCase
	if err := c.do(ctx, http.MethodGet, useCasesPath(org, project)+"/"+seg(key), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateUseCase creates a use case. For kind chat/text a "default" prompt is
// born with it.
func (c *Client) CreateUseCase(ctx context.Context, org, project string, req CreateUseCaseRequest) (*UseCase, error) {
	var out UseCase
	if err := c.do(ctx, http.MethodPost, useCasesPath(org, project), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateUseCase patches the fields that are present. Key and kind cannot
// change — they are the app's contract.
func (c *Client) UpdateUseCase(ctx context.Context, org, project, key string, req UpdateUseCaseRequest) (*UseCase, error) {
	var out UseCase
	if err := c.do(ctx, http.MethodPatch, useCasesPath(org, project)+"/"+seg(key), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- prompts --------------------------------------------------------------

// CreatePrompt opens a new prompt name under a use case.
func (c *Client) CreatePrompt(ctx context.Context, org, project, useCase string, req CreatePromptRequest) (*Prompt, error) {
	var out Prompt
	path := useCasesPath(org, project) + "/" + seg(useCase) + "/prompts"
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CommitVersion commits an immutable version of a named prompt.
func (c *Client) CommitVersion(ctx context.Context, org, project, useCase, name string, req CommitVersionRequest) (*PromptVersion, error) {
	var out PromptVersion
	path := useCasesPath(org, project) + "/" + seg(useCase) + "/prompts/" + seg(name) + "/versions"
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- models ---------------------------------------------------------------

func modelsPath(org, project string) string {
	return "/orgs/" + seg(org) + "/projects/" + seg(project) + "/models"
}

// ListModels returns the project's model catalog.
func (c *Client) ListModels(ctx context.Context, org, project string) ([]Model, error) {
	var out modelsEnvelope
	if err := c.do(ctx, http.MethodGet, modelsPath(org, project), nil, &out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

// RegisterModel adds a provider model to the catalog.
func (c *Client) RegisterModel(ctx context.Context, org, project string, req RegisterModelRequest) (*Model, error) {
	var out Model
	if err := c.do(ctx, http.MethodPost, modelsPath(org, project), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- deployments ----------------------------------------------------------

func deploymentsPath(org, project, useCase string) string {
	return useCasesPath(org, project) + "/" + seg(useCase) + "/deployments"
}

// ListDeployments returns one live revision per environment. With environment
// set it returns every revision of that environment instead, newest first.
func (c *Client) ListDeployments(ctx context.Context, org, project, useCase, environment string) ([]Deployment, error) {
	path := deploymentsPath(org, project, useCase)
	if environment != "" {
		path += "?environment=" + url.QueryEscape(environment)
	}
	var out deploymentsEnvelope
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Deployments, nil
}

// CreateDeployment commits a new revision.
func (c *Client) CreateDeployment(ctx context.Context, org, project, useCase string, req CreateDeploymentRequest) (*Deployment, error) {
	var out Deployment
	if err := c.do(ctx, http.MethodPost, deploymentsPath(org, project, useCase), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Rollback re-commits a past revision, which produces a new higher-numbered
// revision rather than rewinding history.
func (c *Client) Rollback(ctx context.Context, org, project, useCase string, req RollbackRequest) (*Deployment, error) {
	var out Deployment
	path := deploymentsPath(org, project, useCase) + "/rollback"
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- keys -----------------------------------------------------------------

func apiKeysPath(org, project string) string {
	return "/orgs/" + seg(org) + "/projects/" + seg(project) + "/api-keys"
}

// ListAPIKeys returns the project's live runtime keys (secrets not included).
func (c *Client) ListAPIKeys(ctx context.Context, org, project string) ([]APIKey, error) {
	var out apiKeysEnvelope
	if err := c.do(ctx, http.MethodGet, apiKeysPath(org, project), nil, &out); err != nil {
		return nil, err
	}
	return out.APIKeys, nil
}

// IssueAPIKey mints a runtime key. The raw secret is in the response and
// nowhere else, ever again.
func (c *Client) IssueAPIKey(ctx context.Context, org, project string, req IssueAPIKeyRequest) (*APIKey, error) {
	var out APIKey
	if err := c.do(ctx, http.MethodPost, apiKeysPath(org, project), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProviderKey reports whether the org has a BYOK provider key.
func (c *Client) GetProviderKey(ctx context.Context, org string) (*ProviderKey, error) {
	var out ProviderKey
	if err := c.do(ctx, http.MethodGet, "/orgs/"+seg(org)+"/provider-key", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetProviderKey stores the org's BYOK provider key.
func (c *Client) SetProviderKey(ctx context.Context, org string, req SetProviderKeyRequest) (*ProviderKey, error) {
	var out ProviderKey
	if err := c.do(ctx, http.MethodPost, "/orgs/"+seg(org)+"/provider-key", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
