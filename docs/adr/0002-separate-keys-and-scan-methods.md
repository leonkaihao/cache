# ADR-0002: Keep Label-Based Keys and Pattern-Based Scan Separate

**Status:** Accepted

**Date:** 2026-01-09

## Context

Both `CacheBucket.Keys(FilterOptions)` and `CacheBucket.Scan(pattern)` return `[]string` of keys. This raises the question: why not merge them into a single method? For example, adding `KeyPattern string` to `FilterOptions` and deprecating `Scan()`.

The two methods use fundamentally different algorithms with different performance characteristics:

- **`Keys(FilterOptions)`** — Index-based lookup via label sets. O(matching_keys). Fast when you have many keys but selective labels.
- **`Scan(pattern)`** — Full key iteration with pattern matching. O(total_keys). Necessary for key-syntax filtering, but expensive at scale.

## Decision

Keep `Keys()` and `Scan()` as separate methods. Do not merge them or add pattern-matching to `FilterOptions`.

**Key choices:**
- `Keys(FilterOptions)` handles semantic filtering (labels, future: time ranges, metadata)
- `Scan(pattern)` handles syntactic filtering (key patterns)
- Users must choose the right method based on their use case
- Combining both filters requires two calls: label filter first (fast), then pattern match in application code

## Alternatives Considered

### Alternative 1: Merge into Keys(FilterOptions) with optional KeyPattern field
Add `KeyPattern string` to `FilterOptions`, make `Keys()` handle both cases.

**Rejected because:**
- Hides the performance difference — users won't know if they're triggering O(labels) or O(keys)
- Pattern matching can't benefit from label indices (must scan anyway)
- Forces all implementations to support pattern matching in the same call path

### Alternative 2: Smart query optimizer
Detect when both label filter and pattern are present, automatically choose best strategy.

**Rejected because:**
- Over-engineered for current needs
- Query planning complexity belongs in databases, not a cache library
- Explicit methods make performance characteristics clear

## Consequences

**Positive:**
- API clearly signals algorithmic complexity — `Keys()` is index-based, `Scan()` is full-scan
- Users are forced to think about performance when choosing methods
- Each method can optimize for its specific use case

**Negative:**
- No single call for "keys matching pattern AND labels" — users must compose operations
- API has two methods for "get keys" which might confuse newcomers

**Guidance for users:**

```go
// Fast: Use indexed label lookup when possible
keys := bucket.Keys(ctx, model.FilterOptions{
    LabelFilter: [][]string{{"sensor", "outdoor"}},
})

// Slow: Full key scan when pattern matching is needed  
keys := bucket.Scan(ctx, "device_west_*")

// Combined: Filter by label first (fast), then pattern in code
keys := bucket.Keys(ctx, model.FilterOptions{
    LabelFilter: [][]string{{"sensor"}},
})
filtered := filterByPattern(keys, "device_west_*")
```

**Design principle:** Make the slow path explicit. If an operation is O(n), the API should make that obvious.
