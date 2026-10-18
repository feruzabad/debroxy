# debroxy architecture rules (Hexagonal + DDD + CQRS)

Status: binding for all Go code in this repository.
Origin: distilled from [ThreeDotsLabs/wild-workouts-go-ddd-example](https://github.com/ThreeDotsLabs/wild-workouts-go-ddd-example) (commit `8ecfcdf0`) and adapted to debroxy's decisions.

## 0. How to use this file

- Rules have stable IDs (`L-1`, `D-4`, ...). Cite them in reviews and commit bodies ("violates D-4").
- **MUST** = no exceptions without an ADR. **SHOULD** = default; deviation needs a code comment explaining why.
- A conflict between this file and code is resolved by fixing the code, or by an ADR that changes this file. Never by silent drift.
- Each rule is followed by a one-line **Why**. When unsure how to apply a rule, follow the template in §12.

Module path: `github.com/feruzabad/debroxy`.

---

## 1. Vocabulary

| Term | Meaning in this codebase |
|---|---|
| Bounded context | A top-level package under `internal/` with its own model and language (`identity`, `provider`, `job`, `delivery`). |
| Aggregate | Cluster of domain objects changed together in one transaction; accessed only through its root. |
| Entity | Object with identity and lifecycle. |
| Value object | Immutable object defined by its values; compared by value. |
| Driving port (`ports/in`) | What the outside world may ask the context to do: command/query types and handler interfaces. |
| Driven port (`ports/out`) | What the context needs from the outside world: repositories, read models, gateways, clock. |
| Driving adapter (`adapters/in`) | Translates a transport (HTTP, webhook, CLI) into calls on driving ports. |
| Driven adapter (`adapters/out`) | Implements a driven port with a technology (SQLite, memory, TorBox HTTP, another context). |
| Composition root (`service`) | The only place where concrete adapters are constructed and wired. |

---

## 2. Layout (L)

```
cmd/debroxy/main.go                 process entry; config load; starts servers
api/openapi/<context>.yml           HTTP contract per context (source of truth)
migrations/                         goose SQL migrations, named NNNN_<context>_<change>.sql (ADR 0001)
  migrations.go                     embed-only package: `//go:embed *.sql` + `var Files embed.FS`, used by cmd (ADR 0015)
web/                                browser client sources (docs/frontend/); build output in web/dist
  web.go                            embed-only package: `//go:embed all:dist` + `var Dist embed.FS`, used by cmd (ADR 0022)
internal/common/                    shared kernel (small, stable, no business rules)
  errors/                           slug errors + ErrorType
  decorator/                        CommandHandler, QueryHandler, OperationHandler, logging/metrics decorators
  auth/                             Principal{UserID, Role} carried in request context (set by the common/server auth middleware, DEC-22)
  clock/                            Clock interface + system/fixed implementations
  sqlite/                           shared SQLite writer/reader pools and start lock, opened in cmd (DEC-42); no tables, no business rules
  id/                               ID generation
  server/                           HTTP server bootstrap, middleware, httperr
  httpclient/                       outbound HTTP client: pinned egress, User-Agent, retry, circuit breaker, rate buckets, secret URLs (ADR 0017); no business rules
internal/archtest/                  architecture test of §2 and §3; no production code, imported by nothing (ADR 0014)
internal/<context>/
  domain/<aggregate>/               pure model: entities, value objects, state machines, errors, policies
  ports/in/                         driving ports
  ports/out/                        driven ports
  app/command/                      command handler implementations
  app/query/                        query handler implementations
  app/operation/                    operation implementations (A-10 contexts only, e.g. provider)
  adapters/in/http/                 oapi-codegen server + mapping
  adapters/in/<webhook|cli>/        other driving adapters
  adapters/out/sqlite/              SQLite repositories/read models (sqlc code lives here)
  adapters/out/memory/              in-memory repositories (tests, contract suite)
  adapters/out/<tech-or-context>/   provider clients, adapters to other contexts
  service/                          NewApplication, NewComponentTestApplication
```

- **L-1** MUST: One Go module, one binary (modular monolith). Each bounded context is a package tree under `internal/`.
  **Why:** strong boundaries without distributed-system cost; a context can be extracted later because its edges are already ports.
- **L-2** MUST: Every context has exactly the sub-packages above (omit only empty ones).
  **Why:** a reader finds any concept in the same place in every context.
- **L-3** MUST: One aggregate per `domain/<aggregate>` package; package name is the singular aggregate noun (`remoteitem`, `user`).
  **Why:** the package name becomes the qualifier (`remoteitem.Lifecycle`), which reads as language.
- **L-4** SHOULD: File names are snake_case and named after the concept or use case (`submit_job.go`, `remote_item_sqlite_repository.go`).
  **Why:** `ls` shows the use cases.

---

## 3. Dependency rules (DEP)

Allowed imports (→ means "may import"; amended by ADR 0014, ADR 0015, ADR 0016 and ADR 0022):

```
domain        → stdlib, internal/common/errors
ports/in      → domain, internal/common/decorator
ports/out     → domain, ports/in (DTOs returned by read models and gateways, R-8, ADR 0016)
app           → ports/in, ports/out, domain, internal/common/*
adapters/in   → ports/in, internal/common/*            (NOT app, NOT ports/out)
adapters/out  → ports/out, domain, ports/in (read-model and gateway DTOs only), internal/common/*   (NOT app)
service       → everything inside its own context, internal/common/*, other contexts' ports/in (deps types only)
cmd           → <context>/service, internal/common/*, migrations, web (opens the shared pools once, DEC-42)
migrations    → embed (only)
web           → embed (only)
```

`ports`, `ports/in`, `adapters/out` and `service` refer to the importer's own context unless stated otherwise. `service` only names another context's `Application` type to pass it to `adapters/out/<supplier>`; it never calls a supplier handler (DEP-4).

- **DEP-1** MUST: The domain is pure: no I/O, no `context.Context`, no logging, no SQL/JSON/HTTP tags, no time.Now (time is passed in).
  **Why:** the domain is the part humans must understand; it stays testable with plain values.
- **DEP-2** MUST: Adapters never import `app`. They see only port interfaces.
  **Why:** use cases can be replaced or decorated without touching transports.
- **DEP-3** MUST: Adapters never import other adapters.
  **Why:** HTTP and SQLite must not know about each other.
- **DEP-4** MUST: The **only** cross-context use is: a consumer context's `adapters/out/<supplier>` imports and calls the supplier's `ports/in`. The consumer first declares what it needs as an interface in its own `ports/out`. The consumer's `service` may also import the supplier's `ports/in`, but only to name the `Application` type it receives in `deps` and passes to that adapter (ADR 0014).
  **Why:** contexts depend on published capabilities, never on internal models; the consumer owns the shape of the dependency.
- **DEP-5** MUST: Domain types of one context never appear in another context. Translate at the adapter.
  **Why:** a change in one model must not ripple into another.
- **DEP-6** MUST: Transport types (oapi-generated structs, HTTP requests, JWT/session claims) never appear in `ports`, `app`, or `domain`. Adapters convert them to an actor value defined by the context (e.g. `ports/in.Actor`).
  **Why:** wild-workouts leaked `auth.User` into a query; this rule forbids that.
- **DEP-7** MUST: `internal/common` contains no business rules and never imports a context.
  **Why:** a growing shared kernel couples every context.

---

## 4. Ubiquitous language & naming (N)

- **N-1** MUST: Names come from the spec's glossary. If a concept has no glossary entry, add it to the spec before naming it in code.
  **Why:** code and conversation use the same words.
- **N-2** MUST: Never use generic names: `Manager`, `Helper`, `Util`, `Data`, `Info`, `Processor`, `Handle` (as a noun), `common` inside a context.
  **Why:** they hide intent.
- **N-3** MUST: Follow Go initialisms: `ID`, `URL`, `HTTP`, `NZB`, `API` (`HTTPServer`, `ProviderItemID`). wild-workouts' `HttpServer` is not followed.
- **N-4** Type and function naming:

| Thing | Rule | Example |
|---|---|---|
| Command | Imperative verb phrase, no suffix | `SubmitJob`, `ReleaseJob` |
| Query | Noun phrase, no suffix | `JobsOfUser`, `JobDetails` |
| Handler interface (ports/in) | `<Command>Handler` | `SubmitJobHandler` |
| Handler implementation (app) | unexported `<command>Handler` | `submitJobHandler` |
| Handler constructor (app) | `New<Command>Handler` | `NewSubmitJobHandler` |
| Repository port | `<Aggregate>Repository` | `RemoteItemRepository` |
| Repository adapter | `<Tech><Aggregate>Repository` (always this order) | `SQLiteRemoteItemRepository` |
| Read model port | `<Query>ReadModel` | `JobsOfUserReadModel` |
| Gateway port to an external system | `<System>Gateway` | `ProviderGateway` |
| Port to another context | Named for what the consumer needs | `JobOwnershipChecker` |
| Constructor | `NewX` returns `(X, error)`; `MustNewX` panics (tests/fixtures only) | `NewFingerprint` |
| Rehydration | `UnmarshalXFromDatabase` | `UnmarshalRemoteItemFromDatabase` |
| Getter | No `Get` prefix | `item.Name()` |
| Predicate | `Is`/`Has`/`Can` | `IsTerminal()`, `CanBeReleasedBy(u)` |
| Mutator | Domain verb, returns `error` if it can fail | `Release(jobID)` |
| Sentinel error | `ErrXxx` | `ErrJobAlreadyReleased` |
| Error with context | `XxxError` struct | `InvalidTransitionError{From, To}` |
| Slug | kebab-case, stable, part of the API contract | `slot-quota-exceeded` |
| DB model | unexported `<tech><Aggregate>Row` | `sqliteRemoteItemRow` |
| Test | `TestType_Method_case_in_snake_case` | `TestLifecycle_TransitionTo_rejects_backwards_move` |

- **N-5** SHOULD: Package names are short singular nouns; never `models`, `types`, `utils`.

---

## 5. Domain layer (D)

- **D-1** MUST: Entity and value-object fields are unexported. State changes only through methods.
  **Why:** invariants cannot be bypassed.
- **D-2** MUST: Objects are **always valid in memory**. `NewX` validates all invariants and returns an error; there is no "create empty then fill" path.
  **Why:** every method can trust its receiver.
- **D-3** MUST: Rehydration from storage uses `UnmarshalXFromDatabase`, documented with:
  `// UnmarshalXFromDatabase must only be used by repositories. It is not a constructor.`
  It still calls `NewX` (or equivalent checks) for basic invariants.
  **Why:** storage may hold states that constructors would reject (e.g. old statuses), but obvious corruption must still fail loudly.
- **D-4** MUST: Enums are struct value objects, never `type X string`:
  `type Kind struct{ s string }` with package-level values, `NewKindFromString`, `String()`, `IsZero()`.
  **Why:** callers cannot invent values outside the declared set.
- **D-5** MUST: When invariants depend on configuration, use a `Factory` holding a validated `FactoryConfig`; `NewFactory(cfg) (Factory, error)`, `Factory.IsZero()`; adapters panic on a zero factory.
  **Why:** configuration is checked once at startup, not at every call.
- **D-6** MUST: Errors carry context. Use sentinels only when no data helps the caller. Struct errors include the offending and the allowed values.
  **Why:** "invalid state" without saying which state is useless.
- **D-7** MUST: Domain errors that clients must see implement the slug contract (`Slug()`, `ErrorType()`) from `internal/common/errors`.
  **Why:** the HTTP adapter maps them without knowing the domain.
- **D-8** SHOULD: Domain logic that spans aggregates or needs no state is a free function (policy), e.g. `remoteitem.CanUserSeeJob(actor, item) error`.
- **D-9** MUST: Authorization rules about domain data live in the domain (policies), not in HTTP handlers.
  **Why:** wild-workouts did this in two different ways; we use only one.
- **D-10** MUST: Time is a parameter (`now time.Time`) or comes from a `clock.Clock` port in the app layer. The domain never calls `time.Now()`.
- **D-11** MUST: One aggregate is changed per transaction. If an invariant needs two aggregates in one transaction, the aggregate boundary is wrong. Fix the model instead.
  **Why:** clear consistency boundaries; this is why claims live inside `RemoteItem` in the spec.
- **D-12** MUST: Aggregates record domain events (`item.Events() []Event`, cleared by the repository after persisting). Events are past-tense facts: `LastClaimReleased`, `LifecycleAdvanced`.

### 5.1 State machines (SM)

- **SM-1** MUST: Any lifecycle with more than two states is a value object holding the current state plus a package-level transition table `map[State][]State`.
- **SM-2** MUST: Transitions go through one method, `TransitionTo(next State) (Lifecycle, error)`, returning `InvalidTransitionError{From, To}` for illegal moves. There are no direct state setters.
- **SM-3** MUST: Terminal states are declared explicitly (`IsTerminal()`), and the table has no outgoing edges for them.
- **SM-4** MUST: The table is documented in the spec, and a test asserts every allowed and every forbidden pair.
  **Why:** the table is the documentation; tests stop it from drifting.

---

## 6. Driven ports & repositories (R)

- **R-1** MUST: Repository interfaces live in `ports/out`, take and return domain types, and are named `<Aggregate>Repository`.
- **R-2** MUST: Updates use the transactional update-function idiom:
  ```go
  UpdateRemoteItem(ctx context.Context, id remoteitem.ID,
      updateFn func(ctx context.Context, item *remoteitem.RemoteItem) (*remoteitem.RemoteItem, error)) error
  ```
  The repository begins a transaction, loads, calls `updateFn`, persists only on `nil` error, then commits (or rolls back).
  **Why:** the use case never handles transactions, and load-modify-save cannot be split by accident.
- **R-3** MUST: **Secure by design**. When access depends on the actor, the repository method takes the actor and applies the domain policy before `updateFn` (wild-workouts `UpdateTraining(ctx, uuid, user, fn)`).
- **R-4** MUST: Not-found is a domain error type (`NotFoundError{ID}`), never `sql.ErrNoRows` leaking upward.
- **R-5** MUST: SQLite write transactions start with `BEGIN IMMEDIATE`. The commit error is always returned (wild-workouts swallows it in `finishTransaction`; never copy that).
- **R-6** MUST: Domain events and outbox messages are written in the same transaction as the aggregate (transactional outbox).
  **Why:** a provider-side delete must never be lost or duplicated by a crash.
- **R-7** MUST: Every repository has a memory implementation, and one contract test suite runs against all implementations (§11).
- **R-8** MUST: Queries use read-model ports (`<Query>ReadModel`) that return app-layer DTOs defined in `ports/in`, never domain entities.
  **Why:** reads can be optimized without bending the domain.
- **R-9** MUST: Gateways to external systems (`ProviderGateway`, `Clock`, `LinkSigner`) are driven ports with neutral types. No provider-specific field names cross the port.
- **R-10** MUST: When a decision about one aggregate needs facts about **other** aggregates (e.g. a quota count, a look-up before an insert), the repository runs those reads **inside the same write transaction** and passes them to `updateFn` through a read-only, use-case-specific port declared in `ports/out` (e.g. `SubmissionPolicyReads{ActiveChargedItemCount(userID); ClaimableByFingerprint(...)}`):
  ```go
  UpdateRemoteItem(ctx, id, updateFn func(ctx context.Context, item *remoteitem.RemoteItem, reads out.SubmissionPolicyReads) (*remoteitem.RemoteItem, error)) error
  ```
  The reads port has no write methods; the domain receives plain values from it (DEP-1); only one aggregate is saved (D-11).
  **Why:** makes read-then-write checks atomic under the single writer (ADR 0002) without exposing transactions to the app layer.

---

## 7. Driving ports & application layer (A)

- **A-1** MUST: Each use case is one file in `app/command` or `app/query` with exactly: the unexported handler struct, `New<X>Handler`, and `Handle`.
- **A-2** MUST: The command/query struct and the handler interface are declared in `ports/in` (one file per use case, same name):
  ```go
  type SubmitJob struct { ... }
  type SubmitJobHandler decorator.CommandHandler[SubmitJob]
  ```
- **A-3** MUST: Command handlers return only `error`. Query handlers return `(Result, error)`. A command that must hand back an identifier receives it in the command (generated by the caller through `id` before dispatch).
  **Why:** keeps the CQRS split strict; clients can retry idempotently with the same ID.
- **A-4** MUST: Constructors panic on nil dependencies with the dependency's real name (`panic("nil remoteItemRepository")`).
  **Why:** wiring errors fail at startup; wild-workouts copy-pasted wrong messages.
- **A-5** MUST: Every handler is wrapped with `decorator.ApplyCommandDecorators` / `ApplyQueryDecorators` (slog logging → metrics). Handlers do not log their own start/finish.
  **Why:** wild-workouts double-logged; decorators are the single place.
- **A-6** MUST: Handlers preserve domain errors. Wrap with `fmt.Errorf("...: %w", err)` only to add context; never replace a typed error with a generic slug.
  **Why:** wild-workouts turned domain errors into HTTP 500 this way.
- **A-7** MUST: `ports/in` exposes one `Application` struct per context:
  ```go
  type Application struct { Commands Commands; Queries Queries }
  ```
  Driving adapters receive `ports/in.Application`.
- **A-8** SHOULD: Side effects on external systems that must happen after a state change go through the outbox and an outbox processor (an app command fed by a driving adapter), not inline after commit.
- **A-9** MUST: Remote calls that create state elsewhere happen **before** the local transaction, and their result is stored. Where a remote call must happen inside `updateFn`, a remote failure rolls back the local change.
- **A-10** MAY: A context with no domain state of its own (rebuildable caches of learned facts, such as provider capability state, are allowed) that wraps an external system (an anti-corruption-layer context, e.g. `provider`) exposes its driving port as **operations** instead of commands and queries:
  `type CreateRemoteItemOperation decorator.OperationHandler[CreateRemoteItemRequest, CreateRemoteItemResult]`.
  Operations are named `<Verb><Noun>`, are declared in `ports/in` like handlers, implemented in `app/operation`, and decorated with `decorator.ApplyOperationDecorators`. An operation may return a stream (e.g. `io.ReadCloser`); the caller must close it.
  **Why:** forcing CQRS onto a pass-through to a remote API would add IDs and read models with no domain meaning.

---

## 8. Driving adapters (IN)

- **IN-1** MUST: HTTP servers are generated from `api/openapi/<context>.yml` with oapi-codegen (`std-http-server` target, Go 1.22+ `net/http.ServeMux`). The spec is written first.
- **IN-2** MUST: Adapters do only: decode → authenticate → map to command/query → call handler → map result/error to the response. No business decisions.
- **IN-3** MUST: Errors are mapped once, by `internal/common/server/httperr.RespondWithSlugError`:

| ErrorType | HTTP |
|---|---|
| `incorrect-input` | 400 |
| `authentication` | 401 |
| `authorization` | 403 |
| `not-found` | 404 |
| `conflict` | 409 |
| `quota-exceeded` | 429 |
| `unsupported` | 501 |
| `provider-unavailable` | 502 |
| `service-unavailable` | 503 |
| unknown / non-slug | 500 (generic message; never leak `err.Error()`) |

  Response body: `{"slug": "...", "message": "..."}`, plus an optional `"retryAfter": <seconds>` (and a `Retry-After` header) when the error carries a retry hint (ADR 0020).
- **IN-4** MUST: Response mapping functions are named `<appType>ToResponse` / `<request>ToCommand` and sit in the adapter.
- **IN-5** MUST: Webhook adapters verify authenticity before parsing, and convert payloads into neutral commands.

---

## 9. Driven adapters (OUT)

- **OUT-1** MUST: Storage rows are separate structs (`sqliteXRow`). Mapping functions `marshalX`/`unmarshalX` sit next to them; `unmarshalX` uses `UnmarshalXFromDatabase`.
- **OUT-2** MUST: sqlc-generated code stays inside `adapters/out/sqlite` and is never exported from it.
- **OUT-3** MUST: Provider clients are **anti-corruption layers**: provider JSON structs are unexported, live in `adapters/out/<provider>`, and are translated to port types immediately. Native states map to domain states through one explicit table per provider.
- **OUT-4** MUST: Each provider adapter owns its HTTP client config: base URL, **User-Agent (configurable per provider)**, timeout, rate limiter, retry with backoff, circuit breaker. No shared global `http.Client`.
- **OUT-5** MUST: Cross-context adapters (`adapters/out/<supplier>`) call the supplier's `ports/in.Application` and translate types both ways (DEP-4).
- **OUT-6** MUST: Secrets (API keys, tokens, signing keys) are never logged, never put in errors, never in URLs of log lines. Redact at the adapter.

---

## 10. Composition root (S)

- **S-1** MUST: Manual dependency injection in `service.NewApplication(ctx, cfg, deps) (in.Application, error)`; shared resources (database pools, HTTP clients that span contexts) are created in `cmd` and passed in `deps`, and `cmd` closes them. No DI framework, no `init()` side effects, no package-level mutable state outside tests.
- **S-2** MUST: `service.NewComponentTestApplication` wires memory adapters and fake gateways for component tests.
- **S-3** MUST: Configuration is parsed and validated once in `cmd`, then passed down as typed structs. Contexts never read env vars.

---

## 11. Testing (T)

- **T-1** MUST: Domain tests use external test packages (`package remoteitem_test`), `t.Parallel()`, testify `require`/`assert`, and compare errors by value (`assert.Equal(t, remoteitem.InvalidTransitionError{...}, err)`) or with `errors.As`.
- **T-2** MUST: App tests use hand-written mocks of driven ports in the test file. Unused methods `panic("not used in this test")`. Test names describe behaviour: `releases_provider_item_when_last_claim_released`.
- **T-3** MUST: Repository contract tests: one suite (`testXxx(t, repo)` functions) runs against every implementation (memory, SQLite). It covers create, update, rollback on `updateFn` error, not-found, and parallel updates (N goroutines; exactly the allowed number succeed).
- **T-4** MUST: Tests are isolated by unique data (random IDs), not by truncating tables, so they run in parallel.
- **T-5** MUST: Provider adapter tests run against recorded, sanitized fixtures in `docs/discovery/<provider>/` served by `httptest.Server`. Live tests run only behind the `discovery` build tag.
- **T-6** MUST: Component tests start the real HTTP server with `NewComponentTestApplication` and drive it through the oapi-generated client.
- **T-7** MUST: `go test -race ./...` passes.

---

## 12. Templates

Illustrative. Names come from the spec; the shapes are binding.

### 12.1 Enum value object

```go
package remoteitem

// Kind is the type of content a provider fetches.
// A struct instead of `type Kind string` keeps the set of values closed.
type Kind struct{ s string }

var (
	KindTorrent = Kind{"torrent"}
	KindNZB     = Kind{"nzb"}
	KindWeb     = Kind{"web"}
)

var allKinds = []Kind{KindTorrent, KindNZB, KindWeb}

func NewKindFromString(s string) (Kind, error) {
	for _, k := range allKinds {
		if k.s == s {
			return k, nil
		}
	}
	return Kind{}, UnknownKindError{Given: s}
}

func (k Kind) String() string { return k.s }
func (k Kind) IsZero() bool   { return k == Kind{} }

type UnknownKindError struct{ Given string }

func (e UnknownKindError) Error() string { return fmt.Sprintf("unknown kind %q", e.Given) }
```

### 12.2 State machine

```go
package remoteitem

type State struct{ s string }

var (
	StateQueued      = State{"queued"}
	StateDownloading = State{"downloading"}
	StateReady       = State{"ready"}
	StateFailed      = State{"failed"}
)

// allowedTransitions is the single source of truth; mirrored in the spec.
var allowedTransitions = map[State][]State{
	StateQueued:      {StateDownloading, StateReady, StateFailed},
	StateDownloading: {StateReady, StateFailed},
	StateReady:       {},
	StateFailed:      {},
}

type Lifecycle struct{ current State }

func (l Lifecycle) State() State      { return l.current }
func (l Lifecycle) IsTerminal() bool  { return len(allowedTransitions[l.current]) == 0 }

func (l Lifecycle) TransitionTo(next State) (Lifecycle, error) {
	if next == l.current {
		return l, nil
	}
	for _, allowed := range allowedTransitions[l.current] {
		if allowed == next {
			return Lifecycle{current: next}, nil
		}
	}
	return l, InvalidTransitionError{From: l.current, To: next}
}

type InvalidTransitionError struct{ From, To State }

func (e InvalidTransitionError) Error() string {
	return fmt.Sprintf("invalid transition from %s to %s", e.From, e.To)
}
```

### 12.3 Aggregate root

```go
package remoteitem

type RemoteItem struct {
	id          ID
	fingerprint Fingerprint
	lifecycle   Lifecycle
	jobs        []Job
	events      []Event
}

func NewRemoteItem(id ID, fp Fingerprint, firstJob Job) (*RemoteItem, error) {
	if id.IsZero() {
		return nil, errors.New("empty remote item id")
	}
	if fp.IsZero() {
		return nil, errors.New("empty fingerprint")
	}
	return &RemoteItem{id: id, fingerprint: fp, lifecycle: Lifecycle{current: StateQueued}, jobs: []Job{firstJob}}, nil
}

// UnmarshalRemoteItemFromDatabase must only be used by repositories. It is not a constructor.
func UnmarshalRemoteItemFromDatabase(id ID, fp Fingerprint, state State, jobs []Job) (*RemoteItem, error) { ... }

func (r *RemoteItem) ID() ID { return r.id }

func (r *RemoteItem) Release(jobID JobID, actor Actor) error {
	// find job, check policy, mark released, record LastClaimReleased when none remain
}

func (r *RemoteItem) Events() []Event { return r.events }
```

### 12.4 Driven port (repository)

```go
package out

type RemoteItemRepository interface {
	AddRemoteItem(ctx context.Context, item *remoteitem.RemoteItem) error
	RemoteItemByFingerprint(ctx context.Context, provider remoteitem.ProviderID, fp remoteitem.Fingerprint) (*remoteitem.RemoteItem, error)
	UpdateRemoteItem(
		ctx context.Context,
		id remoteitem.ID,
		updateFn func(ctx context.Context, item *remoteitem.RemoteItem) (*remoteitem.RemoteItem, error),
	) error
	// UpdateRemoteItemByJob locates the aggregate through one of its jobs (claims).
	UpdateRemoteItemByJob(
		ctx context.Context,
		jobID remoteitem.JobID,
		updateFn func(ctx context.Context, item *remoteitem.RemoteItem) (*remoteitem.RemoteItem, error),
	) error
}
```

### 12.5 SQLite update-function idiom

```go
func (r SQLiteRemoteItemRepository) UpdateRemoteItem(
	ctx context.Context,
	id remoteitem.ID,
	updateFn func(ctx context.Context, item *remoteitem.RemoteItem) (*remoteitem.RemoteItem, error),
) (err error) {
	tx, err := r.beginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { err = finishTransaction(tx, err) }()

	item, err := r.loadForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	updated, err := updateFn(ctx, item)
	if err != nil {
		return err
	}
	if err := r.save(ctx, tx, updated); err != nil {
		return err
	}
	return r.outbox.Append(ctx, tx, updated.Events())
}

// finishTransaction commits on success, rolls back on failure, and never swallows the commit error.
func finishTransaction(tx *sql.Tx, err error) error {
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("commit: %w", commitErr)
	}
	return nil
}
```

### 12.6 Driving port + command handler

```go
// ports/in/release_job.go
package in

type ReleaseJob struct {
	Actor Actor
	JobID string
}

type ReleaseJobHandler decorator.CommandHandler[ReleaseJob]
```

```go
// app/command/release_job.go
package command

type releaseJobHandler struct {
	remoteItemRepository out.RemoteItemRepository
}

func NewReleaseJobHandler(
	remoteItemRepository out.RemoteItemRepository,
	logger *slog.Logger,
	metricsClient decorator.MetricsClient,
) in.ReleaseJobHandler {
	if remoteItemRepository == nil {
		panic("nil remoteItemRepository")
	}
	return decorator.ApplyCommandDecorators[in.ReleaseJob](
		releaseJobHandler{remoteItemRepository: remoteItemRepository}, logger, metricsClient,
	)
}

func (h releaseJobHandler) Handle(ctx context.Context, cmd in.ReleaseJob) error {
	jobID, err := remoteitem.NewJobID(cmd.JobID)
	if err != nil {
		return err
	}
	return h.remoteItemRepository.UpdateRemoteItemByJob(ctx, jobID,
		func(ctx context.Context, item *remoteitem.RemoteItem) (*remoteitem.RemoteItem, error) {
			if err := item.Release(jobID, toDomainActor(cmd.Actor)); err != nil {
				return nil, err
			}
			return item, nil
		})
}
```

### 12.7 Query handler + read model

```go
// ports/in/jobs_of_user.go
type JobsOfUser struct{ Actor Actor }
type JobSummary struct { ID, Name, Provider, State string; SizeBytes int64 }
type JobsOfUserHandler decorator.QueryHandler[JobsOfUser, []JobSummary]

// ports/out/jobs_of_user_read_model.go
type JobsOfUserReadModel interface {
	JobsOfUser(ctx context.Context, userID string) ([]in.JobSummary, error)
}
```

### 12.8 Cross-context port (consumer side) + adapter

```go
// delivery/ports/out/job_ownership_checker.go   (consumer declares its need)
type JobOwnershipChecker interface {
	DownloadableFile(ctx context.Context, userID, jobID, fileID string) (DownloadableFile, error)
}

// delivery/adapters/out/job/job_ownership_checker.go   (only place importing job/ports/in)
type JobOwnershipChecker struct{ jobs jobin.Application }

func (c JobOwnershipChecker) DownloadableFile(ctx context.Context, userID, jobID, fileID string) (out.DownloadableFile, error) {
	details, err := c.jobs.Queries.JobDetails.Handle(ctx, jobin.JobDetails{...})
	if err != nil {
		return out.DownloadableFile{}, err
	}
	return jobDetailsToDownloadableFile(details, fileID)
}
```

### 12.9 Repository contract test

```go
func TestRemoteItemRepository(t *testing.T) {
	t.Parallel()
	for _, r := range createRepositories(t) {
		t.Run(r.Name, func(t *testing.T) {
			t.Parallel()
			t.Run("update", func(t *testing.T) { t.Parallel(); testUpdateRemoteItem(t, r.Repository) })
			t.Run("rollback", func(t *testing.T) { t.Parallel(); testUpdateRemoteItem_rollback(t, r.Repository) })
			t.Run("parallel", func(t *testing.T) { t.Parallel(); testUpdateRemoteItem_parallel(t, r.Repository) })
			t.Run("not_found", func(t *testing.T) { t.Parallel(); testRemoteItem_not_found(t, r.Repository) })
		})
	}
}
```

### 12.10 Composition root

```go
// cmd opens the shared pools once (internal/common/sqlite, DEC-42) and passes them in;
// a context's service never opens the database file itself.
func NewApplication(ctx context.Context, cfg Config, deps Dependencies) (in.Application, error) {
	db := deps.Database // *commonsqlite.Pools: Writer (1 conn, BEGIN IMMEDIATE) and Reader (query_only)
	remoteItems := sqlite.NewSQLiteRemoteItemRepository(db)
	logger, metrics := deps.Logger, deps.Metrics

	return in.Application{
		Commands: in.Commands{
			ReleaseJob: command.NewReleaseJobHandler(remoteItems, logger, metrics),
		},
		Queries: in.Queries{
			JobsOfUser: query.NewJobsOfUserHandler(sqlite.NewSQLiteJobsOfUserReadModel(db), logger, metrics),
		},
	}, nil
}
```

---

## 13. Standard libraries

| Concern | Choice |
|---|---|
| Logging | `log/slog` |
| HTTP routing | `net/http.ServeMux` (Go 1.22+ patterns) via oapi-codegen `std-http-server` |
| SQL | `database/sql` + sqlc |
| SQLite driver | `modernc.org/sqlite` (pure Go, no CGO) |
| Migrations | goose |
| Testing | stdlib `testing` + testify; `go-cmp` for unexported comparisons |
| Errors | stdlib `errors`/`fmt.Errorf("%w")`; no `pkg/errors` |

Adding a new dependency needs a one-line justification in the PR/commit body.
