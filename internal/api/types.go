package api

import "encoding/json"

// ---- identity -------------------------------------------------------------

// User is the authenticated human behind the CLI session token.
type User struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// Org is one organization the user belongs to. A personal org has no slug and
// is addressed in paths by the literal "personal".
type Org struct {
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
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// Project is one project inside an org.
type Project struct {
	Slug         string        `json:"slug"`
	Description  string        `json:"description"`
	Timezone     string        `json:"timezone"`
	CreatedAt    string        `json:"created_at"`
	Environments []Environment `json:"environments"`
}

// CreateProjectRequest is the body of POST /projects.
type CreateProjectRequest struct {
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
}

// ---- prompts ------------------------------------------------------------

// Variable is one declared input variable of a prompt.
type Variable struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Required    bool    `json:"required"`
	Description *string `json:"description,omitempty"`
	Example     any     `json:"example,omitempty"`
}

// Prompt is one call site. GET of a single prompt also carries recent versions
// and live deployments.
type Prompt struct {
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	Description   *string        `json:"description"`
	Kind          string         `json:"kind"`
	Variables     []Variable     `json:"variables"`
	DefaultParams map[string]any `json:"default_params"`
	Tags          []string       `json:"tags"`
	CreatedAt     string         `json:"created_at"`

	Versions        []VersionSummary `json:"versions,omitempty"`
	PromptTemplates []PromptTemplate `json:"templates,omitempty"`
	Deployments     []Deployment     `json:"deployments,omitempty"`
}

// PromptContract preserves the complete inspection document, including dynamic
// question names and provider response fields. RawMessage retains numeric state
// values exactly rather than converting them through float64.
type PromptContract map[string]json.RawMessage

// InspectPromptContractRequest inspects a candidate without persisting it.
type InspectPromptContractRequest struct {
	Candidate json.RawMessage `json:"candidate"`
}

// CreatePromptRequest is the body of POST /prompts.
type CreatePromptRequest struct {
	Key           string         `json:"key"`
	Name          string         `json:"name,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	Description   string         `json:"description,omitempty"`
	Variables     *[]Variable    `json:"variables,omitempty"`
	DefaultParams map[string]any `json:"default_params,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
}

// UpdatePromptRequest is the body of PATCH /prompts/:key. Only the fields
// that are present are changed, so every field is a pointer: a nil pointer is
// "leave it alone", and a pointer to a zero value is "set it to that".
type UpdatePromptRequest struct {
	Name          *string         `json:"name,omitempty"`
	Description   *string         `json:"description,omitempty"`
	Tags          *[]string       `json:"tags,omitempty"`
	Variables     *[]Variable     `json:"variables,omitempty"`
	DefaultParams *map[string]any `json:"default_params,omitempty"`
}

// Empty reports whether the patch would change nothing.
func (r UpdatePromptRequest) Empty() bool {
	return r.Name == nil && r.Description == nil && r.Tags == nil &&
		r.Variables == nil && r.DefaultParams == nil
}

// ---- prompt templates -----------------------------------------------------

// VersionSummary is one immutable prompt version as it appears in a listing.
type VersionSummary struct {
	Number            int      `json:"number"`
	Message           *string  `json:"message"`
	DetectedVariables []string `json:"detected_variables"`
	CreatedAt         string   `json:"created_at"`
}

// PromptTemplate carries the version summaries for a prompt's default template.
type PromptTemplate struct {
	Name         string           `json:"name"`
	Description  *string          `json:"description"`
	CreatedAt    string           `json:"created_at"`
	VersionCount int              `json:"version_count"`
	Versions     []VersionSummary `json:"versions"`
}

// Message is one chat message in a prompt version.
type Message struct {
	Role    string `json:"role,omitempty"`
	Content any    `json:"content"`
	Type    string `json:"type,omitempty"`
	Name    string `json:"name,omitempty"`
	raw     map[string]json.RawMessage
}

// UnmarshalJSON retains native continuation fields and explicit null content.
func (m *Message) UnmarshalJSON(data []byte) error {
	type fields Message
	var value fields
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = Message(value)
	m.raw = raw
	return nil
}

func (m Message) MarshalJSON() ([]byte, error) {
	if m.raw != nil {
		return json.Marshal(m.raw)
	}
	type fields Message
	return json.Marshal(fields(m))
}

