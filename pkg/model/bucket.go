package model

import (
	"context"
	"time"
)

type CacheBucket interface {
	Name() string
	// Docs returns docs parallel to keys. nil at index i means keys[i] not found (not an error).
	// Returns *BatchError if one or more keys encountered real retrieval errors.
	Docs(ctx context.Context, keys []string) ([]CacheDoc, error)
	// Values returns values parallel to keys. nil at index i means keys[i] not found (not an error).
	// Returns *BatchError if one or more keys encountered real retrieval errors.
	Values(ctx context.Context, keys []string) ([]any, error)
	Update(ctx context.Context, key string, data any) (CacheDoc, error)
	// UpdateWithTs return doc, updated flag, and error
	UpdateWithTs(ctx context.Context, key string, data any, ts time.Time) (CacheDoc, bool, error)
	// Keys returns keys matching filter options using indexed lookups (e.g., label indices).
	// Efficient for semantic filtering - O(matching_keys).
	// For key pattern matching, use Scan() instead.
	Keys(ctx context.Context, opt FilterOptions) ([]string, error)
	// Scan returns keys matching the pattern using full key iteration.
	// O(total_keys) - use Keys() with label filters for better performance when possible.
	Scan(ctx context.Context, match string) ([]string, error)
	Remove(ctx context.Context, keys []string) error
	Clear(ctx context.Context) error
	Delete(ctx context.Context) error
}
