# Contributing

Thanks for helping. Bug reports, fixes and documentation improvements are welcome; for a new feature or a change in behaviour, please open an issue first, so we can agree on it before you write code.

## How changes land

- Every change goes through a pull request into `main`, and is squash-merged. The pull request title becomes the commit message, so write it as a [Conventional Commit](https://www.conventionalcommits.org/): `<type>(<scope>): <summary>`, e.g. `fix(job): keep the slot when a submit times out`.
- The `ci` checks (`go`, `web`) and the `image` build must pass.
- Keep a pull request to one logical change.

## Design documents come first

debroxy is specified before it is built:

- The current specification is the highest-numbered `docs/specs/revisionN.md`; the browser client's is the highest `docs/frontend/revisionN.md`.
- Architecture decisions are in `docs/adr/`. Accepted ADRs are binding.
- A change in behaviour needs a new spec revision (`revisionN+1.md`, with a changelog) or a new ADR in the same pull request. Past revisions are never edited.
- Go code follows the hexagonal, DDD and CQRS rules in `docs/ddd-rules.md`; `internal/archtest` enforces the package rules.

## Local checks

You need Go 1.27 and Docker; the web client, the linter and the visual tests run in pinned containers, so nothing else is installed.

```sh
make check        # Go: build, vet, race tests, architecture test, lint
make web-check    # web client: lint, types, unit tests, size budgets, visual tests
```

If a pull request changes the UI on purpose, regenerate the visual baselines with `make web-visual-update` and commit them.

## Never commit

- API keys, tokens, CDN URLs or other secrets. Tests use fakes and the sanitized recordings in `docs/discovery/`; they never call a real provider.
- `.torrent` or `.nzb` files. Tests build synthetic torrents in code.

By contributing, you agree that your contribution is licensed under the [MIT License](LICENSE).
