# ADR-0001: Split Timeline Interface into Data and Labels

**Status:** Accepted

**Date:** 2026-08-09

## Context

The original `CacheTimeline` interface had 18 methods covering data operations (Append, Insert, GetAt, etc.), label management (Keys, AddKeyLabels, etc.), and configuration (WithRetention, GetRetention). This created a shallow module — callers had to understand all concerns to use any part correctly.

The architecture review identified Timeline as the largest module (1,867 LOC across backends) and a recent performance hot spot. The 18-method surface made testing cumbersome and changes expensive.

## Decision

We split the `CacheTimeline` interface into three composed interfaces:

1. **`TimelineData`** — 13 methods covering data operations (write, query, lifecycle)
2. **`TimelineLabels`** — 4 methods covering label management
3. **`CacheTimeline`** — embeds both interfaces plus options management (WithOptions, GetOptions)

**Key choices:**
- Interface composition, not implementation splitting — mem/redis implementations remain unified
- `GetUpdatedKeys` stays in `TimelineData` (it's a data query, not label filtering)
- Replaced `WithRetention`/`GetRetention` with `WithOptions`/`GetOptions` using `TimelineOptions` struct for future extensibility
- No backward compatibility — this is a breaking change

## Alternatives Considered

### Alternative 1: Split implementations into sub-modules
Create separate `TimelineCore`, `TimelineLabels`, `TimelineRetention` modules with their own implementations.

**Rejected because:**
- Higher risk — moves code, not just types
- Requires coordinating state across multiple structs
- Interface split achieves the immediate goal (better testability, clearer contracts) without implementation risk

### Alternative 2: Keep single interface, add convenience wrappers
Keep `CacheTimeline` as-is, provide typed wrappers like `NewTimelineData(tl CacheTimeline) TimelineData`.

**Rejected because:**
- Doesn't reduce the interface surface callers see
- Tests still cross the full 18-method interface
- Adds indirection without improving depth

### Alternative 3: Preserve backward compatibility via deprecated methods
Keep `WithRetention`/`GetRetention` as sugar over `WithOptions`/`GetOptions`.

**Rejected because:**
- This is early in the library's life; breaking now is cheaper than later
- Two ways to do the same thing fragments usage patterns

## Consequences

**Positive:**
- Tests can target `TimelineData` or `TimelineLabels` interfaces independently
- Future options (e.g., compression, indexing) extend `TimelineOptions` cleanly
- Callers can accept just `TimelineData` if they don't use labels, making dependencies explicit

**Negative:**
- Breaking change requires users to update `WithRetention` → `WithOptions`
- Interface composition adds one level of indirection in godoc

**Future opportunities:**
- If label operations grow complex, their implementation can split out without changing the interface
- `TimelineOptions` can accumulate more configuration without touching method signatures
