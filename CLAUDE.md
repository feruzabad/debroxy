# debroxy

debroxy is a self-hosted Go proxy that lets several users share one debrid provider account (TorBox, Real-Debrid). All provider traffic leaves from one IP, and no files are stored.

- **Architecture rules:** `docs/ddd-rules.md`. All Go code MUST follow it (hexagonal + DDD + CQRS). Cite rule IDs in reviews.
- **Specification:** the highest-numbered `docs/specs/revisionN.md` is current. Never edit a past revision. Write `revisionN+1.md` with a changelog and an updated assumption register.
- **Browser client spec:** the highest-numbered `docs/frontend/revisionN.md` is current (same revision workflow); code lives in `web/`. It must not require server behaviour the server spec and ADRs do not specify.
- **ADRs:** `docs/adr/` (index in `docs/adr/README.md`). Accepted ADRs are binding; a spec revision must not contradict them, and changing one needs a new ADR that supersedes it.
- **Phase:** v1 is implemented. A change to behaviour needs a spec revision or ADR first (see above); start new work only when the owner asks.
- Ask the user instead of assuming; the user prefers many clarifying questions.
