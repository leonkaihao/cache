package model

import (
	"context"
	"time"
)

// CacheTimeline provides time-indexed state storage where multiple versions
// of state coexist at different timestamps. It supports sparse field updates,
// out-of-order insertion, and time-based queries.
type CacheTimeline interface {
	// Name returns the timeline name.
	Name() string

	// Append adds or updates fields at the specified timestamp.
	// Optimized for chronological writes (ts >= last timestamp).
	// If force is false, returns error if any field already exists at timestamp.
	// If force is true, overwrites existing fields.
	Append(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error

	// Insert adds or updates fields at the specified timestamp.
	// Supports out-of-order writes at any historical position.
	// If force is false, returns error if any field already exists at timestamp.
	// If force is true, overwrites existing fields.
	Insert(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error

	// GetAt returns the complete state at or before the specified timestamp.
	// State is reconstructed by merging all field updates up to ts.
	GetAt(ctx context.Context, key string, ts time.Time) (map[string]string, error)

	// GetExact returns the raw sparse data at the exact timestamp.
	// Returns error if no data exists at the exact timestamp.
	GetExact(ctx context.Context, key string, ts time.Time) (map[string]string, error)

	// GetRange returns all complete states in the time range [start, end].
	// Each TimeValue contains a complete state (merged from history).
	GetRange(ctx context.Context, key string, start, end time.Time) ([]TimeValue, error)

	// GetLatest returns the complete state at the most recent timestamp.
	GetLatest(ctx context.Context, key string) (map[string]string, error)

	// Timeline returns all complete states for the key in chronological order.
	Timeline(ctx context.Context, key string) ([]TimeValue, error)

	// GetAffectedRange returns all states from insertedAt (inclusive) to end of timeline.
	// Used for recomputation after historical insertion.
	GetAffectedRange(ctx context.Context, key string, insertedAt time.Time) ([]TimeValue, error)

	// SetRetention sets the retention policy for the timeline.
	// Policy applies to all keys unless overridden with SetKeyRetention.
	SetRetention(policy RetentionPolicy) error

	// SetKeyRetention sets the retention policy for a specific key.
	// Overrides the timeline's default retention policy.
	SetKeyRetention(key string, policy RetentionPolicy) error

	// GetRetention returns the timeline's default retention policy.
	GetRetention() RetentionPolicy

	// GetKeyRetention returns the retention policy for a specific key.
	// Returns timeline's default policy if no key-specific policy is set.
	GetKeyRetention(key string) RetentionPolicy

	// Keys returns all keys that have been written to the timeline.
	Keys(ctx context.Context) ([]string, error)

	// Remove removes the specified keys from the timeline.
	Remove(ctx context.Context, keys []string) error

	// Clear removes all data from the timeline but keeps the timeline instance.
	Clear(ctx context.Context) error

	// Delete removes the timeline instance from the client.
	Delete(ctx context.Context) error
}

// TimeValue represents a moment in time with its associated complete state.
type TimeValue struct {
	Time  time.Time
	Value map[string]string
}

// RetentionPolicy defines automatic data lifecycle management rules.
type RetentionPolicy struct {
	MaxCount    int               // Maximum number of time points per key (0 = unlimited)
	MaxDuration time.Duration     // Maximum age of time points (0 = unlimited)
	Strategy    RetentionStrategy // Strategy for applying count and duration constraints
}

// RetentionStrategy defines how retention boundaries are calculated.
type RetentionStrategy int

const (
	// RetentionMax keeps points that satisfy EITHER count OR duration constraint (safer, keeps more data).
	RetentionMax RetentionStrategy = iota
	// RetentionMin keeps points that satisfy BOTH count AND duration constraints (aggressive, keeps less data).
	RetentionMin
)
