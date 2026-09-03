# Contributing to prompton-cli

Thanks for helping improve the PromptOn CLI. Bug reports, fixes, and new
commands are all welcome.

## Before you start

- For anything larger than a small fix, open an issue first so the change can
  be discussed before you spend time on it.
- The [README](README.md) describes the layout of the code and the `make`
  targets.

## Development

```sh
make build        # ./prompton
make check        # gofmt -l, go vet, go test — what CI runs
```

CI runs the same checks on every push and pull request, so run `make check`
before opening one.

## Developer Certificate of Origin

This project uses the
[Developer Certificate of Origin](https://developercertificate.org/) (DCO)
instead of a contributor license agreement. Signing off a commit certifies
that you wrote the change, or otherwise have the right to submit it, under the
project's license.

Every commit in a pull request must be signed off:

```sh
git commit -s -m "prompts: read templates from stdin"
```

This adds a trailer to the commit message:

```
Signed-off-by: Your Name <you@example.com>
```

Use your real name and a working email address. Pull requests with unsigned
commits are not merged; `git rebase --signoff` adds the trailer to commits you
have already made.

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE), the license of this repository.

PromptOn is a trademark of Polimo. The license does not grant permission to
use the PromptOn name or logo; forks and derived services must use a different
name.
