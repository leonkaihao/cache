package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/consts"
	"github.com/leonkaihao/cache/v2/pkg/model"
	redis "github.com/redis/go-redis/v9"
)

// redisTimeline implements the CacheTimeline interface using Redis backend with field-level storage.
type redisTimeline struct {
	name      string
	cli       *client
	retention model.RetentionPolicy
	mu        sync.RWMutex
}

// Key generation helpers for field-level storage pattern:
// T@{name}/K/                          - SET of all keys
// T@{name}/F/                          - SET of all field names (union across all keys)
// T@{name}/K/{key}/F/{field}           - ZSET: score=timestamp, member="{ts}:{value}"
// T@{name}/L/                          - SET of all labels
// T@{name}/L/{label}                   - SET of keys with this label (inverted index)
// T@{name}/K/{key}/L                   - SET of labels for this key (forward index)
// T@{name}/GTS                         - ZSET: key -> latest timestamp (for AfterTs filtering)

func formatTimelineKeys(name string) string {
	return fmt.Sprintf("%s%s/%s", consts.TIMELINE_PREFIX, name, consts.KEYS_PREFIX)
}

func formatTimelineFields(name string) string {
	return fmt.Sprintf("%s%s/%s", consts.TIMELINE_PREFIX, name, consts.FIELDS_PREFIX)
}

func formatTimelineField(name, key, field string) string {
	return fmt.Sprintf("%s%s/%s%s/%s%s", consts.TIMELINE_PREFIX, name, consts.KEYS_PREFIX, key, consts.FIELDS_PREFIX, field)
}

func formatTimelineLabels(name string) string {
	return fmt.Sprintf("%s%s/%s", consts.TIMELINE_PREFIX, name, consts.LABELS_PREFIX)
}

func formatTimelineLabel(name, label string) string {
	return fmt.Sprintf("%s%s/%s%s", consts.TIMELINE_PREFIX, name, consts.LABELS_PREFIX, label)
}

func formatTimelineKeyLabels(name, key string) string {
	return fmt.Sprintf("%s%s/%s%s/%s", consts.TIMELINE_PREFIX, name, consts.KEYS_PREFIX, key, consts.LABELS_PREFIX)
}

func formatTimelineGlobalTS(name string) string {
	return fmt.Sprintf("%s%s/%s", consts.TIMELINE_PREFIX, name, consts.GLOBAL_TS_SUFFIX)
}

// encodeMember encodes timestamp and value into ZSET member format: "{ts}:{value}"
func encodeMember(tsMicros int64, value string) string {
	return fmt.Sprintf("%d:%s", tsMicros, value)
}

// decodeMember decodes ZSET member format "{ts}:{value}" into timestamp and value
func decodeMember(member string) (int64, string, error) {
	parts := strings.SplitN(member, ":", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid member format: %s", member)
	}
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid timestamp in member: %s", member)
	}
	return ts, parts[1], nil
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
func (t *redisTimeline) Name() string {
	return t.name
}

