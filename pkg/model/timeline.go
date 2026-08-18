package model

import (
	"context"
	"time"
)

// TimelineData provides time-indexed state storage where multiple versions
// of state coexist at different timestamps. It supports sparse field updates,
// out-of-order insertion, and time-based queries.
type TimelineData interface {
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

	// Keys returns all logical keys in the timeline, optionally filtered by labels.
	// Uses indexed lookups - O(matching_keys). Fast even with millions of keys.
	// Empty FilterOptions returns all keys.
	// Labels within a single []string are OR'd together.
	// Multiple []string arrays are AND'd:
	//   FilterOptions{LabelFilter: {{"a","b"}, {"c"}}} returns keys with (a OR b) AND c.
	Keys(ctx context.Context, opt FilterOptions) ([]string, error)

	// GetAt returns the complete merged state at or before ts for each key.
	// Each field in the result includes its actual update timestamp.
	// Results are parallel to keys: results[i] corresponds to keys[i].
	// nil at results[i] means keys[i] has no state at or before ts (not an error).
	// Returns *BatchError if one or more keys encountered retrieval errors.
	GetAt(ctx context.Context, keys []string, ts time.Time, opts QueryOptions) ([]map[string]*FieldTimeValue, error)

	// GetRange returns all complete states in [start, end] for each key.
	// Returns time points where any of the specified fields (or all fields if opts.Fields=nil) has an update.
	// Each TimeValue includes per-field timestamps indicating actual source time of each value.
	// Results are parallel to keys: results[i] corresponds to keys[i].
	// nil at results[i] means keys[i] has no time points in the range (not an error).
	// Non-nil inner slices contain only non-nil *TimeValue pointers.
	// Returns *BatchError if one or more keys encountered retrieval errors.
	GetRange(ctx context.Context, keys []string, start, end time.Time, opts QueryOptions) ([][]*TimeValue, error)

	// GetLatest returns the complete merged state at the most recent timestamp for each key.
	// Each field in the result includes its actual update timestamp.
	// Results are parallel to keys: results[i] corresponds to keys[i].
	// nil at results[i] means keys[i] has no time-series data (not an error).
	// Returns *BatchError if one or more keys encountered retrieval errors.
	GetLatest(ctx context.Context, keys []string, opts QueryOptions) ([]map[string]*FieldTimeValue, error)

	// Timeline returns field-grouped time series for the key.
	// Each field's array contains only its own update points (not interpolated values).
	// Returns a map from field name to chronologically ordered time series.
	Timeline(ctx context.Context, key string, opts QueryOptions) (map[string][]*FieldTimeValue, error)

	// GetAffectedRange returns all states from insertedAt (inclusive) to end of timeline.
	// Used for recomputation after historical insertion.
	// Returns time points where any of the specified fields (or all fields if opts.Fields=nil) changed.
	// Each element is a non-nil *TimeValue pointer.
	GetAffectedRange(ctx context.Context, key string, insertedAt time.Time, opts QueryOptions) ([]*TimeValue, error)

	// Remove removes the specified keys from the timeline.
	Remove(ctx context.Context, keys []string) error

	// Clear removes all data from the timeline but keeps the timeline instance.
	Clear(ctx context.Context) error

	// Delete removes the timeline instance from the client.
	Delete(ctx context.Context) error
}

// TimelineLabels provides label management for timeline keys.
type TimelineLabels interface {
	// AddKeyLabels associates labels with a logical key.
	// Empty strings in labels are ignored. Adding an existing label is a no-op.
	AddKeyLabels(ctx context.Context, key string, labels []string) error

	// RemoveKeyLabels removes labels from a logical key.
	// Removing a non-existent label is a no-op.
	RemoveKeyLabels(ctx context.Context, key string, labels []string) error

	// KeyLabels returns the set of labels associated with a logical key.
	// Returns an empty LabelSet if the key has no labels or does not exist.
	KeyLabels(ctx context.Context, key string) (LabelSet, error)
}

// CacheTimeline combines data operations and label management with configuration options.
type CacheTimeline interface {
	TimelineData
	TimelineLabels

	// WithOptions sets the configuration options for the timeline and returns self for method chaining.
	// The options apply to all keys in the timeline.
	// Options are stored in-memory only and must be set after timeline creation.
	WithOptions(opts TimelineOptions) CacheTimeline

	// GetOptions returns the timeline's configuration options.
	GetOptions() TimelineOptions
}

// FieldTimeValue represents a field's value at a specific time.
// Used both for time series (Timeline method) and merged state (GetAt, GetLatest).
type FieldTimeValue struct {
	Time  time.Time // When this field was last updated
	Value string    // The field's value
}

// TimeValue represents a moment in time with its associated complete state.
// Each field in Value includes its actual update timestamp.
type TimeValue struct {
	Time  time.Time                   // The snapshot timestamp
	Value map[string]*FieldTimeValue // Fields with per-field timestamps
}

// QueryOptions specifies which fields to query.
type QueryOptions struct {
	Fields []string // Specific fields to query. nil means all fields.
}

// TimelineOptions defines configuration options for a timeline.
type TimelineOptions struct {
	Retention RetentionPolicy // Automatic data lifecycle management rules
}

// RetentionPolicy defines automatic data lifecycle management rules.
// In field-level storage, MaxCount and MaxDuration are applied per-field independently.
type RetentionPolicy struct {
	MaxCount    int               // Maximum number of time points per field (0 = unlimited)
	MaxDuration time.Duration     // Maximum age of time points per field (0 = unlimited)
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
