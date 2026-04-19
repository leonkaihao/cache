# Cache

[![CI](https://github.com/leonkaihao/cache/actions/workflows/ci.yml/badge.svg)](https://github.com/leonkaihao/cache/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/leonkaihao/cache)](https://goreportcard.com/report/github.com/leonkaihao/cache)
[![codecov](https://codecov.io/gh/leonkaihao/cache/branch/master/graph/badge.svg)](https://codecov.io/gh/leonkaihao/cache)
[![Go Reference](https://pkg.go.dev/badge/github.com/leonkaihao/cache.svg)](https://pkg.go.dev/github.com/leonkaihao/cache)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/leonkaihao/cache)](https://github.com/leonkaihao/cache/releases)

A flexible and type-safe caching library for Go with support for both in-memory and Redis backends.

## Features

- **Multiple Backend Support**: In-memory cache and Redis cache implementations
- **Type-Safe API**: Generic-based bucket operations for type safety
- **Label-Based Filtering**: Organize and query cached items using labels
- **Time-Based Updates**: Conditional updates based on timestamps
- **Expiration Support**: Built-in TTL and expiration callbacks
- **Collections**: Manage sets of members associated with keys
- **Extensible Logger**: Custom logging interface for integration with your logging framework

## Installation

```bash
go get github.com/leonkaihao/cache
```

## Quick Start

### In-Memory Cache

```go
package main

import (
    "time"
    cache "github.com/leonkaihao/cache/pkg/client/mem"
    "github.com/leonkaihao/cache/pkg/model"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
    Age  int    `json:"age"`
}

func main() {
    // Create client
    cli := cache.NewClient()
    
    // Create bucket
    userBkt := cache.NewBucket[User](cli, "users")
    cli.WithBucket(userBkt)
    
    // Update/insert document
    doc := userBkt.Update("user1", &User{ID: 1, Name: "Alice", Age: 30})
    
    // Add labels
    doc.AddLabels([]string{"active", "premium"})
    
    // Filter by labels
    keys := userBkt.Filter([]string{"active"})
    users := userBkt.Values(keys) // Get actual User values
}
```

### Redis Cache

```go
package main

import (
    "context"
    cache "github.com/leonkaihao/cache/pkg/client/redis"
    "github.com/redis/go-redis/v9"
)

type Product struct {
    ID    string  `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}

func main() {
    // Create Redis client
    redisClient := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    
    cli := cache.NewClient(context.Background(), redisClient)
    
    // Create bucket with encoding
    productBkt := cache.NewBucket[Product](
        cli, 
        "products",
        cache.WithJsonEncoding[Product](),
    )
    cli.WithBucket(productBkt)
    
    // Update document
    doc := productBkt.Update("prod1", &Product{
        ID:    "p001",
        Name:  "Laptop",
        Price: 999.99,
    })
}
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
}
```

### CacheBucket

Type-safe storage for cached objects:

```go
type CacheBucket interface {
    Name() string
    Docs(keys []string) []CacheDoc
    Values(keys []string) []any
    Update(key string, data any) CacheDoc
    UpdateWithTs(key string, data any, ts time.Time) (CacheDoc, bool)
    Filter(labelFilters ...[]string) []string
    Scan(match string) []string
    Remove(keys []string) []CacheDoc
    Clear()
    Delete()
}
```

### CacheDoc

Individual cached document with metadata:

```go
type CacheDoc interface {
    Key() string
    Val() any
    SetValue(val any) CacheDoc
    Labels() LabelSet
    AddLabels(labels []string) LabelSet
    RemoveLabels(label []string) LabelSet
    Delete()
    WithTime(ts time.Time) CacheDoc
    SetValueWithTs(val any, ts time.Time) (CacheDoc, bool)
    Time() time.Time
    Expire(d time.Duration, onExpire func(CacheDoc))
}
```

### CacheCollection

Manage sets of members:

```go
type CacheCollection interface {
    Name() string
    Keys() []string
    Add(key string, members []string)
    Remove(key string, members []string)
    MembersMap(key string) MemberSet
    Clear(key string)
    ClearAll()
    Delete()
}
```

## Advanced Features

### Time-Based Updates

Only update cache if the new data is newer:

```go
doc, updated := bucket.UpdateWithTs("key1", data, time.Now())
if updated {
    // Data was updated because timestamp was newer
}
```

### Expiration with Callbacks

```go
doc := bucket.Update("session", sessionData)
doc.Expire(time.Hour, func(d model.CacheDoc) {
    log.Printf("Session expired: %s", d.Key())
    d.Delete()
})
```

### Label-Based Filtering

```go
// Add labels
doc1.AddLabels([]string{"active", "premium"})
doc2.AddLabels([]string{"active", "free"})

// Filter by single label
activeKeys := bucket.Filter([]string{"active"}) // Returns both doc1 and doc2

// Filter by multiple labels (OR logic)
premiumKeys := bucket.Filter([]string{"premium", "free"}) // Returns doc1 and doc2

// Check labels
labels := doc1.Labels()
labels.CheckAnd([]string{"active", "premium"}) // true
labels.CheckOr([]string{"active", "trial"})    // true
```

### Collections for Set Operations

```go
clt := cli.Collection("user_groups")

// Add members to sets
clt.Add("admins", []string{"user1", "user2"})
clt.Add("admins", []string{"user2", "user3"}) // Merges with existing

// Check membership
members := clt.MembersMap("admins")
members.Exists("user1") // true
members.List()          // ["user1", "user2", "user3"]

// Remove members
clt.Remove("admins", []string{"user2"})
```

## Testing

```bash
# Run unit tests
make test

# Run integration tests (requires Redis)
make test/integration

# Run benchmarks
make test/bench
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
│   │   └── redis/       # Redis implementation
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

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Examples

Full examples can be found in the `cmd/` directory:

- [`cmd/sample-mem/main.go`](cmd/sample-mem/main.go) - In-memory cache usage
- [`cmd/sample-redis/main.go`](cmd/sample-redis/main.go) - Redis cache usage
