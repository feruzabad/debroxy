# 0019. Observations of tracked failed or gone items are not leftovers

| | |
|---|---|
| Status | Accepted (2026-10-12, project owner) |
| Date | 2026-10-12 |
| Deciders | project owner |
| Spec | revision 10, §5.6 steps 4 and 6; changelog C10-04; decision DEC-10 |
| Related | amends ADR 0009 rule 18 (refresh pipeline, leftovers) |

## Context

ADR 0009 rule 16 revives a `failed` or `gone` item only when its provider item reappears **active or ready**. Rule 18 then sends every remaining observation to the Unmanaged Items read model.

A provider item that a `failed` item still holds, and that the provider still reports as failed (or in any other state that is neither active nor ready), therefore becomes an "unmanaged item" in the admin list. DEC-10 defines Unmanaged Items as provider items that no debroxy item tracks. The admin would see a tracked item listed as unmanaged on every refresh, and could mistake it for clutter left outside debroxy.

## Decision

1. An observation whose provider item ID is held by a `failed` or `gone` item and whose state is neither active nor ready is **dropped as tracked**: it changes nothing and is not a leftover.
2. A **blocked revive** keeps the routing of ADR 0009 rule 16: dropped when a `deleting` item holds the provider item ID, otherwise a leftover.
3. ADR 0009 rule 18 therefore reads: remaining observations that **no debroxy item holds** (and blocked revives per rule 2) → Unmanaged Items. All other rules of ADR 0009 are unchanged.

## Consequences

### Positive
- The Unmanaged Items list shows only what DEC-10 means: provider items debroxy does not track.

### Negative
- A failed item whose provider item stays failed is visible only through its jobs (state `failed`), not in the admin's unmanaged list. When its last claim is released, `RequestDeletion` deletes the provider item as before.

### Risks and mitigations
- *Risk:* a provider item that stays failed is never cleaned up. *Mitigation:* releasing the last claim deletes it (ADR 0004 rule 12, ADR 0008).

## Alternatives considered

**Follow ADR 0009 rule 18 literally.** Rejected: the admin list would show tracked items as unmanaged, against DEC-10.

## Compliance
- Pipeline test: a `failed` item whose provider item is listed as failed is neither changed nor recorded as unmanaged; a revive blocked by a claimable item still produces an unmanaged item.
