package mem

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/huandu/skiplist"
	"github.com/leonkaihao/cache/v2/pkg/model"
)

// memTimeline implements the CacheTimeline interface using in-memory storage with field-level time series.
type memTimeline struct {
	name string
	data map[string]*timelineData
	retention model.RetentionPolicy
	// keyLabels maps each logical key to its set of labels (forward index).
	keyLabels map[string]map[string]bool
	// labelIndex maps each label to the set of logical keys with that label (inverted index).
	labelIndex map[string]map[string]bool
	// globalTS maps each key to its latest update timestamp (microseconds since epoch).
	// Used for efficient time-based key filtering in Keys method.
	globalTS map[string]int64
	mu       sync.RWMutex
	client   *client
}

// timelineData holds independent time series for each field of a key.
type timelineData struct {
	fields map[string]*fieldTimeline
}

// fieldTimeline holds the time series for a single field using a skiplist.
type fieldTimeline struct {
	points *skiplist.SkipList // key: int64 (timestamp), value: string (field value)
}

// normalizeTimestamp truncates a time.Time to microsecond precision and returns microseconds since epoch.
func normalizeTimestamp(ts time.Time) int64 {
	return ts.Truncate(time.Microsecond).UnixMicro()
}

// shouldIncludeField checks if a field should be included based on QueryOptions.
func shouldIncludeField(fieldName string, fieldsFilter []string) bool {
	if len(fieldsFilter) == 0 {
		return true // nil means all fields
	}
	for _, f := range fieldsFilter {
		if f == fieldName {
			return true
		}
	}
	return false
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
		td = &timelineData{fields: make(map[string]*fieldTimeline)}
		t.data[key] = td
	}

	// Update each field independently
	for fieldName, value := range data {
		ft, ok := td.fields[fieldName]
		if !ok {
			ft = &fieldTimeline{
				points: skiplist.New(skiplist.Int64),
			}
			td.fields[fieldName] = ft
		}

		// Check for conflicts if force is false
		if !force {
			if existingElem := ft.points.Get(tsMicros); existingElem != nil {
				existingValue := existingElem.Value.(string)
				if existingValue != value {
					return fmt.Errorf("field '%s' already exists at %s", fieldName, time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
				}
			}
		}

		// Set the value in the skiplist
		ft.points.Set(tsMicros, value)
	}

	// Update global timestamp index
	if currentMax, exists := t.globalTS[key]; !exists || tsMicros > currentMax {
		t.globalTS[key] = tsMicros
	}

	// Enforce retention per-field
	t.enforceRetention(key, td)
	return nil
}

// Insert adds or updates fields at the specified timestamp (supports out-of-order writes).
func (t *memTimeline) Insert(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error {
	return t.Append(ctx, key, ts, data, force)
}

// GetAt returns the complete merged state at or before ts for each key.
func (t *memTimeline) GetAt(ctx context.Context, keys []string, ts time.Time, opts model.QueryOptions) ([]map[string]*model.FieldTimeValue, error) {
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

	results := make([]map[string]*model.FieldTimeValue, len(keys))
	be := model.NewBatchError(len(keys))

	for i, key := range keys {
		td, ok := t.data[key]
		if !ok {
			results[i] = nil
			continue
		}

		result := make(map[string]*model.FieldTimeValue)
		for fieldName, ft := range td.fields {
			if !shouldIncludeField(fieldName, opts.Fields) {
				continue
			}

			// Find the latest value at or before ts
			elem := ft.points.Find(tsMicros)
			if elem == nil {
				// No exact match, find the element immediately before
				elem = ft.points.Front()
				var lastElem *skiplist.Element
				for elem != nil {
					elemTs := elem.Key().(int64)
					if elemTs > tsMicros {
						break
					}
					lastElem = elem
					elem = elem.Next()
				}
				elem = lastElem
			}

			if elem != nil {
				elemTs := elem.Key().(int64)
				if elemTs <= tsMicros {
					result[fieldName] = &model.FieldTimeValue{
						Time:  time.UnixMicro(elemTs),
						Value: elem.Value.(string),
					}
				}
			}
		}

		if len(result) == 0 {
			results[i] = nil
		} else {
			results[i] = result
		}
	}

	return results, be.OrNil()
}

// GetRange returns all complete states in [start, end] for each key.
func (t *memTimeline) GetRange(ctx context.Context, keys []string, start, end time.Time, opts model.QueryOptions) ([][]*model.TimeValue, error) {
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

		// Collect all unique timestamps where any specified field has an update in the range
		timestampsMap := make(map[int64]bool)
		for fieldName, ft := range td.fields {
			if !shouldIncludeField(fieldName, opts.Fields) {
				continue
			}

			elem := ft.points.Front()
			for elem != nil {
				elemTs := elem.Key().(int64)
				if elemTs > endMicros {
					break
				}
				if elemTs >= startMicros {
					timestampsMap[elemTs] = true
				}
				elem = elem.Next()
			}
		}

		// Sort timestamps
		timestamps := make([]int64, 0, len(timestampsMap))
		for ts := range timestampsMap {
			timestamps = append(timestamps, ts)
		}
		// Simple bubble sort for small slices
		for i := 0; i < len(timestamps)-1; i++ {
			for j := i + 1; j < len(timestamps); j++ {
				if timestamps[i] > timestamps[j] {
					timestamps[i], timestamps[j] = timestamps[j], timestamps[i]
				}
			}
		}

		// Build TimeValue for each timestamp
		var tvs []*model.TimeValue
		for _, ts := range timestamps {
			value := make(map[string]*model.FieldTimeValue)
			
			// For each field, get the value at or before this timestamp
			for fieldName, ft := range td.fields {
				if !shouldIncludeField(fieldName, opts.Fields) {
					continue
				}

				// Find the latest value at or before ts
				elem := ft.points.Find(ts)
				if elem == nil {
					// No exact match, find the element immediately before
					elem = ft.points.Front()
					var lastElem *skiplist.Element
					for elem != nil {
						elemTs := elem.Key().(int64)
						if elemTs > ts {
							break
						}
						lastElem = elem
						elem = elem.Next()
					}
					elem = lastElem
				}

				if elem != nil {
					elemTs := elem.Key().(int64)
					if elemTs <= ts {
						value[fieldName] = &model.FieldTimeValue{
							Time:  time.UnixMicro(elemTs),
							Value: elem.Value.(string),
						}
					}
				}
			}

			if len(value) > 0 {
				tvs = append(tvs, &model.TimeValue{
					Time:  time.UnixMicro(ts),
					Value: value,
				})
			}
		}

		results[i] = tvs
	}

	return results, be.OrNil()
}

