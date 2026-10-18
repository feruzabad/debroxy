# 0015. Embed the migrations through a `migrations` package

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 8, §5.8 (storage setup), NFR-05; decisions DEC-13, DEC-42 |
| Related | ADR 0001 (rule on migration naming), ADR 0002 (rule 1), ADR 0014; ddd-rules §2, §3, S-1 |

## Context

- debroxy ships as a single binary (NFR-05). The goose migrations must therefore be compiled into it; reading them from disk at runtime would add a second artefact to deploy and keep in step with the binary.
- ddd-rules §2 and ADR 0001 keep all migrations in one folder, `migrations/` at the repository root, named `NNNN_<context>_<change>.sql`. One folder gives one version sequence for goose across all contexts.
- Go embeds files only through `//go:embed` in a Go file whose directory contains them; a pattern cannot reach a parent directory. `cmd/debroxy` therefore cannot embed `../../migrations`.
- `internal/common/sqlite` runs the migrations (`Migrate(ctx, database, fs.FS)`), but it owns no tables and no schema (ADR 0002 rule 1), so it must not hold the SQL files.
- The §2 layout (as amended by ADR 0014) has no Go package outside `cmd/` and `internal/`, and the §3 table lets `cmd` import only `<context>/service` and `internal/common/*`.

## Decision

1. `migrations/` contains one Go file, `migrations/migrations.go`, in `package migrations`. It holds only the embed directive and one exported variable:
   ```go
   //go:embed *.sql
   var Files embed.FS
   ```
   It has no functions, no other declarations and no imports besides `embed`.
2. `cmd` passes `migrations.Files` to `internal/common/sqlite.Migrate` at startup (and in the `migrate` subcommand). Nothing else imports `migrations`.
3. The SQL files stay where ADR 0001 puts them, with the same naming rule and the same one-context-per-file rule.
4. ddd-rules changes:
   - §2 lists `migrations/migrations.go` as the embed-only package;
   - the §3 table gains `migrations → embed (only)` and extends `cmd → <context>/service, internal/common/*, migrations`.

## Consequences

### Positive
- The binary carries its schema; `serve` and `migrate` need no files besides the configuration.
- One folder and one goose version sequence remain, as ADR 0001 decided.

### Negative
- A Go file sits among SQL files at the repository root.
- `go:embed *.sql` fails to compile while the folder has no SQL file, so the package is created together with the first migration.

### Risks and mitigations
- *Risk:* the package grows helpers (listing, filtering, version logic). *Mitigation:* the architecture test allows `migrations` to import only `embed` and only `cmd` to import `migrations`; code review rejects any declaration besides `Files`.

## Alternatives considered

**Per-context migration folders embedded by each context.** Each context would embed its own SQL files under `adapters/out/sqlite` and expose them through its `service`; `cmd` would merge them for goose. No package is added at the root, but one goose version sequence would span several folders, so two contexts could pick the same number unnoticed. It also reverses the single folder of ADR 0001, which would need a superseding ADR.

**Migrations inside `internal/common/sqlite`.** That package owns no tables (ADR 0002 rule 1). Holding every context's schema would make the shared kernel depend on all contexts' storage, which DEP-7 exists to prevent.

**Read migrations from disk at runtime.** Breaks the single-binary requirement (NFR-05) and lets binary and schema drift apart.

## Compliance

- `internal/archtest` classifies `migrations` as its own layer: it may import only `embed`, and only `cmd` may import it.
- Code review: `migrations/migrations.go` declares nothing but `Files`.