// Append adds or updates fields at the specified timestamp.
func (t *redisTimeline) Append(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error {
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
	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)
	fieldsKey := formatTimelineFields(t.name)
	globalTSKey := formatTimelineGlobalTS(t.name)

	// Check for field conflicts if force=false
	if !force {
		pipe := redisCli.Pipeline()
		cmds := make(map[string]*redis.StringSliceCmd)
		
		for fieldName := range data {
			fieldKey := formatTimelineField(t.name, key, fieldName)
			cmds[fieldName] = pipe.ZRangeByScore(ctx, fieldKey, &redis.ZRangeBy{
				Min: fmt.Sprintf("%d", tsMicros),
				Max: fmt.Sprintf("%d", tsMicros),
			})
		}
		
		_, _ = pipe.Exec(ctx)
		
		for fieldName, newValue := range data {
			members, err := cmds[fieldName].Result()
			if err == nil && len(members) > 0 {
				// Found existing member at this timestamp
				_, existingValue, err := decodeMember(members[0])
				if err == nil && existingValue != newValue {
					return fmt.Errorf("field '%s' already exists at %s", fieldName, time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
				}
			}
		}
	}

	// Write all fields
	pipe := redisCli.Pipeline()
	
	for fieldName, value := range data {
		fieldKey := formatTimelineField(t.name, key, fieldName)
		member := encodeMember(tsMicros, value)
		pipe.ZAdd(ctx, fieldKey, redis.Z{Score: float64(tsMicros), Member: member})
		// Maintain timeline-level field set (union of all fields across all keys)
		pipe.SAdd(ctx, fieldsKey, fieldName)
	}
	
	// Add key to keys set
	pipe.SAdd(ctx, keysKey, key)
	
	// Update global timestamp index
	pipe.ZAdd(ctx, globalTSKey, redis.Z{Score: float64(tsMicros), Member: key})
	
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	// Enforce retention policy (best-effort)
	if err := t.enforceRetention(ctx, key); err != nil {
		Logger.Error("retention enforcement failed",
			"key", key,
			"timeline", t.name,
			"error", err)
	}

	return nil
}

// Insert adds or updates fields at the specified timestamp (supports out-of-order writes).
func (t *redisTimeline) Insert(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error {
	return t.Append(ctx, key, ts, data, force)
}

// GetAt returns the complete merged state at or before ts for each key.
func (t *redisTimeline) GetAt(ctx context.Context, keys []string, ts time.Time, opts model.QueryOptions) ([]map[string]*model.FieldTimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	tsMicros := normalizeTimestamp(ts)
	redisCli := t.cli.getRedisCli()
	fieldsKey := formatTimelineFields(t.name)
	results := make([]map[string]*model.FieldTimeValue, len(keys))
	be := model.NewBatchError(len(keys))

	// Get all known fields for this timeline
	fieldNames, err := redisCli.SMembers(ctx, fieldsKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get timeline fields: %w", err)
	}

	// For each key, query all known fields
	for i, key := range keys {
		if len(fieldNames) == 0 {
			results[i] = nil
			continue
		}

		// For each field, get the latest value at or before ts
		pipe := redisCli.Pipeline()
		cmds := make([]*redis.StringSliceCmd, len(fieldNames))
		
		for j, fieldName := range fieldNames {
			if !shouldIncludeField(fieldName, opts.Fields) {
				continue
			}
			
			fieldKey := formatTimelineField(t.name, key, fieldName)
			// Query for values <= ts, get the last one
			cmds[j] = pipe.ZRevRangeByScore(ctx, fieldKey, &redis.ZRangeBy{
				Min:   "-inf",
				Max:   fmt.Sprintf("%d", tsMicros),
				Count: 1,
			})
		}
		
		_, _ = pipe.Exec(ctx)
		
		result := make(map[string]*model.FieldTimeValue)
		for j, cmd := range cmds {
			if cmd == nil || !shouldIncludeField(fieldNames[j], opts.Fields) {
				continue
			}
			
			members, err := cmd.Result()
			if err != nil || len(members) == 0 {
				// Field doesn't exist for this key, skip it
				continue
			}
			
			fieldTs, fieldValue, err := decodeMember(members[0])
			if err != nil {
				continue
			}
			
			result[fieldNames[j]] = &model.FieldTimeValue{
				Time:  time.UnixMicro(fieldTs),
				Value: fieldValue,
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
func (t *redisTimeline) GetRange(ctx context.Context, keys []string, start, end time.Time, opts model.QueryOptions) ([][]*model.TimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	startMicros := normalizeTimestamp(start)
	endMicros := normalizeTimestamp(end)
	redisCli := t.cli.getRedisCli()
	fieldsKey := formatTimelineFields(t.name)
	results := make([][]*model.TimeValue, len(keys))
	be := model.NewBatchError(len(keys))

	// Get all known fields for this timeline
	fieldNames, err := redisCli.SMembers(ctx, fieldsKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get timeline fields: %w", err)
	}

	for i, key := range keys {
		if len(fieldNames) == 0 {
			results[i] = nil
			continue
		}

		// Collect all unique timestamps in range from all fields
		timestampsMap := make(map[int64]bool)
		
		pipe := redisCli.Pipeline()
		rangeCmds := make([]*redis.StringSliceCmd, len(fieldNames))
		
		for j, fieldName := range fieldNames {
			if !shouldIncludeField(fieldName, opts.Fields) {
				continue
			}
			
			fieldKey := formatTimelineField(t.name, key, fieldName)
			rangeCmds[j] = pipe.ZRangeByScore(ctx, fieldKey, &redis.ZRangeBy{
				Min: fmt.Sprintf("%d", startMicros),
				Max: fmt.Sprintf("%d", endMicros),
			})
		}
		
		_, _ = pipe.Exec(ctx)
		
		for j, cmd := range rangeCmds {
			if cmd == nil || !shouldIncludeField(fieldNames[j], opts.Fields) {
				continue
			}
			
			members, err := cmd.Result()
			if err != nil {
				continue
			}
			
			for _, member := range members {
				fieldTs, _, err := decodeMember(member)
				if err == nil {
					timestampsMap[fieldTs] = true
				}
			}
		}

		// Sort timestamps
		timestamps := make([]int64, 0, len(timestampsMap))
		for ts := range timestampsMap {
			timestamps = append(timestamps, ts)
		}
		// Simple bubble sort
		for a := 0; a < len(timestamps)-1; a++ {
			for b := a + 1; b < len(timestamps); b++ {
				if timestamps[a] > timestamps[b] {
					timestamps[a], timestamps[b] = timestamps[b], timestamps[a]
				}
			}
		}

		// For each timestamp, build complete state
		var tvs []*model.TimeValue
		for _, ts := range timestamps {
			pipe := redisCli.Pipeline()
			allCmds := make([]*redis.StringSliceCmd, len(fieldNames))
			
			for j, fieldName := range fieldNames {
				if !shouldIncludeField(fieldName, opts.Fields) {
					continue
				}
				
				fieldKey := formatTimelineField(t.name, key, fieldName)
				allCmds[j] = pipe.ZRevRangeByScore(ctx, fieldKey, &redis.ZRangeBy{
					Min:   "-inf",
					Max:   fmt.Sprintf("%d", ts),
					Count: 1,
				})
			}
			
			_, _ = pipe.Exec(ctx)
			
			value := make(map[string]*model.FieldTimeValue)
			for j, cmd := range allCmds {
				if cmd == nil || !shouldIncludeField(fieldNames[j], opts.Fields) {
					continue
				}
				
				members, err := cmd.Result()
				if err != nil || len(members) == 0 {
					continue
				}
				
				fieldTs, fieldValue, err := decodeMember(members[0])
				if err != nil {
					continue
				}
				
				value[fieldNames[j]] = &model.FieldTimeValue{
					Time:  time.UnixMicro(fieldTs),
					Value: fieldValue,
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
func (t *redisTimeline) GetLatest(ctx context.Context, keys []string, opts model.QueryOptions) ([]map[string]*model.FieldTimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	fieldsKey := formatTimelineFields(t.name)
	results := make([]map[string]*model.FieldTimeValue, len(keys))
	be := model.NewBatchError(len(keys))

	// Get all known fields for this timeline
	fieldNames, err := redisCli.SMembers(ctx, fieldsKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get timeline fields: %w", err)
	}

	for i, key := range keys {
		if len(fieldNames) == 0 {
			results[i] = nil
			continue
		}

		// For each field, get the latest value
		pipe := redisCli.Pipeline()
		cmds := make([]*redis.StringSliceCmd, len(fieldNames))
		
		for j, fieldName := range fieldNames {
			if !shouldIncludeField(fieldName, opts.Fields) {
				continue
			}
			
			fieldKey := formatTimelineField(t.name, key, fieldName)
			cmds[j] = pipe.ZRevRange(ctx, fieldKey, 0, 0)
		}
		
		_, _ = pipe.Exec(ctx)
		
		result := make(map[string]*model.FieldTimeValue)
		for j, cmd := range cmds {
			if cmd == nil || !shouldIncludeField(fieldNames[j], opts.Fields) {
				continue
			}
			
			members, err := cmd.Result()
			if err != nil || len(members) == 0 {
				// Field doesn't exist for this key, skip it
				continue
			}
			
			fieldTs, fieldValue, err := decodeMember(members[0])
			if err != nil {
				continue
			}
			
			result[fieldNames[j]] = &model.FieldTimeValue{
				Time:  time.UnixMicro(fieldTs),
				Value: fieldValue,
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
func (t *redisTimeline) Timeline(ctx context.Context, key string, opts model.QueryOptions) (map[string][]*model.FieldTimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	redisCli := t.cli.getRedisCli()
	fieldsKey := formatTimelineFields(t.name)
	
	// Get all known fields for this timeline
	fieldNames, err := redisCli.SMembers(ctx, fieldsKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get timeline fields: %w", err)
	}

	if len(fieldNames) == 0 {
		return nil, fmt.Errorf("key '%s' not found in timeline '%s'", key, t.name)
	}

	// For each field, get all its values
	pipe := redisCli.Pipeline()
	cmds := make([]*redis.StringSliceCmd, len(fieldNames))
	
	for j, fieldName := range fieldNames {
		if !shouldIncludeField(fieldName, opts.Fields) {
			continue
		}
		
		fieldKey := formatTimelineField(t.name, key, fieldName)
		cmds[j] = pipe.ZRange(ctx, fieldKey, 0, -1)
	}
	
	_, _ = pipe.Exec(ctx)
	
	result := make(map[string][]*model.FieldTimeValue)
	for j, cmd := range cmds {
		if cmd == nil || !shouldIncludeField(fieldNames[j], opts.Fields) {
			continue
		}
		
		members, err := cmd.Result()
		if err != nil {
			continue
		}
		
		var series []*model.FieldTimeValue
		for _, member := range members {
			fieldTs, fieldValue, err := decodeMember(member)
			if err != nil {
				continue
			}
			
			series = append(series, &model.FieldTimeValue{
				Time:  time.UnixMicro(fieldTs),
				Value: fieldValue,
			})
		}
		
		if len(series) > 0 {
			result[fieldNames[j]] = series
		}
	}

	return result, nil
}

// GetAffectedRange returns all states from insertedAt (inclusive) to end of timeline.
func (t *redisTimeline) GetAffectedRange(ctx context.Context, key string, insertedAt time.Time, opts model.QueryOptions) ([]*model.TimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	insertedMicros := normalizeTimestamp(insertedAt)
	redisCli := t.cli.getRedisCli()
	fieldsKey := formatTimelineFields(t.name)
	
	// Get all known fields for this timeline
	fieldNames, err := redisCli.SMembers(ctx, fieldsKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get timeline fields: %w", err)
	}

	if len(fieldNames) == 0 {
		return nil, fmt.Errorf("key '%s' not found in timeline '%s'", key, t.name)
	}

	// Collect all unique timestamps >= insertedAt from specified fields
	timestampsMap := make(map[int64]bool)
	
	pipe := redisCli.Pipeline()
	rangeCmds := make([]*redis.StringSliceCmd, len(fieldNames))
	
	for j, fieldName := range fieldNames {
		if !shouldIncludeField(fieldName, opts.Fields) {
			continue
		}
		
		fieldKey := formatTimelineField(t.name, key, fieldName)
		rangeCmds[j] = pipe.ZRangeByScore(ctx, fieldKey, &redis.ZRangeBy{
			Min: fmt.Sprintf("%d", insertedMicros),
			Max: "+inf",
		})
	}
	
	_, _ = pipe.Exec(ctx)
	
	for j, cmd := range rangeCmds {
		if cmd == nil || !shouldIncludeField(fieldNames[j], opts.Fields) {
			continue
		}
		
		members, err := cmd.Result()
		if err != nil {
			continue
		}
		
		for _, member := range members {
			fieldTs, _, err := decodeMember(member)
			if err == nil {
				timestampsMap[fieldTs] = true
			}
		}
	}

	// Sort timestamps
	timestamps := make([]int64, 0, len(timestampsMap))
	for ts := range timestampsMap {
		timestamps = append(timestamps, ts)
	}
	// Simple bubble sort
	for a := 0; a < len(timestamps)-1; a++ {
		for b := a + 1; b < len(timestamps); b++ {
			if timestamps[a] > timestamps[b] {
				timestamps[a], timestamps[b] = timestamps[b], timestamps[a]
			}
		}
	}

	// For each timestamp, build complete state
	var result []*model.TimeValue
	for _, ts := range timestamps {
		pipe := redisCli.Pipeline()
		allCmds := make([]*redis.StringSliceCmd, len(fieldNames))
		
		for j, fieldName := range fieldNames {
			if !shouldIncludeField(fieldName, opts.Fields) {
				continue
			}
			
			fieldKey := formatTimelineField(t.name, key, fieldName)
			allCmds[j] = pipe.ZRevRangeByScore(ctx, fieldKey, &redis.ZRangeBy{
				Min:   "-inf",
				Max:   fmt.Sprintf("%d", ts),
				Count: 1,
			})
		}
		
		_, _ = pipe.Exec(ctx)
		
		value := make(map[string]*model.FieldTimeValue)
		for j, cmd := range allCmds {
			if cmd == nil || !shouldIncludeField(fieldNames[j], opts.Fields) {
				continue
			}
			
			members, err := cmd.Result()
			if err != nil || len(members) == 0 {
				continue
			}
			
			fieldTs, fieldValue, err := decodeMember(members[0])
			if err != nil {
				continue
			}
			
			value[fieldNames[j]] = &model.FieldTimeValue{
				Time:  time.UnixMicro(fieldTs),
				Value: fieldValue,
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
func (t *redisTimeline) Keys(ctx context.Context, opt model.FilterOptions) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)
	labelFilters := opt.LabelFilter

	// Start with all keys or label-filtered keys
	var result []string
	var err error

	if len(labelFilters) == 0 {
		result, err = redisCli.SMembers(ctx, keysKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get keys: %w", err)
		}
	} else {
		// Pipeline SMEMBERS for every label across all filter steps.
		pipe := redisCli.Pipeline()
		cmds := map[string]*redis.StringSliceCmd{}
		for _, step := range labelFilters {
			for _, label := range step {
				labelKey := formatTimelineLabel(t.name, label)
				if _, exists := cmds[labelKey]; !exists {
					cmds[labelKey] = pipe.SMembers(ctx, labelKey)
				}
			}
		}
		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			return nil, fmt.Errorf("failed to get label members: %w", err)
		}

		// Build filterStepsNew: each step is a slice of key-sets (one per label).
		filterStepsNew := make([][]map[string]bool, 0, len(labelFilters))
		for _, step := range labelFilters {
			keysets := make([]map[string]bool, 0, len(step))
			for _, label := range step {
				labelKey := formatTimelineLabel(t.name, label)
				members, err := cmds[labelKey].Result()
				if err != nil && err != redis.Nil {
					return nil, fmt.Errorf("failed to get members for label %s: %w", label, err)
				}
				keysets = append(keysets, arrToMap(members))
			}
			filterStepsNew = append(filterStepsNew, keysets)
		}

		// Step 0: union of all label sets.
		base := map[string]bool{}
		for i, keysets := range filterStepsNew {
			collection := map[string]bool{}
			if len(keysets) == 0 {
				// Empty step — get all keys.
				all, err := redisCli.SMembers(ctx, keysKey).Result()
				if err != nil {
					return nil, fmt.Errorf("failed to get all keys: %w", err)
				}
				collection = arrToMap(all)
			} else {
				for j, keyset := range keysets {
					if j == 0 {
						collection = keyset
					} else {
						collection = union(collection, keyset)
					}
				}
			}
			if i == 0 {
				base = collection
			} else {
				base = intersect(base, collection)
			}
		}

		result = make([]string, 0, len(base))
		for key := range base {
			result = append(result, key)
		}
	}

	// Apply time filter if specified
	if opt.AfterTs != nil {
		afterMicros := normalizeTimestamp(*opt.AfterTs)
		globalTSKey := formatTimelineGlobalTS(t.name)
		
		// Query keys with timestamp > afterMicros
		updatedKeys, err := redisCli.ZRangeByScore(ctx, globalTSKey, &redis.ZRangeBy{
			Min: fmt.Sprintf("(%d", afterMicros), // Exclusive
			Max: "+inf",
		}).Result()
		if err != nil && err != redis.Nil {
			return nil, fmt.Errorf("failed to query updated keys: %w", err)
		}
		
		// Intersect with current result
		updatedSet := arrToMap(updatedKeys)
		filtered := []string{}
		for _, key := range result {
			if updatedSet[key] {
				filtered = append(filtered, key)
			}
		}
		result = filtered
	}

	return result, nil
}

// AddKeyLabels associates labels with a logical key.
func (t *redisTimeline) AddKeyLabels(ctx context.Context, key string, labels []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	pipe := redisCli.Pipeline()
	labelsKey := formatTimelineLabels(t.name)
	keyLabelsKey := formatTimelineKeyLabels(t.name, key)

	for _, label := range labels {
		if label == "" {
			continue
		}
		labelKey := formatTimelineLabel(t.name, label)
		pipe.SAdd(ctx, labelsKey, label)
		pipe.SAdd(ctx, labelKey, key)
		pipe.SAdd(ctx, keyLabelsKey, label)
	}

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return fmt.Errorf("failed to add key labels: %w", err)
	}
	return nil
}

// RemoveKeyLabels removes labels from a logical key.
func (t *redisTimeline) RemoveKeyLabels(ctx context.Context, key string, labels []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	pipe := redisCli.Pipeline()
	keyLabelsKey := formatTimelineKeyLabels(t.name, key)

	for _, label := range labels {
		if label == "" {
			continue
		}
		labelKey := formatTimelineLabel(t.name, label)
		pipe.SRem(ctx, labelKey, key)
		pipe.SRem(ctx, keyLabelsKey, label)
	}

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return fmt.Errorf("failed to remove key labels: %w", err)
	}
	return nil
}

// KeyLabels returns the label set for a logical key.
func (t *redisTimeline) KeyLabels(ctx context.Context, key string) (model.LabelSet, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keyLabelsKey := formatTimelineKeyLabels(t.name, key)

	members, err := redisCli.SMembers(ctx, keyLabelsKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get key labels: %w", err)
	}

	ls := model.LabelSet{}
	for _, label := range members {
		ls[label] = true
	}
	return ls, nil
}

// Remove removes the specified keys from the timeline, cleaning up label indexes and field data.
func (t *redisTimeline) Remove(ctx context.Context, keys []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)
	globalTSKey := formatTimelineGlobalTS(t.name)

	for _, key := range keys {
		// Read forward label index to clean up inverted index.
		keyLabelsKey := formatTimelineKeyLabels(t.name, key)
		labels, err := redisCli.SMembers(ctx, keyLabelsKey).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("failed to read labels for key %s: %w", key, err)
		}

		// Find all field keys for this key
		pattern := formatTimelineField(t.name, key, "*")
		var fieldKeys []string
		iter := redisCli.Scan(ctx, 0, pattern, 0).Iterator()
		for iter.Next(ctx) {
			fieldKeys = append(fieldKeys, iter.Val())
		}
		if err := iter.Err(); err != nil {
			return fmt.Errorf("failed to scan field keys: %w", err)
		}

		pipe := redisCli.Pipeline()
		
		// Remove key from each inverted label set.
		for _, label := range labels {
			pipe.SRem(ctx, formatTimelineLabel(t.name, label), key)
		}
		
		// Delete the forward label index for this key.
		pipe.Del(ctx, keyLabelsKey)
		
		// Remove from keys set.
		pipe.SRem(ctx, keysKey, key)
		
		// Remove from global timestamp index.
		pipe.ZRem(ctx, globalTSKey, key)
		
		// Delete all field ZSETs
		for _, fieldKey := range fieldKeys {
			pipe.Del(ctx, fieldKey)
		}

		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			return fmt.Errorf("failed to remove key %s: %w", key, err)
		}
	}

	return nil
}

// Clear removes all data from the timeline using prefix-based scanning.
func (t *redisTimeline) Clear(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	pattern := fmt.Sprintf("%s%s/*", consts.TIMELINE_PREFIX, t.name)

	return scanAndDeleteByPrefix(ctx, redisCli, pattern)
}

// Delete removes the timeline instance from the client.
func (t *redisTimeline) Delete(ctx context.Context) error {
	if err := t.Clear(ctx); err != nil {
		return err
	}
	return t.cli.RemoveTimeline(t.name)
}

// WithOptions sets the configuration options for the timeline and returns self for method chaining.
func (t *redisTimeline) WithOptions(opts model.TimelineOptions) model.CacheTimeline {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.retention = opts.Retention
	return t
}

// GetOptions returns the timeline's configuration options.
func (t *redisTimeline) GetOptions() model.TimelineOptions {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return model.TimelineOptions{Retention: t.retention}
}

// enforceRetention removes old time points from each field's ZSET based on retention policy.
func (t *redisTimeline) enforceRetention(ctx context.Context, key string) error {
	t.mu.RLock()
	policy := t.retention
	t.mu.RUnlock()

	if policy.MaxCount == 0 && policy.MaxDuration == 0 {
		return nil
	}

	redisCli := t.cli.getRedisCli()
	fieldsKey := formatTimelineFields(t.name)
	
	// Get all known fields for this timeline
	fieldNames, err := redisCli.SMembers(ctx, fieldsKey).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("failed to get timeline fields: %w", err)
	}

	// For each field, apply retention
	for _, fieldName := range fieldNames {
		fieldKey := formatTimelineField(t.name, key, fieldName)
		count, err := redisCli.ZCard(ctx, fieldKey).Result()
		if err != nil || count == 0 {
			continue
		}

		var countBoundary int64
		var durationBoundary int64

		// Calculate count boundary
		if policy.MaxCount > 0 && int(count) > policy.MaxCount {
			countBoundary = count - int64(policy.MaxCount)
		}

		// Calculate duration boundary
		if policy.MaxDuration > 0 {
			// Get the most recent timestamp
			members, err := redisCli.ZRevRange(ctx, fieldKey, 0, 0).Result()
			if err == nil && len(members) > 0 {
				mostRecentTs, _, err := decodeMember(members[0])
				if err == nil {
					cutoffTs := mostRecentTs - policy.MaxDuration.Microseconds()
					
					// Count how many elements are below cutoff
					countBelow, err := redisCli.ZCount(ctx, fieldKey, "-inf", fmt.Sprintf("(%d", cutoffTs)).Result()
					if err == nil {
						durationBoundary = countBelow
					}
				}
			}
		}

		// Determine removal boundary based on strategy
		var removeCount int64
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

		// Remove old elements from the ZSET
		if removeCount > 0 {
			_, err := redisCli.ZPopMin(ctx, fieldKey, removeCount).Result()
			if err != nil {
				return fmt.Errorf("failed to remove old elements: %w", err)
			}
		}
	}

	return nil
}
