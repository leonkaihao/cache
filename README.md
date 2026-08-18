# Cache

[![CI](https://github.com/leonkaihao/cache/actions/workflows/ci.yml/badge.svg)](https://github.com/leonkaihao/cache/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/leonkaihao/cache/v2)](https://goreportcard.com/report/github.com/leonkaihao/cache/v2)
[![codecov](https://codecov.io/gh/leonkaihao/cache/branch/master/graph/badge.svg)](https://codecov.io/gh/leonkaihao/cache)
[![Bencher](https://img.shields.io/badge/Performance-Bencher-blue)](https://bencher.dev/perf/project-leonkaihao-cache)
[![Go Reference](https://pkg.go.dev/badge/github.com/leonkaihao/cache/v2.svg)](https://pkg.go.dev/github.com/leonkaihao/cache/v2)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A flexible and type-safe caching library for Go with support for both in-memory and Redis backends.

## Features

- **Multiple Backend Support**: In-memory cache and Redis cache implementations
- **Type-Safe API**: Generic-based bucket operations for type safety
- **Context Support**: Full context.Context integration for cancellation and timeouts
- **Production-Safe Error Handling**: All operations return errors instead of panicking
- **Label-Based Filtering**: Organize and query cached items using labels
- **Time-Based Updates**: Conditional updates based on timestamps
- **Expiration Support**: Built-in TTL and expiration callbacks
- **Collections**: Manage sets of members associated with keys
- **Timeline**: Time-indexed state storage with **per-field time series**, sparse field updates, out-of-order insertion support, and **per-field retention policies** (v3.0+)
- **Configurable Timeouts**: Per-client timeout configuration for Redis operations

## Installation

```bash
go get github.com/leonkaihao/cache/v2@v2
```

## Quick Start

### In-Memory Cache

```go
package main

import (
    "context"
    "log"
    "time"
    cache "github.com/leonkaihao/cache/v2/pkg/client/mem"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
    Age  int    `json:"age"`
}

func main() {
    ctx := context.Background()
    
    // Create client
    cli := cache.NewClient()
    
    // Create bucket with error handling
    userBkt, err := cache.NewBucket[User](cli, "users")
    if err != nil {
    log.Fatal(err)
}
```

### Timeline for Time-Series State Management

```go
ctx := context.Background()

// Create timeline
timeline := cli.Timeline("device_states")

// Set retention policy (config-driven, in-memory)
timeline.WithOptions(model.TimelineOptions{
    Retention: model.RetentionPolicy{
        MaxCount:    100,
        MaxDuration: 2 * time.Hour,
        Strategy:    model.RetentionMax,
    },
})

// Record device state
if err := timeline.Append(ctx, "device_A", time.Now(), map[string]string{
    "zones":   "Z1,Z3",
    "beacons": "B5",
    "battery": "85",
}, false); err != nil {
    log.Fatal(err)
}

// Sparse update (only zones changed)
if err := timeline.Append(ctx, "device_A", time.Now().Add(5*time.Minute), map[string]string{
    "zones": "Z1,Z3,Z5",
}, false); err != nil {
    log.Fatal(err)
}

// Query current state (merged from all updates)
states, err := timeline.GetLatest(ctx, []string{"device_A"}, model.QueryOptions{})
if err != nil {
    log.Fatal(err)
}
state := states[0]
log.Printf("Current state: zones=%s, beacons=%s, battery=%s\n",
    state["zones"].Value, state["beacons"].Value, state["battery"].Value)
// Output: zones=Z1,Z3,Z5, beacons=B5, battery=85

// Query historical state (10 minutes ago)
historicalStates, err := timeline.GetAt(ctx, []string{"device_A"}, time.Now().Add(-10*time.Minute), model.QueryOptions{})
if err != nil {
    log.Fatal(err)
}
historicalState := historicalStates[0]
log.Printf("Historical: zones=%s (updated at %v)\n",
    historicalState["zones"].Value, historicalState["zones"].Time)

// Insert out-of-order event
lateEvent := time.Now().Add(-1 * time.Hour)
if err := timeline.Insert(ctx, "device_A", lateEvent, map[string]string{
    "zones": "Z1,Z2",
}, false); err != nil {
    log.Fatal(err)
}

// Find affected states for recomputation
affected, err := timeline.GetAffectedRange(ctx, "device_A", lateEvent, model.QueryOptions{})
if err != nil {
    log.Fatal(err)
}
log.Printf("States needing recomputation: %d\n", len(affected))

// Query only specific fields
filteredStates, err := timeline.GetLatest(ctx, []string{"device_A"}, model.QueryOptions{
    Fields: []string{"zones", "battery"}, // only fetch these fields
})
if err != nil {
    log.Fatal(err)
}

// Find keys updated after a timestamp
recentKeys, err := timeline.Keys(ctx, model.FilterOptions{
    AfterTs: &lateEvent, // only keys updated after this time
})
if err != nil {
    log.Fatal(err)
}
log.Printf("Recently updated keys: %v\n", recentKeys)
```

### Redis Cache

```go
package main

import (
    "context"
    "log"
    "time"
    cache "github.com/leonkaihao/cache/v2/pkg/client/redis"
    "github.com/leonkaihao/cache/v2/pkg/coding"
)

type Product struct {
    ID    string  `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}

func main() {
    ctx := context.Background()
    
    // Create Redis client with custom timeout
    cli := cache.NewClient(
        "localhost:6379", 
        "password", 
        0,
        cache.WithTimeout(5*time.Second), // optional: custom timeout
    )
    
    // Create bucket with error handling
    productBkt, err := cache.NewBucket[Product](
        cli, 
        "products",
        coding.NewJsonCoder(),
    )
    if err != nil {
        log.Fatal(err)
    }
    cli.WithBucket(productBkt)
    
    // Update document with context
    doc, err := productBkt.Update(ctx, "prod1", &Product{
        ID:    "p001",
        Name:  "Laptop",
        Price: 999.99,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Created product: %s\n", doc.Key())
}
```

## Context and Timeout Management

All operations accept a `context.Context` for cancellation and timeouts:

```go
ctx := context.Background()

// Use context with timeout
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

doc, err := bucket.Update(ctx, "key1", data)
if err != nil {
    // Handle timeout or cancellation
    log.Printf("Operation failed: %v", err)
}

// Redis client has default timeout (1 second)
// You can customize it:
cli := cache.NewClient("localhost:6379", "pass", 0, 
    cache.WithTimeout(5*time.Second))

// Context deadline takes precedence over default timeout
// If context has shorter deadline, it will be used
shortCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
defer cancel()
_, err = bucket.Update(shortCtx, "key", data) // Uses 100ms timeout
```

## API Overview

### CacheClient

The main client interface for managing buckets and collections:

```go
type CacheClient interface {
    WithBucket(CacheBucket) CacheBucket
    Bucket(name string) CacheBucket
    Buckets() []CacheBucket
    RemoveBucket(bktName string)
    
    Collection(name string) CacheCollection
    Collections() []CacheCollection
    RemoveCollection(name string)
    
    Timeline(name string) CacheTimeline
    Timelines() []CacheTimeline
    RemoveTimeline(name string) error
}
```

### CacheBucket

Type-safe storage for cached objects (all methods accept context and return errors):

```go
type CacheBucket interface {
    Name() string
    Docs(ctx context.Context, keys []string) ([]CacheDoc, error)
    Values(ctx context.Context, keys []string) ([]any, error)
    Update(ctx context.Context, key string, data any) (CacheDoc, error)
    UpdateWithTs(ctx context.Context, key string, data any, ts time.Time) (CacheDoc, bool, error)
    Filter(ctx context.Context, labelFilters ...[]string) ([]string, error)
    Scan(ctx context.Context, match string) ([]string, error)
    Remove(ctx context.Context, keys []string) error
    Clear(ctx context.Context) error
    Delete(ctx context.Context) error
}
```

### CacheDoc

Individual cached document with metadata:

```go
type CacheDoc interface {
    Key() string
    Val(ctx context.Context) (any, error)
    SetValue(ctx context.Context, val any) error
    Labels(ctx context.Context) (LabelSet, error)
    AddLabels(ctx context.Context, labels []string) error
    RemoveLabels(ctx context.Context, labels []string) error
    Delete(ctx context.Context) error
    WithTime(ctx context.Context, ts time.Time) error
    SetValueWithTs(ctx context.Context, val any, ts time.Time) (bool, error)
    Time(ctx context.Context) (time.Time, error)
    Expire(d time.Duration, onExpire func(CacheDoc)) error
    CancelExpire() error
}
```

### CacheCollection

Manage sets of members:

```go
type CacheCollection interface {
    Name() string
    Keys(ctx context.Context) ([]string, error)
    Add(ctx context.Context, key string, members []string) error
    Remove(ctx context.Context, key string, members []string) error
    MembersMap(ctx context.Context, key string) (MembersMap, error)
    MembersMaps(ctx context.Context, keys []string) ([]MembersMap, error)
    Clear(ctx context.Context, key string) error
    ClearAll(ctx context.Context) error
    Delete(ctx context.Context) error
}
```

### CacheTimeline

Time-indexed state storage for managing historical data. The interface is composed of:

- **TimelineData** — data operations (write, query, lifecycle)
- **TimelineLabels** — label management and filtering
- **Options** — configuration management

```go
type TimelineData interface {
    Name() string
    
    // Write operations
    Append(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error
    Insert(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error
    
    // Batch query operations (results parallel to keys, nil = no data, BatchError = partial failure)
    // Returns map[string]*FieldTimeValue per key, where each field has its own timestamp
    GetAt(ctx context.Context, keys []string, ts time.Time, opts QueryOptions) ([]map[string]*FieldTimeValue, error)
    GetRange(ctx context.Context, keys []string, start, end time.Time, opts QueryOptions) ([][]*TimeValue, error)
    GetLatest(ctx context.Context, keys []string, opts QueryOptions) ([]map[string]*FieldTimeValue, error)
    
    // Timeline returns per-field time series: map[fieldName][]*FieldTimeValue
    Timeline(ctx context.Context, key string, opts QueryOptions) (map[string][]*FieldTimeValue, error)
    GetAffectedRange(ctx context.Context, key string, insertedAt time.Time, opts QueryOptions) ([]*TimeValue, error)
    
    // Keys supports label-based filtering and timestamp filtering
    Keys(ctx context.Context, opt FilterOptions) ([]string, error)
    
    // Management
    Remove(ctx context.Context, keys []string) error
    Clear(ctx context.Context) error
    Delete(ctx context.Context) error
}

type TimelineLabels interface {
    AddKeyLabels(ctx context.Context, key string, labels []string) error
    RemoveKeyLabels(ctx context.Context, key string, labels []string) error
    KeyLabels(ctx context.Context, key string) (LabelSet, error)
}

type CacheTimeline interface {
    TimelineData
    TimelineLabels
    
    // Options management (config-driven, in-memory)
    WithOptions(opts TimelineOptions) CacheTimeline
    GetOptions() TimelineOptions
}
```

#### Timeline Types (v3.0+)

```go
// FieldTimeValue represents a single field's value with its timestamp
type FieldTimeValue struct {
    Value string    // Field value
    Time  time.Time // When this field was last updated
}

// QueryOptions controls which fields to retrieve and how to filter
type QueryOptions struct {
    Fields []string // If non-empty, only fetch these fields
}

// FilterOptions for Keys() method
type FilterOptions struct {
    LabelFilter [][]string // OR within array, AND between arrays
    AfterTs     *time.Time  // Only keys updated after this timestamp
}

// TimeValue represents a complete state snapshot at a point in time
type TimeValue struct {
    Time   time.Time
    Values map[string]*FieldTimeValue // Field name -> value with timestamp
}
```

**Key Changes in v3.0**:
- Each field maintains its own timestamp (data freshness visibility)
- Per-field retention (low-frequency fields no longer lost)
- Field filtering via `QueryOptions.Fields` (fetch only what you need)
- Time-based key filtering via `FilterOptions.AfterTs` (replaces `GetUpdatedKeys`)
- `Timeline()` returns per-field time series instead of merged snapshots
- Removed methods: `GetExact()`, `GetUpdatedKeys()` (use `Keys()` with `AfterTs`)

## Advanced Features

### Time-Based Updates

Only update cache if the new data is newer (equal timestamps are rejected):

```go
ctx := context.Background()
ts := time.Now()

// First update
doc, updated, err := bucket.UpdateWithTs(ctx, "key1", data, ts)
if err != nil {
    log.Fatal(err)
}
log.Printf("Updated: %v", updated) // true

// Try to update with same timestamp (rejected)
_, updated, err = bucket.UpdateWithTs(ctx, "key1", newData, ts)
if err != nil {
    log.Fatal(err)
}
log.Printf("Updated: %v", updated) // false - equal timestamp rejected

// Update with newer timestamp (succeeds)
newerTs := ts.Add(time.Second)
_, updated, err = bucket.UpdateWithTs(ctx, "key1", newerData, newerTs)
if err != nil {
    log.Fatal(err)
}
log.Printf("Updated: %v", updated) // true
```

### Expiration with Callbacks

```go
ctx := context.Background()

doc, err := bucket.Update(ctx, "session", sessionData)
if err != nil {
    log.Fatal(err)
}

err = doc.Expire(time.Hour, func(d model.CacheDoc) {
    log.Printf("Session expired: %s", d.Key())
    _ = d.Delete(context.Background())
})
if err != nil {
    log.Fatal(err)
}

// Cancel expiration if needed
err = doc.CancelExpire()
if err != nil {
    log.Fatal(err)
}
```

### Label-Based Filtering

```go
ctx := context.Background()

// Add labels
if err := doc1.AddLabels(ctx, []string{"active", "premium"}); err != nil {
    log.Fatal(err)
}
if err := doc2.AddLabels(ctx, []string{"active", "free"}); err != nil {
    log.Fatal(err)
}

// Filter by single label
activeKeys, err := bucket.Filter(ctx, []string{"active"})
if err != nil {
    log.Fatal(err)
}
// Returns both doc1 and doc2

// Filter by multiple labels (OR within array, AND between arrays)
premiumKeys, err := bucket.Filter(ctx, []string{"premium", "free"})
if err != nil {
    log.Fatal(err)
}
// Returns doc1 and doc2

// Check labels
labels, err := doc1.Labels(ctx)
if err != nil {
    log.Fatal(err)
}
hasActive := labels.CheckAnd([]string{"active", "premium"}) // true
hasTrial := labels.CheckOr([]string{"active", "trial"})    // true
```

### Collections for Set Operations

```go
ctx := context.Background()
clt := cli.Collection("user_groups")

// Add members to sets (empty members rejected)
if err := clt.Add(ctx, "admins", []string{"user1", "user2"}); err != nil {
    log.Fatal(err)
}
// Merges with existing
if err := clt.Add(ctx, "admins", []string{"user2", "user3"}); err != nil {
    log.Fatal(err)
}

// Check membership
members, err := clt.MembersMap(ctx, "admins")
if err != nil {
    log.Fatal(err)
}
if members != nil {
    exists := members.Exists("user1") // true
    list := members.List()            // ["user1", "user2", "user3"]
}

// Remove members
if err := clt.Remove(ctx, "admins", []string{"user2"}); err != nil {
    log.Fatal(err)
}
```

### Timeline Batch Queries and Labels

Timeline supports batch queries (multiple keys in one call) and label-based filtering for efficient time-series operations.

#### Batch Queries

Query multiple keys at once for better performance:

```go
ctx := context.Background()
timeline := cli.Timeline("device_states")

// Query multiple devices at once
states, err := timeline.GetLatest(ctx, []string{"device_A", "device_B", "device_C"}, model.QueryOptions{})
if err != nil {
    log.Fatal(err)
}

// Results are parallel to input keys
// nil at position i means key i has no data (not an error)
for i, key := range []string{"device_A", "device_B", "device_C"} {
    if states[i] == nil {
        log.Printf("%s: no data", key)
    } else {
        log.Printf("%s: battery=%s zones=%s", key, 
            states[i]["battery"].Value, states[i]["zones"].Value)
    }
}

// Batch historical queries
ts := time.Now().Add(-1 * time.Hour)
historicalStates, err := timeline.GetAt(ctx, []string{"device_A", "device_B"}, ts, model.QueryOptions{})
if err != nil {
    log.Fatal(err)
}

// Batch range queries return [][]*TimeValue
// Outer nil = key has no data in range
// Inner slice elements are always non-nil pointers
start := time.Now().Add(-24 * time.Hour)
end := time.Now()
ranges, err := timeline.GetRange(ctx, []string{"device_A", "device_B"}, start, end)
if err != nil {
    log.Fatal(err)
}
for i, key := range []string{"device_A", "device_B"} {
    if ranges[i] == nil {
        log.Printf("%s: no data in range", key)
    } else {
        log.Printf("%s: %d time points in range", key, len(ranges[i]))
        for _, tv := range ranges[i] {
            log.Printf("  %v: battery=%s", tv.Time, tv.Value["battery"])
        }
    }
}
```

#### Label-Based Key Filtering

Organize timeline keys with labels for semantic grouping and filtering:

```go
ctx := context.Background()
timeline := cli.Timeline("device_states")

// Add labels to categorize devices
if err := timeline.AddKeyLabels(ctx, "device_A", []string{"sensor", "outdoor", "region-west"}); err != nil {
    log.Fatal(err)
}
if err := timeline.AddKeyLabels(ctx, "device_B", []string{"sensor", "indoor", "region-west"}); err != nil {
    log.Fatal(err)
}
if err := timeline.AddKeyLabels(ctx, "device_C", []string{"actuator", "outdoor", "region-east"}); err != nil {
    log.Fatal(err)
}

// Query labels for a key
labels, err := timeline.KeyLabels(ctx, "device_A")
if err != nil {
    log.Fatal(err)
}
hasSensor := labels.CheckAnd([]string{"sensor", "outdoor"}) // true
hasIndoor := labels.CheckOr([]string{"indoor", "outdoor"})  // true

// Filter keys by labels (OR within array, AND between arrays)
// All keys with no filter
allKeys, err := timeline.Keys(ctx, model.FilterOptions{})

// Keys with "outdoor" label
outdoorKeys, err := timeline.Keys(ctx, model.FilterOptions{
    LabelFilter: [][]string{{"outdoor"}},
})
// Returns: ["device_A", "device_C"]

// Keys with "sensor" OR "actuator" label
deviceKeys, err := timeline.Keys(ctx, model.FilterOptions{
    LabelFilter: [][]string{{"sensor", "actuator"}},
})
// Returns: ["device_A", "device_B", "device_C"]

// Keys matching (outdoor OR indoor) AND region-west AND sensor
westSensors, err := timeline.Keys(ctx, model.FilterOptions{
    LabelFilter: [][]string{
        {"outdoor", "indoor"},  // OR: any of these
        {"region-west"},         // AND this
        {"sensor"},              // AND this
    },
})
// Returns: ["device_A", "device_B"]

// Combine label filtering with batch queries
states, err := timeline.GetLatest(ctx, westSensors, model.QueryOptions{})
if err != nil {
    log.Fatal(err)
}
for i, key := range westSensors {
    if states[i] != nil {
        log.Printf("%s: battery=%s", key, states[i]["battery"].Value)
    }
}

// Remove labels
if err := timeline.RemoveKeyLabels(ctx, "device_A", []string{"outdoor"}); err != nil {
    log.Fatal(err)
}
```

#### Partial Failure Handling with BatchError

Batch operations return `*BatchError` for partial failures, allowing you to use successful results even when some keys fail:

```go
import "errors"

ctx := context.Background()
timeline := cli.Timeline("device_states")

deviceIDs := []string{"device_A", "device_B", "device_C", "device_D"}
states, err := timeline.GetLatest(ctx, deviceIDs, model.QueryOptions{})

if err != nil {
    var batchErr *model.BatchError
    if errors.As(err, &batchErr) {
        // Partial failure - some devices succeeded
        log.Printf("Retrieved %d/%d devices successfully", 
            batchErr.Total-batchErr.Failed, batchErr.Total)
        
        // Process successful results (nil = no data, which is not an error)
        for i, state := range states {
            if state != nil {
                log.Printf("%s: %v", deviceIDs[i], state)
            } else if _, failed := batchErr.KeyErrors[deviceIDs[i]]; !failed {
                log.Printf("%s: no data available", deviceIDs[i])
            }
        }
        
        // Handle failed devices
        for key, keyErr := range batchErr.KeyErrors {
            log.Printf("Device %s failed: %v", key, keyErr)
        }
    } else {
        // Total failure - no results available
        log.Fatalf("Timeline query failed: %v", err)
    }
} else {
    // Complete success - process all results
    for i, state := range states {
        if state != nil {
            log.Printf("%s: %v", deviceIDs[i], state)
        } else {
            log.Printf("%s: no data", deviceIDs[i])
        }
    }
}
```

#### Understanding Nil in Batch Results

```go
// GetLatest/GetAt: []map[string]*FieldTimeValue
states, err := timeline.GetLatest(ctx, []string{"key1", "key2"}, model.QueryOptions{})
// states[i] == nil → "key i has no data" (NOT an error, just no time points exist)
// err != nil && errors.As(err, &BatchError{}) → partial failure (check KeyErrors for which keys failed)

// GetRange: [][]*TimeValue
ranges, err := timeline.GetRange(ctx, []string{"key1", "key2"}, start, end, model.QueryOptions{})
// ranges[i] == nil → "key i has no time points in the range"
// ranges[i][j] → Always non-nil if ranges[i] != nil (pointers avoid copying)
```

#### Query Keys by Update Time

Find keys that were updated after a specific timestamp:

```go
ctx := context.Background()
timeline := cli.Timeline("device_states")

// Get all keys updated in the last hour (v3.0+)
lastHour := time.Now().Add(-1 * time.Hour)
recentKeys, err := timeline.Keys(ctx, model.FilterOptions{
    AfterTs: &lastHour,
})
if err != nil {
    log.Fatal(err)
}
log.Printf("Keys updated since %v: %v", lastHour, recentKeys)

// Query only recently updated devices
if len(recentKeys) > 0 {
    states, err := timeline.GetLatest(ctx, recentKeys, model.QueryOptions{})
    if err != nil {
        log.Fatal(err)
    }
    // Process recent device states
    for i, key := range recentKeys {
        if states[i] != nil {
            log.Printf("%s: battery=%s (updated at %v)", 
                key, states[i]["battery"].Value, states[i]["battery"].Time)
        }
    }
}
```

## Error Handling Philosophy

**v2.0.0 is production-safe**: All operations return errors instead of panicking or using `Logger.Fatal()`. This allows your application to:

1. **Gracefully handle failures** - No unexpected crashes
2. **Implement retry logic** - Wrap operations in your own retry mechanism
3. **Log errors appropriately** - Use your application's logging system
4. **Test error paths** - Write tests that verify error handling

```go
ctx := context.Background()

// Always check errors
doc, err := bucket.Update(ctx, "key1", data)
if err != nil {
    // Handle error appropriately
    log.Printf("Failed to update cache: %v", err)
    // Optionally retry, use fallback, or propagate error
    return fmt.Errorf("cache update failed: %w", err)
}

// Context cancellation is detected
ctx, cancel := context.WithCancel(context.Background())
cancel() // Cancel immediately

err = doc.AddLabels(ctx, []string{"label1"})
if errors.Is(err, context.Canceled) {
    log.Println("Operation was cancelled")
}
```

## Migration Guide (v1.x → v2.0.0)

### Breaking Changes

1. **All operations now accept `context.Context` as first parameter**
2. **All operations now return `error`**
3. **`SetValueWithTs` returns `(bool, error)` instead of `(CacheDoc, bool)`**
4. **`UpdateWithTs` returns `(CacheDoc, bool, error)` instead of `(CacheDoc, bool)`**
5. **`Remove` returns `error` instead of `[]CacheDoc`**
6. **`GetLastErrors()` removed** - errors are returned directly
7. **Empty members in `Collection.Add()` now rejected**
8. **Redis client accepts `WithTimeout()` option**
9. **NewBucket returns `(CacheBucket, error)` for both backends**

### Migration Examples

#### Before (v1.x):
```go
cli := mem.NewClient()
bucket := mem.NewBucket[User](cli, "users")
doc := bucket.Update("key1", data)
doc.AddLabels([]string{"active"})
keys := bucket.Filter([]string{"active"})
bucket.Clear()
```

#### After (v2.0.0):
```go
ctx := context.Background()
cli := mem.NewClient()
bucket, err := mem.NewBucket[User](cli, "users")
if err != nil {
    return err
}
doc, err := bucket.Update(ctx, "key1", data)
if err != nil {
    return err
}
if err := doc.AddLabels(ctx, []string{"active"}); err != nil {
    return err
}
keys, err := bucket.Filter(ctx, []string{"active"})
if err != nil {
    return err
}
if err := bucket.Clear(ctx); err != nil {
    return err
}
```

#### Redis Timeout Configuration:
```go
// Before (v1.x): No timeout configuration
cli := redis.NewClient("localhost:6379", "pass", 0)

// After (v2.0.0): Optional timeout configuration
cli := redis.NewClient("localhost:6379", "pass", 0, 
    redis.WithTimeout(5*time.Second)) // default is 1s
```

## Testing

```bash
# Run unit tests
make test

# Run integration tests (requires Redis)
make test/integration

# Run benchmarks
make test/bench

# Run specific test
go test ./pkg/client/mem/... -run TestBucket -v
```

## Project Structure

```
cache/
├── cmd/
│   ├── sample-mem/      # In-memory cache example
│   └── sample-redis/    # Redis cache example
├── pkg/
│   ├── client/
│   │   ├── mem/         # In-memory implementation
│   │   ├── redis/       # Redis implementation
│   │   └── test/        # Shared test suite
│   ├── model/           # Core interfaces
│   ├── coding/          # Encoding/decoding utilities
│   ├── consts/          # Constants
│   └── logger/          # Logging interfaces
└── Makefile
```

## Requirements

- Go 1.23 or higher
- Redis server (for Redis backend)

## Dependencies

- [go-redis/v9](https://github.com/redis/go-redis) - Redis client for Go
- [protobuf](https://github.com/golang/protobuf) - Protocol buffer support
- [testify](https://github.com/stretchr/testify) - Testing toolkit

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Examples

Full examples can be found in the `cmd/` directory:

- [`cmd/sample-mem/main.go`](cmd/sample-mem/main.go) - In-memory cache usage with context and error handling
- [`cmd/sample-redis/main.go`](cmd/sample-redis/main.go) - Redis cache usage with timeout configuration
