# Changelog

## 0.8.0 — 2026-10-14

- Prompt variable definitions use `variables` throughout CLI requests and responses.
- Create and update prompts with `--variables-file`; the previous flag and wrapper field are no longer accepted.
- Use this release with the current schema-1 PromptOn server and SDK 0.8.0.

## 0.7.0

- Use readable resource references throughout CLI output: organization/project slugs, prompt keys, provider/model pairs, prompt version numbers, and deployment revisions.
- Remove private resource IDs from JSON output, tables, and stored account metadata. Existing config files remain readable; legacy organization ID aliases no longer select an organization.
- Read model names from `model`; preserve deployment `sequence`, `prompt_version_number`, and request contract metadata. SDK key responses retain the key name and prefix, and issuance still returns the secret once.
- Requires the readable management response contract. Existing commands, authentication tokens, and native prompt content remain unchanged.

Install from the pinned GitHub release instructions in README.md or build the tagged source. This release does not require publication to a package registry. Existing stable tags remain immutable.
