package api

import "encoding/json"

// ---- identity -------------------------------------------------------------

// User is the authenticated human behind the CLI session token.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// Org is one organization the user belongs to. A personal org has no slug and
// is addressed in paths by the literal "personal".
type Org struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug,omitempty"`
	Personal  bool   `json:"personal"`
	CreatedAt string `json:"created_at,omitempty"`
}

// Ref is the :org path segment for this org.
func (o Org) Ref() string {
	if o.Personal || o.Slug == "" {
		return "personal"
	}
	return o.Slug
}

// Me is the payload of GET /api/v1/me.
type Me struct {
	User User  `json:"user"`
	Orgs []Org `json:"organizations"`
}

// ---- device login ---------------------------------------------------------

// DeviceCodeRequest starts a device authorization.
type DeviceCodeRequest struct {
	Client string `json:"client"`
	Name   string `json:"name"`
}

// DeviceCode is the response of POST /api/v1/device/code.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// DeviceToken is the successful response of POST /api/v1/device/token. The
// token is returned exactly once; later polls answer expired_token.
type DeviceToken struct {
	Token string `json:"token"`
	User  User   `json:"user"`
	Orgs  []Org  `json:"organizations"`
}

// ---- projects -------------------------------------------------------------

// Environment is one deploy target inside a project.
type Environment struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// Project is one project inside an org.
type Project struct {
	ID           string        `json:"id"`
	Slug         string        `json:"slug"`
	Name         string        `json:"name"`
	Timezone     string        `json:"timezone"`
	CreatedAt    string        `json:"created_at"`
	Environments []Environment `json:"environments"`
}

// CreateProjectRequest is the body of POST /projects.
type CreateProjectRequest struct {
	Key      string `json:"key"`
	Name     string `json:"name,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// ---- use cases ------------------------------------------------------------

// InputField is one declared input variable of a use case.
type InputField struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Required    bool    `json:"required"`
	Description *string `json:"description,omitempty"`
	Example     any     `json:"example,omitempty"`
}

// UseCase is one call site. GET of a single use case also carries its prompts
// and live deployments.
type UseCase struct {
	ID            string         `json:"id"`
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	Description   *string        `json:"description"`
	Kind          string         `json:"kind"`
	InputSchema   []InputField   `json:"input_schema"`
	DefaultParams map[string]any `json:"default_params"`
	Tags          []string       `json:"tags"`
	CreatedAt     string         `json:"created_at"`

	Prompts     []Prompt     `json:"prompts,omitempty"`
	Deployments []Deployment `json:"deployments,omitempty"`
}

// CreateUseCaseRequest is the body of POST /use-cases.
type CreateUseCaseRequest struct {
	Key           string         `json:"key"`
	Name          string         `json:"name,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	Description   string         `json:"description,omitempty"`
	InputSchema   []InputField   `json:"input_schema,omitempty"`
	DefaultParams map[string]any `json:"default_params,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
}

// UpdateUseCaseRequest is the body of PATCH /use-cases/:key. Only the fields
// that are present are changed, so every field is a pointer: a nil pointer is
// "leave it alone", and a pointer to a zero value is "set it to that".
type UpdateUseCaseRequest struct {
	Name          *string         `json:"name,omitempty"`
	Description   *string         `json:"description,omitempty"`
	Tags          *[]string       `json:"tags,omitempty"`
	InputSchema   *[]InputField   `json:"input_schema,omitempty"`
	DefaultParams *map[string]any `json:"default_params,omitempty"`
}

// Empty reports whether the patch would change nothing.
func (r UpdateUseCaseRequest) Empty() bool {
	return r.Name == nil && r.Description == nil && r.Tags == nil &&
		r.InputSchema == nil && r.DefaultParams == nil
}

// ---- prompts --------------------------------------------------------------

// VersionSummary is one immutable prompt version as it appears in a listing.
type VersionSummary struct {
	ID                string   `json:"id"`
	Number            int      `json:"number"`
	Message           *string  `json:"message"`
	DetectedVariables []string `json:"detected_variables"`
	CreatedAt         string   `json:"created_at"`
}

// Prompt is one named prompt of a use case ("default", "ko", …).
type Prompt struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  *string          `json:"description"`
	CreatedAt    string           `json:"created_at"`
	VersionCount int              `json:"version_count"`
	Versions     []VersionSummary `json:"versions"`
}

// Message is one chat message in a prompt version.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CreatePromptRequest opens a new prompt name.
type CreatePromptRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CommitVersionRequest commits an immutable version. Exactly one of Messages
// (kind chat) or TextTemplate (kind text) is sent.
type CommitVersionRequest struct {
	Messages     []Message `json:"messages,omitempty"`
	TextTemplate string    `json:"text_template,omitempty"`
	Engine       string    `json:"engine,omitempty"`
	Message      string    `json:"message,omitempty"`
}

// PromptVersion is the full committed version.
type PromptVersion struct {
	ID                string    `json:"id"`
	PromptID          string    `json:"prompt_id"`
	Number            int       `json:"number"`
	Engine            string    `json:"engine"`
	Messages          []Message `json:"messages"`
	TextTemplate      *string   `json:"text_template"`
	DetectedVariables []string  `json:"detected_variables"`
	Message           *string   `json:"message"`
	ContentSHA256     string    `json:"content_sha256"`
	CreatedAt         string    `json:"created_at"`
}

// ---- models ---------------------------------------------------------------

// Pricing is per-million-token pricing for a catalog model.
type Pricing struct {
	InputPerM  float64 `json:"input_per_m"`
	OutputPerM float64 `json:"output_per_m"`
	Currency   string  `json:"currency"`
	Unit       string  `json:"unit"`
}

// Model is one catalog entry. Note the two meanings of "model id": Model.ID is
// the catalog UUID a deployment pins, Model.ModelID is the provider-side
// string ("anthropic/claude-sonnet-4").
type Model struct {
	ID              string         `json:"id"`
	Provider        string         `json:"provider"`
	ModelID         string         `json:"model_id"`
	DisplayName     string         `json:"display_name"`
	Metadata        map[string]any `json:"metadata"`
	ProviderOptions map[string]any `json:"provider_options"`
	Pricing         *Pricing       `json:"pricing"`
	ContextLength   int            `json:"context_length"`
	Capabilities    []string       `json:"capabilities"`
	Status          string         `json:"status"`
	CreatedAt       string         `json:"created_at"`
}

// RegisterModelRequest is the body of POST /models. Only ModelID is required;
// the server fills the rest from OpenRouter's public catalog when it can.
type RegisterModelRequest struct {
	Provider        string         `json:"provider,omitempty"`
	ModelID         string         `json:"model_id"`
	DisplayName     string         `json:"display_name,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
	Pricing         *Pricing       `json:"pricing,omitempty"`
	ContextLength   int            `json:"context_length,omitempty"`
	Capabilities    []string       `json:"capabilities,omitempty"`
	Status          string         `json:"status,omitempty"`
}

