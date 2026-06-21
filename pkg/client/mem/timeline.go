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
	// keyLabels maps each logical key to its set of labels (forward index).
	keyLabels map[string]map[string]bool
	// labelIndex maps each label to the set of logical keys with that label (inverted index).
	labelIndex map[string]map[string]bool
	mu         sync.RWMutex
	client     *client
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

// GetAt returns the complete merged state at or before ts for each key.
func (t *memTimeline) GetAt(ctx context.Context, keys []string, ts time.Time) ([]map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	tsMicros := normalizeTimestamp(ts)
	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	results := make([]map[string]string, len(keys))
	be := model.NewBatchError(len(keys))

	for i, key := range keys {
		td, ok := t.data[key]
		if !ok {
			results[i] = nil
			continue
		}
		if len(td.points) == 0 {
			results[i] = nil
			continue
		}
		idx := -1
		for j, point := range td.points {
			if point.ts <= tsMicros {
				idx = j
			} else {
				break
			}
		}
		if idx == -1 {
			results[i] = nil
			continue
		}
		results[i] = mergeFields(td.points[:idx+1])
	}

	return results, be.OrNil()
}

// GetExact returns the raw sparse fields at the exact timestamp for each key.
func (t *memTimeline) GetExact(ctx context.Context, keys []string, ts time.Time) ([]map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	tsMicros := normalizeTimestamp(ts)
	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	results := make([]map[string]string, len(keys))
	be := model.NewBatchError(len(keys))

	for i, key := range keys {
		td, ok := t.data[key]
		if !ok {
			results[i] = nil
			continue
		}
		idx, found := findTimePoint(td.points, tsMicros)
		if !found {
			results[i] = nil
			continue
		}
		result := make(map[string]string, len(td.points[idx].fields))
		for k, v := range td.points[idx].fields {
			result[k] = v
		}
		results[i] = result
	}

	return results, be.OrNil()
}

// GetRange returns all complete states in [start, end] for each key.
func (t *memTimeline) GetRange(ctx context.Context, keys []string, start, end time.Time) ([][]*model.TimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	startMicros := normalizeTimestamp(start)
	endMicros := normalizeTimestamp(end)
	t.mu.RLock()
	defer t.mu.RUnlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	results := make([][]*model.TimeValue, len(keys))
	be := model.NewBatchError(len(keys))

	for i, key := range keys {
		td, ok := t.data[key]
		if !ok {
			results[i] = nil
			continue
		}
		var tvs []*model.TimeValue
		for j, point := range td.points {
			if point.ts >= startMicros && point.ts <= endMicros {
				merged := mergeFields(td.points[:j+1])
				tv := &model.TimeValue{
					Time:  time.UnixMicro(point.ts),
					Value: merged,
				}
				tvs = append(tvs, tv)
			}
		}
		results[i] = tvs // nil if no points in range
	}

	return results, be.OrNil()
}

// GetLatest returns the complete merged state at the most recent timestamp for each key.
func (t *memTimeline) GetLatest(ctx context.Context, keys []string) ([]map[string]string, error) {
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

	results := make([]map[string]string, len(keys))
	be := model.NewBatchError(len(keys))

	for i, key := range keys {
		td, ok := t.data[key]
		if !ok || len(td.points) == 0 {
			results[i] = nil
			continue
		}
		results[i] = mergeFields(td.points)
	}

	return results, be.OrNil()
}

// Timeline returns all complete states for the key in chronological order.
func (t *memTimeline) Timeline(ctx context.Context, key string) ([]*model.TimeValue, error) {
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

	result := make([]*model.TimeValue, 0, len(td.points))
	for i, point := range td.points {
		merged := mergeFields(td.points[:i+1])
		result = append(result, &model.TimeValue{
			Time:  time.UnixMicro(point.ts),
			Value: merged,
		})
	}

	return result, nil
}

// GetAffectedRange returns all states from insertedAt (inclusive) to end of timeline.
func (t *memTimeline) GetAffectedRange(ctx context.Context, key string, insertedAt time.Time) ([]*model.TimeValue, error) {
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

	var result []*model.TimeValue
	for i, point := range td.points {
		if point.ts >= insertedMicros {
			merged := mergeFields(td.points[:i+1])
			result = append(result, &model.TimeValue{
				Time:  time.UnixMicro(point.ts),
				Value: merged,
			})
		}
	}

	return result, nil
}

// Keys returns all logical keys, optionally filtered by labels.
// With no arguments returns all keys. Labels within one []string are OR'd;
// multiple arguments are AND'd.
func (t *memTimeline) Keys(ctx context.Context, labelFilters ...[]string) ([]string, error) {
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

	if len(labelFilters) == 0 {
		result := make([]string, 0, len(t.data))
		for key := range t.data {
			result = append(result, key)
		}
		return result, nil
	}

	// Step 0: union of all keys matching any label in first filter step.
	result := map[string]bool{}
	firstStep := labelFilters[0]
	if len(firstStep) == 0 {
		for key := range t.data {
			result[key] = true
		}
	} else {
		for _, label := range firstStep {
			for key := range t.labelIndex[label] {
				result[key] = true
			}
		}
	}

	// Subsequent steps: AND — keep only keys that match at least one label in this step.
	for _, step := range labelFilters[1:] {
		if len(step) == 0 {
			continue
		}
		for key := range result {
			labels := t.keyLabels[key]
			matchesStep := false
			for _, label := range step {
				if labels[label] {
					matchesStep = true
					break
				}
			}
			if !matchesStep {
				delete(result, key)
			}
		}
	}

	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	return keys, nil
}

// AddKeyLabels associates labels with a logical key.
func (t *memTimeline) AddKeyLabels(ctx context.Context, key string, labels []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.keyLabels[key] == nil {
		t.keyLabels[key] = make(map[string]bool)
	}
	for _, label := range labels {
		if label == "" {
			continue
		}
		t.keyLabels[key][label] = true
		if t.labelIndex[label] == nil {
			t.labelIndex[label] = make(map[string]bool)
		}
		t.labelIndex[label][key] = true
	}
	return nil
}

// RemoveKeyLabels removes labels from a logical key.
func (t *memTimeline) RemoveKeyLabels(ctx context.Context, key string, labels []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, label := range labels {
		if label == "" {
			continue
		}
		delete(t.keyLabels[key], label)
		if idx, ok := t.labelIndex[label]; ok {
			delete(idx, key)
		}
	}
	return nil
}

// KeyLabels returns the label set for a logical key.
func (t *memTimeline) KeyLabels(ctx context.Context, key string) (model.LabelSet, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	ls := model.LabelSet{}
	for label := range t.keyLabels[key] {
		ls[label] = true
	}
	return ls, nil
}

// Remove removes the specified keys from the timeline, cleaning up label indexes.
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
		// Clean up label indexes.
		for label := range t.keyLabels[key] {
			if idx, ok := t.labelIndex[label]; ok {
				delete(idx, key)
			}
		}
		delete(t.keyLabels, key)
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
	t.keyLabels = make(map[string]map[string]bool)
	t.labelIndex = make(map[string]map[string]bool)

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
