# prompton-cli

The command-line interface for [PromptOn](https://app.prompton.ai) — the control
plane for LLM prompts and models.

PromptOn does not sit in your application's request path. It stores the
configuration (a prompt version plus a model pin) and your app fetches it. This
CLI is what creates that configuration, and it is built to be driven by a coding
agent as much as by a person: every command takes `--json`, exits 0/1/2, and
turns "already exists" into something a re-run can survive.

```
prompton login
prompton projects create helpdesk
prompton prompt create support_reply
prompton prompt commit support_reply --file messages.json
prompton deploy support_reply --model openai/gpt-4o-mini
prompton api-keys issue --name 'Helpdesk server'
```

---

## Install

```sh
curl -fsSL https://prompton.ai/install.sh | sh
```

`prompton.ai/install.sh` redirects to [`install.sh`](install.sh) on this
repository's `main` branch, so the two are always the same script. It detects
your OS and architecture, downloads the matching release archive, **verifies
its SHA-256 against the release checksums**, and installs to `/usr/local/bin`
when that is writable or `~/.local/bin` otherwise. Override either choice:

```sh
PTN_VERSION=v0.1.0 PTN_INSTALL_DIR="$HOME/bin" \
  sh -c "$(curl -fsSL https://prompton.ai/install.sh)"
```

Until the first tagged release, `install.sh` installs the rolling `main`
build — the [`main-latest`](https://github.com/polimo-dev/prompton-cli/releases/tag/main-latest)
pre-release, rebuilt on every push to `main` — and prints a line saying so;
pin with `PTN_VERSION=v0.1.0` later. `PTN_VERSION=main` asks for the rolling
build explicitly at any time.

Other ways in:

```sh
brew install polimo-dev/tap/prompton                  # Homebrew adds the tap itself
go install github.com/polimo-dev/prompton-cli@latest  # from source; reports the compiled-in version
```

Release archives — macOS and Linux on amd64 and arm64, Windows on amd64 — are
on the [Releases](https://github.com/polimo-dev/prompton-cli/releases) page;
put `prompton` on your `PATH`.

---

### Uninstall

```sh
curl -fsSL https://prompton.ai/uninstall.sh | sh
```

Signs the CLI out (revoking the session token on the server), removes the `prompton` binary
install.sh put in place, and deletes `~/.config/prompton`. Set `PTN_KEEP_CONFIG=1` to keep the
configuration directory. Homebrew installs: `brew uninstall prompton`.

## Signing in

There is no management key to copy around. `prompton login` runs a device
approval in your browser and stores a **long-lived, revocable session token for
your user**:

```
$ prompton login

  Open this URL to approve the login:
    https://app.prompton.ai/device?code=K7QP-M4XZ

  Your code: K7QP-M4XZ

Waiting for approval…

Logged in as ada@example.com
Organization: personal
Session stored in /Users/ada/.config/prompton/config.json
```

Everything the CLI does afterwards runs **as you**, so it can reach exactly the
organizations and projects your membership already allows — no more. Anything
outside them answers 404, as if it did not exist.

`prompton logout` revokes the token server-side and then clears it locally.
Losing a laptop is a `logout` on another machine away from being handled.

---

## Scope: organization and project

Paths in the API are organization-scoped, so every command needs to know which
organization it is acting in, and most also need a project. Set them once:

```sh
prompton use --org acme --project helpdesk
prompton use --org personal            # your personal organization
```

`personal` is the reserved name for your own organization, which has no slug.
Both values are checked against the server before they are stored, so a typo
fails here instead of three commands later.

Any command can override them for a single run with `--org` and `--project`.

---

## Quickstart: onboarding an existing app

This is the sequence a coding agent follows when it migrates an app's
hard-coded prompts into PromptOn. It ends with the app fetching its
configuration instead of carrying it.

### 1. Sign in and create the project

```sh
prompton login
prompton projects create helpdesk --description 'Customer support app' --timezone Etc/UTC
prompton use --project helpdesk
```

`production` (protected) and `staging` environments are created with the
project.

### 2. Create a prompt per call site

One prompt for each place the app calls an LLM. The key is the app's
contract and cannot be changed later.

```sh
prompton prompt create support_reply \
  --name 'Support reply' \
  --description 'Answers a customer message in the support inbox' \
  --input-schema-file schema.json \
  --default-params '{"temperature":0.5}'
```

`schema.json` declares the variables the template expects:

```json
[
  {"name": "question", "type": "string", "required": true,
   "description": "The customer's message"},
  {"name": "plan", "type": "string", "required": false,
   "description": "free or pro"}
]
```

The prompt is created with its initial default template; callers still address only the prompt key.

### 3. Commit the app's existing prompt as version 1

```sh
prompton prompt commit support_reply \
  --file messages.json \
  --message "migrated from the app's hardcoded prompt"
```

`messages.json` is the prompt exactly as the app had it:

```json
[
  {"role": "system", "content": "You are a friendly support agent for Acme. Answer in two or three sentences; if you are not sure, say so and offer to escalate."},
  {"role": "user", "content": "{{ question }}"}
]
```

A file holding a JSON array (or an object with a `messages` array) is committed
as chat messages. Any other non-empty file is committed as one `user` message.
Pass `--file -` to read stdin.

Versions are immutable, and committing one changes nothing at runtime. A
version goes live only when a deployment pins it.

If the same call site needs language, tone, or tenant variants, keep them in
one prompt with variables and Liquid branches. If the purpose differs, create a
separate prompt.

### 4. Pin a deployment — the app's current model, unchanged

```sh
prompton models register openai/gpt-4o-mini    # optional; deploy does it too

prompton deploy support_reply \
  --environment production \
  --model openai/gpt-4o-mini \
  --params '{"temperature":0.3}' \
  --version 1
```

A revision is a pin, not a router: one model, its params, and one prompt
version. Committing it makes it the live configuration for that
(prompt, environment) pair.

- `--model` takes a provider string or a catalog UUID. A provider string that
  is not in the catalog is registered on the way past.
- `--version` takes a version number, the word `latest`, or a version UUID.
  Omit `--version` to pin the newest committed prompt version.
- Promoting staging to production is the same command with a different
  `--environment` and the same version — staging can carry its own params, say
  `--params '{"temperature":0.7}'` against the same model.

### 5. Issue the runtime key

```sh
prompton api-keys issue --name 'Helpdesk server' --scopes read,logs
```

The secret is printed once and never again. It is scoped to this project and
to deployed prompt reads (`read`) plus monitoring logs (`logs`). One key
covers every environment; the app names the environment in each request.

For a script:

```sh
PTN_KEY=$(prompton api-keys issue --quiet)
```

### 6. Point the app at PromptOn

Replace the hard-coded prompt and model with a deployed prompt fetch. The
runtime API (`/api/v1/prompts`, `/api/v1/prompts/:key/render`,
`/api/v1/logs`) is documented
separately; the app calls its LLM provider directly, with its own key, so
PromptOn stays out of the request path.

Confirm onboarding is done by filling a deployed prompt with the runtime key:

```sh
curl -sS -H "Authorization: Bearer $PTN_KEY" \
  -H 'content-type: application/json' \
  -d '{"variables":{"question":"Where is my order?"},"environment":"production"}' \
  https://app.prompton.ai/api/v1/prompts/support_reply/render
```

### 7. Optional: connect a provider key

Only needed for the places PromptOn itself calls an LLM — the arena, AI drafts,
evaluations. The app's own traffic never needs it.

```sh
prompton provider-key set                 # prompts, or reads PTN_OPENROUTER_KEY
prompton provider-key status
```

### 8. Operate

```sh
prompton prompt get support_reply           # what is live right now
prompton deployments list support_reply
prompton deployments list support_reply --environment production   # history
prompton rollback support_reply --environment production --revision 2
```

Rolling back re-commits an old revision, so it produces a new, higher revision
number. History is never rewritten.

---

## Commands

Every command accepts the global flags below.

### Session

| Command | What it does |
|---|---|
| `prompton login [--host H] [--no-browser] [--org O]` | Browser approval; stores a session token |
| `prompton logout` | Revokes the token server-side, then clears it locally |
| `prompton whoami` | The signed-in user, their organizations, and the active scope |
| `prompton orgs list` | Organizations you belong to |
| `prompton use --org O [--project P]` | Remembers the default scope |

### Projects

| Command | What it does |
|---|---|
| `prompton projects list` | The organization's projects |
| `prompton projects create <slug> [--description D] [--timezone TZ]` | Creates a project plus its environments |

### Prompts

| Command | What it does |
|---|---|
| `prompton prompt list` | Every call site in the project |
| `prompton prompt get <key>` | The prompt with its versions and live deployments |
| `prompton prompt contract <key> [--candidate-file F] --json` | Read live, draft, and optional proposed response contracts without saving or deploying |
| `prompton prompt create <key> [--name N] [--description D] [--input-schema-file F] [--default-params JSON] [--tags a,b]` | Creates a chat prompt |
| `prompton prompt update <key> [--name N] [--description D] [--tags a,b] [--input-schema-file F] [--default-params JSON]` | Changes only the fields given; schema and params are replaced, not merged |

### Prompts

| Command | What it does |
|---|---|
| `prompton prompt commit <prompt> --file F [--engine liquid\|raw] [--message M]` | Commits an immutable chat version |

### Models

| Command | What it does |
|---|---|
| `prompton models list` | The project's catalog |
| `prompton models register <model-id> [--display-name N] [--provider P]` | Adds a provider model; OpenRouter details are filled in server-side |

### Deployments

| Command | What it does |
|---|---|
| `prompton deploy <prompt> --model M [--environment E] [--params JSON] [--provider-options JSON] [--version latest\|N\|UUID]` | Commits a revision |
| `prompton deployments list <prompt> [--environment E]` | Live revisions, or one environment's history |
| `prompton rollback <prompt> --revision N [--environment E]` | Re-commits a past revision |

### Keys

| Command | What it does |
|---|---|
| `prompton api-keys issue [--name N] [--scopes read,logs]` | Mints a runtime key; the secret is shown once |
| `prompton api-keys list` | Live runtime keys, without secrets |
| `prompton provider-key set [--secret S] [--label L]` | Stores the organization's OpenRouter key |
| `prompton provider-key status` | Whether one is connected, and its masked hint |

---

## For agents

### `--json`

Every command prints a JSON document on stdout under `--json`, and **only**
that: progress lines move to stderr, so `prompton … --json | jq` is always
safe.

```sh
prompton prompt get support_reply --json | jq -r '.deployments[].model'
prompton projects list --json | jq -r '.projects[].slug'
```

Before changing a deployed prompt's type or Decision questions, inspect its
response contracts:

```sh
prompton prompt contract support_reply --json
prompton prompt contract support_reply --candidate-file migration-candidate.json --json
```

The result includes every live environment's pinned content and response contract,
the effective draft, and response formats for Chat and Decision answers. A
candidate file is an unapplied preview: `{ "kind": "decision", "engine": "liquid",
"decision": { "state": "{{input}}", "questions": { "is_bug": { "type": "noul",
"instructions": "Is this a bug?" } } } }`. Chat candidates use `kind: "chat"` and
`messages` instead. Use `--candidate-file -` for stdin. Inspection never changes a
draft, creates a deployment, or calls a provider. It always prints JSON.

Update the app to handle both live and proposed response contracts, test both,
and finish rolling out every app instance and worker before applying the type
change or deploying the new prompt. Parse each response with the same request's
API and pinned questions; an SDK cache may briefly retain the old deployment.
Keep both paths for rollback. Decision `noul` is a numeric probability, not a
boolean; question names, types, choice labels, and score rubrics can change the
application's response interpretation even when the prompt remains Decision.

Failures are JSON too, on stderr, in the same envelope the API uses:

```json
{
  "error": {
    "code": "not_found",
    "message": "no such revision",
    "status": 404,
    "details": {"available_revisions": [1, 2, 3]}
  }
}
```

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | The request was well-formed but did not work — server error, network failure, "already exists" |
| `2` | The invocation was wrong — unknown flag, missing argument, no organization selected, malformed JSON |

Code 2 always means *retyping the command can fix this*.

### Re-runnable provisioning: `--idempotent`

Creating something that already exists is HTTP 409, and the API returns the
existing resource in the error. The CLI prints that resource either way; the
flag decides the exit code:

```sh
prompton projects create helpdesk                # exit 1, "already exists"
prompton projects create helpdesk --idempotent   # exit 0, prints the existing project
```

So a provisioning script runs cleanly the second time:

```sh
set -e
prompton projects create helpdesk --idempotent --json > project.json
prompton prompt create support_reply --idempotent --json > prompt.json
```

### Quiet output

`--quiet` drops headers and progress, leaving values only — handy for capture:

```sh
PTN_KEY=$(prompton api-keys issue --quiet)
```

---

## Configuration

### File

`~/.config/prompton/config.json`, created `0600` inside a `0700` directory
because it holds a token. `$XDG_CONFIG_HOME` is honoured when set.

```json
{
  "host": "https://app.prompton.ai",
  "token": "…",
  "user": {"id": "0192…", "email": "ada@example.com"},
  "organizations": [
    {"id": "0192…", "name": "Ada", "personal": true},
    {"id": "0192…", "name": "Acme", "slug": "acme", "personal": false}
  ],
  "org": "acme",
  "project": "helpdesk"
}
```

### Environment

| Variable | Effect |
|---|---|
| `PTN_HOST` | API host; defaults to `https://app.prompton.ai`, with overrides for development and testing |
| `PTN_TOKEN` | Session token, for CI where no browser can approve one |
| `PTN_ORG` | Default organization |
| `PTN_PROJECT` | Default project |
| `PTN_OPENROUTER_KEY` | Read by `provider-key set` when `--secret` is absent |
| `PTN_CONFIG` | Config file path, overriding the default location |

### Precedence

**Flag beats environment beats config file beats built-in default.** So a
one-off `--org acme` never disturbs what `prompton use` stored, and CI can
set `PTN_TOKEN` without a config file existing at all:

```sh
PTN_TOKEN=$CI_SECRET PTN_ORG=acme \
  prompton deploy support_reply --model openai/gpt-4o-mini --json
```

---

## Development

```sh
make build         # ./prompton
make test          # go test ./...
make test-install  # sh -n install.sh, then install_test.sh (release resolution, no network)
make check         # gofmt -l, go vet, go test, test-install — what CI must see pass
make snapshot      # goreleaser release --snapshot --clean
make release-check # goreleaser check && shellcheck install.sh install_test.sh
```

Layout:

| Path | What lives there |
|---|---|
| `cmd/` | The cobra command tree, one file per area |
| `internal/api/` | Typed client for the management API, plus error-envelope decoding |
| `internal/config/` | Config file, environment, and flag precedence |
| `internal/device/` | The browser-approval login flow |
| `internal/output/` | Tables versus `--json` |
| `internal/meta/` | Name, version, module path — rename the product here |

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs `gofmt`,
`go vet`, `go test ./...`, and the `install.sh` checks on every push and pull
request.

Every push to `main` also runs
[`.github/workflows/main-build.yml`](.github/workflows/main-build.yml): a
GoReleaser snapshot build — the same archives and `checksums.txt` a release
gets, with the version stamped as `0.0.0-main.<sha>` — published to the
rolling `main-latest` pre-release, "main (rolling)". The tag is force-moved
and the release updated in place, so `install.sh` has something to install
before the first release and `PTN_VERSION=main` always has the newest build.

Releases are cut by tagging: `git tag v0.1.0 && git push --tags`. The tag
triggers [`.github/workflows/release.yml`](.github/workflows/release.yml),
which runs GoReleaser to build the archives, publish the GitHub release, and
update the Homebrew tap.

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Every
commit must be signed off under the
[Developer Certificate of Origin](https://developercertificate.org/)
(`git commit -s`), and contributions are licensed under this repository's
license.

## License

Licensed under the [Apache License 2.0](LICENSE).

**Trademark.** PromptOn is a trademark of Polimo. The license does not grant
permission to use the PromptOn name or logo; forks and derived services must
use a different name.