// ---- deployments ----------------------------------------------------------

// Deployment is one immutable revision: a model, its params, and one pinned
// version per prompt name.
type Deployment struct {
	ID              string            `json:"id"`
	Revision        int               `json:"revision"`
	Environment     string            `json:"environment"`
	ModelID         string            `json:"model_id"`
	Model           string            `json:"model"`
	Params          map[string]any    `json:"params"`
	ProviderOptions map[string]any    `json:"provider_options"`
	PromptPins      map[string]string `json:"prompt_pins"`
	CreatedAt       string            `json:"created_at"`
}

// CreateDeploymentRequest commits a new revision. Exactly one of ModelID (a
// catalog UUID) or Model (a provider string, registered on the fly) is
// required; when both are sent ModelID wins.
type CreateDeploymentRequest struct {
	Environment     string            `json:"environment,omitempty"`
	ModelID         string            `json:"model_id,omitempty"`
	Model           string            `json:"model,omitempty"`
	PromptPins      map[string]string `json:"prompt_pins,omitempty"`
	Params          map[string]any    `json:"params,omitempty"`
	ProviderOptions map[string]any    `json:"provider_options,omitempty"`
}

// RollbackRequest re-commits a past revision as a new one.
type RollbackRequest struct {
	Environment string `json:"environment,omitempty"`
	Revision    int    `json:"revision"`
}

// ---- keys -----------------------------------------------------------------

// APIKey is a project-scoped runtime key. Key (the raw secret) is populated
// only in the response that creates it.
type APIKey struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	KeyPrefix  string   `json:"key_prefix"`
	Scopes     []string `json:"scopes"`
	LastUsedAt *string  `json:"last_used_at"`
	CreatedAt  string   `json:"created_at"`
	Key        string   `json:"key,omitempty"`
}

// IssueAPIKeyRequest is the body of POST /api-keys.
type IssueAPIKeyRequest struct {
	Name   string   `json:"name,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

// ProviderKey is the org's BYOK provider credential. The secret never comes
// back; only a masked hint does.
type ProviderKey struct {
	Connected  bool    `json:"connected"`
	ID         string  `json:"id,omitempty"`
	Provider   string  `json:"provider"`
	Label      string  `json:"label,omitempty"`
	Hint       string  `json:"hint,omitempty"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	CreatedAt  string  `json:"created_at,omitempty"`
}

// SetProviderKeyRequest is the body of POST /provider-key.
type SetProviderKeyRequest struct {
	Secret string `json:"secret"`
	Label  string `json:"label,omitempty"`
}

// ---- list envelopes -------------------------------------------------------
//
// Success bodies are bare objects; lists are a single plural key.

type orgsEnvelope struct {
	Orgs []Org `json:"organizations"`
}
type projectsEnvelope struct {
	Projects []Project `json:"projects"`
}
type useCasesEnvelope struct {
	UseCases []UseCase `json:"use_cases"`
}
type modelsEnvelope struct {
	Models []Model `json:"models"`
}
type deploymentsEnvelope struct {
	Deployments []Deployment `json:"deployments"`
}
type apiKeysEnvelope struct {
	APIKeys []APIKey `json:"api_keys"`
}

// Raw is an undecoded JSON fragment, used for the contents of an error
// envelope's "details" object.
type Raw = json.RawMessage