// CommitVersionRequest commits an immutable prompt version.
type CommitVersionRequest struct {
	Kind     string          `json:"kind,omitempty"`
	Messages []Message       `json:"messages,omitempty"`
	Decision json.RawMessage `json:"decision,omitempty"`
	Engine   string          `json:"engine,omitempty"`
	Message  string          `json:"message,omitempty"`
	Tools    json.RawMessage `json:"tools,omitempty"`
	Output   json.RawMessage `json:"output,omitempty"`
}

// PromptVersion is the full committed version.
type PromptVersion struct {
	Number            int             `json:"number"`
	Kind              string          `json:"kind,omitempty"`
	Engine            string          `json:"engine"`
	Messages          []Message       `json:"messages"`
	Decision          json.RawMessage `json:"decision,omitempty"`
	Tools             json.RawMessage `json:"tools,omitempty"`
	TextTemplate      *string         `json:"text_template"`
	DetectedVariables []string        `json:"detected_variables"`
	Message           *string         `json:"message"`
	ContentSHA256     string          `json:"content_sha256"`
	CreatedAt         string          `json:"created_at"`
}

// ---- models ---------------------------------------------------------------

// Pricing is per-million-token pricing for a catalog model.
type Pricing struct {
	InputPerM  float64 `json:"input_per_m"`
	OutputPerM float64 `json:"output_per_m"`
	Currency   string  `json:"currency"`
	Unit       string  `json:"unit"`
}

// Model is identified by its provider and provider-native model name.
type Model struct {
	Model           string         `json:"model"`
	Provider        string         `json:"provider"`
	DisplayName     string         `json:"display_name"`
	Metadata        map[string]any `json:"metadata"`
	ProviderOptions map[string]any `json:"provider_options"`
	Pricing         *Pricing       `json:"pricing"`
	ContextLength   int            `json:"context_length"`
	Capabilities    []string       `json:"capabilities"`
	Status          string         `json:"status"`
	CreatedAt       string         `json:"created_at"`
}

// RegisterModelRequest is the body of POST /models. Only Model is required;
// the server fills the rest from OpenRouter's public catalog when it can.
type RegisterModelRequest struct {
	Provider        string         `json:"provider,omitempty"`
	Model           string         `json:"model"`
	DisplayName     string         `json:"display_name,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
	Pricing         *Pricing       `json:"pricing,omitempty"`
	ContextLength   int            `json:"context_length,omitempty"`
	Capabilities    []string       `json:"capabilities,omitempty"`
	Status          string         `json:"status,omitempty"`
}

// ---- deployments ----------------------------------------------------------

// Deployment is one immutable revision: a model, its params, and the pinned
// prompt version. Revisions and sequence counters are readable scoped references.
type Deployment struct {
	Sequence        int            `json:"sequence"`
	API             string         `json:"api,omitempty"`
	RequestPath     string         `json:"request_path,omitempty"`
	Revision        string         `json:"revision"`
	Environment     string         `json:"environment"`
	Provider        string         `json:"provider"`
	Model           string         `json:"model"`
	PromptVersion   int            `json:"prompt_version_number,omitempty"`
	Params          map[string]any `json:"params"`
	ProviderOptions map[string]any `json:"provider_options"`
	CreatedAt       string         `json:"created_at"`
}

// CreateDeploymentRequest commits a new revision with readable resource
// references. Omitting PromptVersion asks the server to pin the latest
// committed compatible version.
type CreateDeploymentRequest struct {
	Environment     string         `json:"environment,omitempty"`
	Provider        string         `json:"provider,omitempty"`
	Model           string         `json:"model,omitempty"`
	PromptVersion   int            `json:"prompt_version,omitempty"`
	Params          map[string]any `json:"params,omitempty"`
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// PromoteRequest promotes an existing source revision to another environment.
type PromoteRequest struct {
	Environment       string `json:"environment"`
	SourceEnvironment string `json:"source_environment"`
	Revision          string `json:"revision"`
}

// RollbackRequest re-commits a past revision as a new one.
type RollbackRequest struct {
	Environment string `json:"environment,omitempty"`
	Revision    string `json:"revision"`
}

// ---- keys -----------------------------------------------------------------

// APIKey is a project-scoped SDK key. Key (the raw secret) is populated
// only in the response that creates it.
type APIKey struct {
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
type promptsEnvelope struct {
	Prompts []Prompt `json:"prompts"`
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