// GetLatest returns the complete merged state at the most recent timestamp for each key.
func (t *memTimeline) GetLatest(ctx context.Context, keys []string, opts model.QueryOptions) ([]map[string]*model.FieldTimeValue, error) {
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

	results := make([]map[string]*model.FieldTimeValue, len(keys))
	be := model.NewBatchError(len(keys))

	for i, key := range keys {
		td, ok := t.data[key]
		if !ok {
			results[i] = nil
			continue
		}

		result := make(map[string]*model.FieldTimeValue)
		for fieldName, ft := range td.fields {
			if !shouldIncludeField(fieldName, opts.Fields) {
				continue
			}

			// Get the last (most recent) element from the skiplist
			elem := ft.points.Back()
			if elem != nil {
				elemTs := elem.Key().(int64)
				result[fieldName] = &model.FieldTimeValue{
					Time:  time.UnixMicro(elemTs),
					Value: elem.Value.(string),
				}
			}
		}

		if len(result) == 0 {
			results[i] = nil
		} else {
			results[i] = result
		}
	}

	return results, be.OrNil()
}

// Timeline returns field-grouped time series for the key.
func (t *memTimeline) Timeline(ctx context.Context, key string, opts model.QueryOptions) (map[string][]*model.FieldTimeValue, error) {
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

	result := make(map[string][]*model.FieldTimeValue)
	for fieldName, ft := range td.fields {
		if !shouldIncludeField(fieldName, opts.Fields) {
			continue
		}

		var series []*model.FieldTimeValue
		elem := ft.points.Front()
		for elem != nil {
			elemTs := elem.Key().(int64)
			series = append(series, &model.FieldTimeValue{
				Time:  time.UnixMicro(elemTs),
				Value: elem.Value.(string),
			})
			elem = elem.Next()
		}
		result[fieldName] = series
	}

	return result, nil
}

