package mem

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/leonkaihao/cache/pkg/model"
)

// memTimeline implements the CacheTimeline interface using in-memory storage.
type memTimeline struct {
	name          string
	data          map[string]*timelineData
	retention     model.RetentionPolicy
	keyRetentions map[string]model.RetentionPolicy
	mu            sync.RWMutex
	client        *client
}

// timelineData holds all time points for a specific key.
type timelineData struct {
	points []timePoint
}

// timePoint represents a moment in time with sparse field updates.
type timePoint struct {
	ts     int64             // Microseconds since epoch
	fields map[string]string // Sparse field updates
}

// normalizeTimestamp truncates a time.Time to microsecond precision and returns microseconds since epoch.
func normalizeTimestamp(ts time.Time) int64 {
	return ts.Truncate(time.Microsecond).UnixMicro()
}

// findTimePoint performs binary search to find the index of the time point with timestamp ts.
// Returns (index, true) if found, or (insertIndex, false) if not found.
func findTimePoint(points []timePoint, ts int64) (int, bool) {
	left, right := 0, len(points)
	for left < right {
		mid := left + (right-left)/2
		if points[mid].ts < ts {
			left = mid + 1
		} else if points[mid].ts > ts {
			right = mid
		} else {
			return mid, true
		}
	}
	return left, false
}

// mergeFields merges field updates from multiple time points.
// Later updates overwrite earlier ones.
func mergeFields(points []timePoint) map[string]string {
	result := make(map[string]string)
	for _, point := range points {
		for field, value := range point.fields {
			result[field] = value
		}
	}
	return result
}

// Name returns the timeline name.
func (t *memTimeline) Name() string {
	return t.name
}

// Append adds or updates fields at the specified timestamp.
func (t *memTimeline) Append(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}
	if len(data) == 0 {
		return nil
	}

	tsMicros := normalizeTimestamp(ts)
	t.mu.Lock()
	defer t.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	td, ok := t.data[key]
	if !ok {
		td = &timelineData{points: []timePoint{}}
		t.data[key] = td
	}

	idx, found := findTimePoint(td.points, tsMicros)
	if found {
		existing := &td.points[idx]
		if !force {
			for field := range data {
				if existingValue, exists := existing.fields[field]; exists && existingValue != data[field] {
					return fmt.Errorf("field '%s' already exists at %s", field, time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
				}
			}
		}
		for field, value := range data {
			existing.fields[field] = value
		}
	} else {
		newPoint := timePoint{
			ts:     tsMicros,
			fields: make(map[string]string, len(data)),
		}
		for field, value := range data {
			newPoint.fields[field] = value
		}
		td.points = append(td.points, timePoint{})
		copy(td.points[idx+1:], td.points[idx:])
		td.points[idx] = newPoint
	}

	t.enforceRetention(key, td)
	return nil
}

