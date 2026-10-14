# Changelog

## 0.7.0

- Use readable resource references throughout CLI output: organization/project slugs, prompt keys, provider/model pairs, prompt version numbers, and deployment revisions.
- Remove private resource IDs from JSON output, tables, and stored account metadata. Existing config files remain readable; legacy organization ID aliases no longer select an organization.
- Read model names from `model`; preserve deployment `sequence`, `prompt_version_number`, and request contract metadata. SDK key responses retain the key name and prefix, and issuance still returns the secret once.
- Requires the readable management response contract. Existing commands, authentication tokens, and native prompt content remain unchanged.

Install from the pinned GitHub release instructions in README.md or build the tagged source. This release does not require publication to a package registry. Existing stable tags remain immutable.