// GetAffectedRange returns all states from insertedAt (inclusive) to end of timeline.
func (t *memTimeline) GetAffectedRange(ctx context.Context, key string, insertedAt time.Time, opts model.QueryOptions) ([]*model.TimeValue, error) {
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

	// Collect all unique timestamps >= insertedAt where any specified field has an update
	timestampsMap := make(map[int64]bool)
	for fieldName, ft := range td.fields {
		if !shouldIncludeField(fieldName, opts.Fields) {
			continue
		}

		elem := ft.points.Front()
		for elem != nil {
			elemTs := elem.Key().(int64)
			if elemTs >= insertedMicros {
				timestampsMap[elemTs] = true
			}
			elem = elem.Next()
		}
	}

	// Sort timestamps
	timestamps := make([]int64, 0, len(timestampsMap))
	for ts := range timestampsMap {
		timestamps = append(timestamps, ts)
	}
	// Simple bubble sort for small slices
	for i := 0; i < len(timestamps)-1; i++ {
		for j := i + 1; j < len(timestamps); j++ {
			if timestamps[i] > timestamps[j] {
				timestamps[i], timestamps[j] = timestamps[j], timestamps[i]
			}
		}
	}

	// Build TimeValue for each timestamp
	var result []*model.TimeValue
	for _, ts := range timestamps {
		value := make(map[string]*model.FieldTimeValue)
		
		// For each field, get the value at or before this timestamp
		for fieldName, ft := range td.fields {
			if !shouldIncludeField(fieldName, opts.Fields) {
				continue
			}

			// Find the latest value at or before ts
			elem := ft.points.Find(ts)
			if elem == nil {
				// No exact match, find the element immediately before
				elem = ft.points.Front()
				var lastElem *skiplist.Element
				for elem != nil {
					elemTs := elem.Key().(int64)
					if elemTs > ts {
						break
					}
					lastElem = elem
					elem = elem.Next()
				}
				elem = lastElem
			}

			if elem != nil {
				elemTs := elem.Key().(int64)
				if elemTs <= ts {
					value[fieldName] = &model.FieldTimeValue{
						Time:  time.UnixMicro(elemTs),
						Value: elem.Value.(string),
					}
				}
			}
		}

		if len(value) > 0 {
			result = append(result, &model.TimeValue{
				Time:  time.UnixMicro(ts),
				Value: value,
			})
		}
	}

	return result, nil
}

// Keys returns all logical keys, optionally filtered by labels and update time.
func (t *memTimeline) Keys(ctx context.Context, opt model.FilterOptions) ([]string, error) {
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

	labelFilters := opt.LabelFilter

	// Start with all keys or label-filtered keys
	var result map[string]bool
	if len(labelFilters) == 0 {
		result = make(map[string]bool, len(t.data))
		for key := range t.data {
			result[key] = true
		}
	} else {
		// Step 0: union of all keys matching any label in first filter step.
		result = map[string]bool{}
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
	}

	// Apply time filter if specified
	if opt.AfterTs != nil {
		afterMicros := normalizeTimestamp(*opt.AfterTs)
		for key := range result {
			lastTs, exists := t.globalTS[key]
			if !exists || lastTs <= afterMicros {
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
		delete(t.globalTS, key)
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
	t.keyLabels = make(map[string]map[string]bool)
	t.labelIndex = make(map[string]map[string]bool)
	t.globalTS = make(map[string]int64)

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

// WithOptions sets the configuration options for the timeline and returns self for method chaining.
// The options apply to all keys in the timeline and are stored in-memory only.
func (t *memTimeline) WithOptions(opts model.TimelineOptions) model.CacheTimeline {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.retention = opts.Retention
	return t
}

// GetOptions returns the timeline's configuration options.
// Returns zero values if no options have been set (meaning unlimited retention).
func (t *memTimeline) GetOptions() model.TimelineOptions {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return model.TimelineOptions{Retention: t.retention}
}

// enforceRetention removes old time points based on retention policy.
// Applied per-field independently.
// Must be called with write lock held.
func (t *memTimeline) enforceRetention(key string, td *timelineData) {
	policy := t.retention

	if policy.MaxCount == 0 && policy.MaxDuration == 0 {
		return
	}

	// Apply retention to each field independently
	for _, ft := range td.fields {
		if ft.points.Len() == 0 {
			continue
		}

		var countBoundary int
		var durationBoundary int

		// Calculate count boundary
		if policy.MaxCount > 0 && ft.points.Len() > policy.MaxCount {
			countBoundary = ft.points.Len() - policy.MaxCount
		}

		// Calculate duration boundary
		if policy.MaxDuration > 0 {
			mostRecentElem := ft.points.Back()
			if mostRecentElem != nil {
				mostRecentTs := mostRecentElem.Key().(int64)
				cutoffTs := mostRecentTs - int64(policy.MaxDuration.Microseconds())

				idx := 0
				elem := ft.points.Front()
				for elem != nil {
					elemTs := elem.Key().(int64)
					if elemTs < cutoffTs {
						idx++
						elem = elem.Next()
					} else {
						break
					}
				}
				durationBoundary = idx
			}
		}

		// Determine removal boundary based on strategy
		var removeCount int
		if policy.MaxCount == 0 {
			removeCount = durationBoundary
		} else if policy.MaxDuration == 0 {
			removeCount = countBoundary
		} else {
			if policy.Strategy == model.RetentionMax {
				// Keep more data: remove less
				if countBoundary < durationBoundary {
					removeCount = countBoundary
				} else {
					removeCount = durationBoundary
				}
			} else {
				// RetentionMin: Keep less data: remove more
				if countBoundary > durationBoundary {
					removeCount = countBoundary
				} else {
					removeCount = durationBoundary
				}
			}
		}

		// Remove old elements from the skiplist
		if removeCount > 0 {
			elem := ft.points.Front()
			for i := 0; i < removeCount && elem != nil; i++ {
				nextElem := elem.Next()
				ft.points.Remove(elem.Key())
				elem = nextElem
			}
		}
	}
}