// Insert adds or updates fields at the specified timestamp (supports out-of-order writes).
func (t *memTimeline) Insert(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error {
	return t.Append(ctx, key, ts, data, force)
}

// GetAt returns the complete state at or before the specified timestamp.
func (t *memTimeline) GetAt(ctx context.Context, key string, ts time.Time) (map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	tsMicros := normalizeTimestamp(ts)
	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	td, ok := t.data[key]
	if !ok {
		return nil, fmt.Errorf("key '%s' not found in timeline '%s'", key, t.name)
	}

	if len(td.points) == 0 {
		return nil, fmt.Errorf("no state found at or before %s", time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
	}

	idx := -1
	for i, point := range td.points {
		if point.ts <= tsMicros {
			idx = i
		} else {
			break
		}
	}

	if idx == -1 {
		return nil, fmt.Errorf("no state found at or before %s", time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
	}

	return mergeFields(td.points[:idx+1]), nil
}

// GetExact returns the raw sparse data at the exact timestamp.
func (t *memTimeline) GetExact(ctx context.Context, key string, ts time.Time) (map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	tsMicros := normalizeTimestamp(ts)
	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	td, ok := t.data[key]
	if !ok {
		return nil, fmt.Errorf("key '%s' not found in timeline '%s'", key, t.name)
	}

	idx, found := findTimePoint(td.points, tsMicros)
	if !found {
		return nil, fmt.Errorf("no exact timestamp found at %s", time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
	}

	result := make(map[string]string, len(td.points[idx].fields))
	for k, v := range td.points[idx].fields {
		result[k] = v
	}
	return result, nil
}

// GetRange returns all complete states in the time range [start, end].
func (t *memTimeline) GetRange(ctx context.Context, key string, start, end time.Time) ([]model.TimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	startMicros := normalizeTimestamp(start)
	endMicros := normalizeTimestamp(end)
	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	td, ok := t.data[key]
	if !ok {
		return nil, fmt.Errorf("key '%s' not found in timeline '%s'", key, t.name)
	}

	var result []model.TimeValue
	for i, point := range td.points {
		if point.ts >= startMicros && point.ts <= endMicros {
			merged := mergeFields(td.points[:i+1])
			result = append(result, model.TimeValue{
				Time:  time.UnixMicro(point.ts),
				Value: merged,
			})
		}
	}

	return result, nil
}

// GetLatest returns the complete state at the most recent timestamp.
func (t *memTimeline) GetLatest(ctx context.Context, key string) (map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	td, ok := t.data[key]
	if !ok {
		return nil, fmt.Errorf("key '%s' not found in timeline '%s'", key, t.name)
	}

	if len(td.points) == 0 {
		return nil, fmt.Errorf("no state found for key '%s'", key)
	}

	return mergeFields(td.points), nil
}

// Timeline returns all complete states for the key in chronological order.
func (t *memTimeline) Timeline(ctx context.Context, key string) ([]model.TimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	td, ok := t.data[key]
	if !ok {
		return nil, fmt.Errorf("key '%s' not found in timeline '%s'", key, t.name)
	}

	result := make([]model.TimeValue, 0, len(td.points))
	for i, point := range td.points {
		merged := mergeFields(td.points[:i+1])
		result = append(result, model.TimeValue{
			Time:  time.UnixMicro(point.ts),
			Value: merged,
		})
	}

	return result, nil
}

// GetAffectedRange returns all states from insertedAt (inclusive) to end of timeline.
func (t *memTimeline) GetAffectedRange(ctx context.Context, key string, insertedAt time.Time) ([]model.TimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	insertedMicros := normalizeTimestamp(insertedAt)
	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	td, ok := t.data[key]
	if !ok {
		return nil, fmt.Errorf("key '%s' not found in timeline '%s'", key, t.name)
	}

	var result []model.TimeValue
	for i, point := range td.points {
		if point.ts >= insertedMicros {
			merged := mergeFields(td.points[:i+1])
			result = append(result, model.TimeValue{
				Time:  time.UnixMicro(point.ts),
				Value: merged,
			})
		}
	}

	return result, nil
}

// Keys returns all keys that have been written to the timeline.
func (t *memTimeline) Keys(ctx context.Context) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := make([]string, 0, len(t.data))
	for key := range t.data {
		result = append(result, key)
	}

	return result, nil
}

// Remove removes the specified keys from the timeline.
func (t *memTimeline) Remove(ctx context.Context, keys []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, key := range keys {
		delete(t.data, key)
	}

	return nil
}

// Clear removes all data from the timeline but keeps the timeline instance.
func (t *memTimeline) Clear(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	t.data = make(map[string]*timelineData)
	t.keyRetentions = make(map[string]model.RetentionPolicy)

	return nil
}

// Delete removes the timeline instance from the client.
func (t *memTimeline) Delete(ctx context.Context) error {
	if err := t.Clear(ctx); err != nil {
		return err
	}

	if t.client != nil {
		return t.client.RemoveTimeline(t.name)
	}

	return nil
}

// SetRetention sets the retention policy for the timeline.
func (t *memTimeline) SetRetention(policy model.RetentionPolicy) error {
	if policy.MaxCount < 0 {
		policy.MaxCount = 0
	}
	if policy.MaxDuration < 0 {
		policy.MaxDuration = 0
	}

	if policy.Strategy != model.RetentionMax && policy.Strategy != model.RetentionMin {
		return fmt.Errorf("invalid retention strategy: %d", policy.Strategy)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.retention = policy
	return nil
}

// GetRetention returns the timeline's default retention policy.
func (t *memTimeline) GetRetention() model.RetentionPolicy {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.retention
}

// SetKeyRetention sets the retention policy for a specific key.
func (t *memTimeline) SetKeyRetention(key string, policy model.RetentionPolicy) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	if policy.MaxCount < 0 {
		policy.MaxCount = 0
	}
	if policy.MaxDuration < 0 {
		policy.MaxDuration = 0
	}

	if policy.Strategy != model.RetentionMax && policy.Strategy != model.RetentionMin {
		return fmt.Errorf("invalid retention strategy: %d", policy.Strategy)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.keyRetentions[key] = policy
	return nil
}

// GetKeyRetention returns the retention policy for a specific key.
func (t *memTimeline) GetKeyRetention(key string) model.RetentionPolicy {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if policy, ok := t.keyRetentions[key]; ok {
		return policy
	}
	return t.retention
}

// enforceRetention removes old time points based on retention policy.
// Must be called with write lock held.
func (t *memTimeline) enforceRetention(key string, td *timelineData) {
	policy, ok := t.keyRetentions[key]
	if !ok {
		policy = t.retention
	}

	if policy.MaxCount == 0 && policy.MaxDuration == 0 {
		return
	}

	if len(td.points) == 0 {
		return
	}

	var countBoundary int = 0
	var durationBoundary int = 0

	if policy.MaxCount > 0 && len(td.points) > policy.MaxCount {
		countBoundary = len(td.points) - policy.MaxCount
	}

	if policy.MaxDuration > 0 {
		mostRecentTs := td.points[len(td.points)-1].ts
		cutoffTs := mostRecentTs - int64(policy.MaxDuration.Microseconds())

		for i := len(td.points) - 1; i >= 0; i-- {
			if td.points[i].ts < cutoffTs {
				durationBoundary = i + 1
				break
			}
		}
	}

	var removeBeforeIdx int
	if policy.MaxCount == 0 {
		removeBeforeIdx = durationBoundary
	} else if policy.MaxDuration == 0 {
		removeBeforeIdx = countBoundary
	} else {
		if policy.Strategy == model.RetentionMax {
			if countBoundary < durationBoundary {
				removeBeforeIdx = countBoundary
			} else {
				removeBeforeIdx = durationBoundary
			}
		} else {
			if countBoundary > durationBoundary {
				removeBeforeIdx = countBoundary
			} else {
				removeBeforeIdx = durationBoundary
			}
		}
	}

	if removeBeforeIdx > 0 {
		td.points = td.points[removeBeforeIdx:]
	}
}
