package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/leonkaihao/cache/pkg/consts"
	"github.com/leonkaihao/cache/pkg/model"
	redis "github.com/redis/go-redis/v9"
)

// redisTimeline implements the CacheTimeline interface using Redis backend.
type redisTimeline struct {
	name          string
	cli           *client
	retention     model.RetentionPolicy
	keyRetentions map[string]model.RetentionPolicy
	mu            sync.RWMutex
}

// Key generation helpers following the pattern: T@{name}/K/, T@{name}/K/{key}/TS/, T@{name}/K/{key}/{ts}

func formatTimelineKeys(name string) string {
	return fmt.Sprintf("%s%s/%s", consts.TIMELINE_PREFIX, name, consts.KEYS_PREFIX)
}

func formatTimelineTS(name, key string) string {
	return fmt.Sprintf("%s%s/%s%s/%s", consts.TIMELINE_PREFIX, name, consts.KEYS_PREFIX, key, consts.TS_PREFIX)
}

func formatTimelineData(name, key string, tsMicros int64) string {
	return fmt.Sprintf("%s%s/%s%s/%d", consts.TIMELINE_PREFIX, name, consts.KEYS_PREFIX, key, tsMicros)
}

func formatTimelineRetention(name string) string {
	return fmt.Sprintf("%s%s/%s", consts.TIMELINE_PREFIX, name, consts.RETENTION_PREFIX)
}

func formatTimelineKeyRetention(name, key string) string {
	return fmt.Sprintf("%s%s/%s%s", consts.TIMELINE_PREFIX, name, consts.RETENTION_PREFIX, key)
}

// Label key helpers: T@{name}/L/, T@{name}/L/{label}, T@{name}/K/{key}/L

func formatTimelineLabels(name string) string {
	return fmt.Sprintf("%s%s/%s", consts.TIMELINE_PREFIX, name, consts.LABELS_PREFIX)
}

func formatTimelineLabel(name, label string) string {
	return fmt.Sprintf("%s%s/%s%s", consts.TIMELINE_PREFIX, name, consts.LABELS_PREFIX, label)
}

func formatTimelineKeyLabels(name, key string) string {
	return fmt.Sprintf("%s%s/%s%s/%s", consts.TIMELINE_PREFIX, name, consts.KEYS_PREFIX, key, consts.LABELS_PREFIX)
}

// normalizeTimestamp truncates a time.Time to microsecond precision and returns microseconds since epoch.
func normalizeTimestamp(ts time.Time) int64 {
	return ts.Truncate(time.Microsecond).UnixMicro()
}

// Name returns the timeline name.
func (t *redisTimeline) Name() string {
	return t.name
}

// Append adds or updates fields at the specified timestamp.
func (t *redisTimeline) Append(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Validate inputs
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	// Empty data is a no-op
	if len(data) == 0 {
		return nil
	}

	// Normalize timestamp
	tsMicros := normalizeTimestamp(ts)

	redisCli := t.cli.getRedisCli()
	dataKey := formatTimelineData(t.name, key, tsMicros)
	tsKey := formatTimelineTS(t.name, key)
	keysKey := formatTimelineKeys(t.name)

	// Check for field conflicts if force=false
	if !force {
		existing, err := redisCli.HGetAll(ctx, dataKey).Result()
		if err == nil {
			for field, newValue := range data {
				if existingValue, exists := existing[field]; exists && existingValue != newValue {
					return fmt.Errorf("field '%s' already exists at %s", field, time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
				}
			}
		}
	}

	// Add timestamp to ZSET
	if err := redisCli.ZAdd(ctx, tsKey, redis.Z{Score: float64(tsMicros), Member: fmt.Sprintf("%d", tsMicros)}).Err(); err != nil {
		return fmt.Errorf("failed to add timestamp: %w", err)
	}

	// Set fields in HASH
	for field, value := range data {
		if err := redisCli.HSet(ctx, dataKey, field, value).Err(); err != nil {
			return fmt.Errorf("failed to set field: %w", err)
		}
	}

	// Add key to keys set
	if err := redisCli.SAdd(ctx, keysKey, key).Err(); err != nil {
		return fmt.Errorf("failed to add key to set: %w", err)
	}

	// TODO: Enforce retention policy (task-042)

	return nil
}

// Insert adds or updates fields at the specified timestamp (supports out-of-order writes).
func (t *redisTimeline) Insert(ctx context.Context, key string, ts time.Time, data map[string]string, force bool) error {
	// Insert has same implementation as Append for Redis
	return t.Append(ctx, key, ts, data, force)
}

// GetAt returns the complete merged state at or before ts for each key.
func (t *redisTimeline) GetAt(ctx context.Context, keys []string, ts time.Time) ([]map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	tsMicros := normalizeTimestamp(ts)
	redisCli := t.cli.getRedisCli()
	results := make([]map[string]string, len(keys))
	be := model.NewBatchError(len(keys))

	// Phase 1: Pipeline all ZRangeArgs commands to fetch timestamps ≤ ts
	pipe := redisCli.Pipeline()
	zrangeCmds := make([]*redis.StringSliceCmd, len(keys))
	for i, key := range keys {
		tsKey := formatTimelineTS(t.name, key)
		zrangeCmds[i] = pipe.ZRangeArgs(ctx, redis.ZRangeArgs{
			Key:     tsKey,
			Start:   "-inf",
			Stop:    fmt.Sprintf("%d", tsMicros),
			ByScore: true,
		})
	}

	// Pipeline execution may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Phase 2: Collect timestamp results and build flat list of HGetAll operations
	type hgetSpec struct {
		keyIdx int
		tsStr  string
	}
	var hgetSpecs []hgetSpec

	for i, cmd := range zrangeCmds {
		timestamps, err := cmd.Result()
		if err != nil && err != redis.Nil {
			be.Add(keys[i], fmt.Errorf("failed to query timestamps: %w", err))
			continue
		}
		if len(timestamps) == 0 {
			results[i] = nil
			continue
		}
		// Add all HGetAll operations needed for this key
		for _, tsStr := range timestamps {
			hgetSpecs = append(hgetSpecs, hgetSpec{keyIdx: i, tsStr: tsStr})
		}
	}

	if len(hgetSpecs) == 0 {
		return results, be.OrNil()
	}

	// Phase 3: Pipeline all HGetAll commands for data fetching
	pipe = redisCli.Pipeline()
	hgetCmds := make([]*redis.MapStringStringCmd, len(hgetSpecs))
	for i, spec := range hgetSpecs {
		dataKey := formatTimelineData(t.name, keys[spec.keyIdx], mustParseInt64(spec.tsStr))
		hgetCmds[i] = pipe.HGetAll(ctx, dataKey)
	}

	// Pipeline execution may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Phase 4: Map HGetAll results back to keys, building merged state per key
	keyMerged := make([]map[string]string, len(keys))
	keyHasError := make([]bool, len(keys))

	for i, spec := range hgetSpecs {
		if keyHasError[spec.keyIdx] {
			continue // Skip processing for keys that already have errors
		}

		fields, err := hgetCmds[i].Result()
		if err != nil && err != redis.Nil {
			be.Add(keys[spec.keyIdx], fmt.Errorf("failed to get data at %s: %w", spec.tsStr, err))
			keyHasError[spec.keyIdx] = true
			continue
		}

		if keyMerged[spec.keyIdx] == nil {
			keyMerged[spec.keyIdx] = make(map[string]string)
		}
		// Merge fields in chronological order
		for k, v := range fields {
			keyMerged[spec.keyIdx][k] = v
		}
	}

	// Copy merged results to final results (only for keys without errors)
	for i := range keys {
		if !keyHasError[i] && keyMerged[i] != nil {
			results[i] = keyMerged[i]
		}
	}

	return results, be.OrNil()
}

// GetExact returns the raw sparse fields at the exact timestamp for each key.
func (t *redisTimeline) GetExact(ctx context.Context, keys []string, ts time.Time) ([]map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	tsMicros := normalizeTimestamp(ts)
	redisCli := t.cli.getRedisCli()
	results := make([]map[string]string, len(keys))
	be := model.NewBatchError(len(keys))

	// Pipeline all HGetAll commands
	pipe := redisCli.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(keys))
	for i, key := range keys {
		dataKey := formatTimelineData(t.name, key, tsMicros)
		cmds[i] = pipe.HGetAll(ctx, dataKey)
	}

	// Execute pipeline - may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Map results back to keys array
	for i, cmd := range cmds {
		result, err := cmd.Result()
		if err != nil && err != redis.Nil {
			be.Add(keys[i], fmt.Errorf("failed to get data: %w", err))
			continue
		}
		if len(result) == 0 {
			results[i] = nil
			continue
		}
		results[i] = result
	}

	return results, be.OrNil()
}

// GetRange returns all complete states in [start, end] for each key.
func (t *redisTimeline) GetRange(ctx context.Context, keys []string, start, end time.Time) ([][]*model.TimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	startMicros := normalizeTimestamp(start)
	endMicros := normalizeTimestamp(end)
	redisCli := t.cli.getRedisCli()
	results := make([][]*model.TimeValue, len(keys))
	be := model.NewBatchError(len(keys))

	// Phase 1: Pipeline all ZRangeArgs commands to fetch timestamps in [start, end]
	pipe := redisCli.Pipeline()
	rangeCmds := make([]*redis.StringSliceCmd, len(keys))
	for i, key := range keys {
		tsKey := formatTimelineTS(t.name, key)
		rangeCmds[i] = pipe.ZRangeArgs(ctx, redis.ZRangeArgs{
			Key:     tsKey,
			Start:   fmt.Sprintf("%d", startMicros),
			Stop:    fmt.Sprintf("%d", endMicros),
			ByScore: true,
		})
	}

	// Pipeline execution may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Phase 2: Pipeline all ZRangeArgs commands to fetch timestamps in [-inf, end]
	pipe = redisCli.Pipeline()
	allCmds := make([]*redis.StringSliceCmd, len(keys))
	for i, key := range keys {
		tsKey := formatTimelineTS(t.name, key)
		allCmds[i] = pipe.ZRangeArgs(ctx, redis.ZRangeArgs{
			Key:     tsKey,
			Start:   "-inf",
			Stop:    fmt.Sprintf("%d", endMicros),
			ByScore: true,
		})
	}

	// Pipeline execution may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Collect results and determine all HGetAll operations needed for merging
	type hgetSpec struct {
		keyIdx         int
		resultIdx      int   // Index in the key's result array
		resultTsMicros int64 // Timestamp of the result TimeValue
		mergeTsMicros  int64 // Timestamp of data to merge
	}
	var hgetSpecs []hgetSpec
	keyRangeTimestamps := make([][]string, len(keys))
	keyAllTimestamps := make([][]string, len(keys))

	for i := range keys {
		// Get range timestamps
		timestamps, err := rangeCmds[i].Result()
		if err != nil && err != redis.Nil {
			be.Add(keys[i], fmt.Errorf("failed to query timestamps: %w", err))
			continue
		}
		if len(timestamps) == 0 {
			results[i] = nil
			continue
		}
		keyRangeTimestamps[i] = timestamps

		// Get all timestamps for merging
		allTimestamps, err := allCmds[i].Result()
		if err != nil && err != redis.Nil {
			be.Add(keys[i], fmt.Errorf("failed to query all timestamps: %w", err))
			continue
		}
		keyAllTimestamps[i] = allTimestamps

		// For each result timestamp, determine which data timestamps need to be merged
		for resultIdx, tsStr := range timestamps {
			tsMicros := mustParseInt64(tsStr)
			for _, allTsStr := range allTimestamps {
				allTsMicros := mustParseInt64(allTsStr)
				if allTsMicros <= tsMicros {
					hgetSpecs = append(hgetSpecs, hgetSpec{
						keyIdx:         i,
						resultIdx:      resultIdx,
						resultTsMicros: tsMicros,
						mergeTsMicros:  allTsMicros,
					})
				}
			}
		}
	}

	if len(hgetSpecs) == 0 {
		return results, be.OrNil()
	}

	// Phase 3: Pipeline all HGetAll commands for data fetching
	pipe = redisCli.Pipeline()
	hgetCmds := make([]*redis.MapStringStringCmd, len(hgetSpecs))
	for i, spec := range hgetSpecs {
		dataKey := formatTimelineData(t.name, keys[spec.keyIdx], spec.mergeTsMicros)
		hgetCmds[i] = pipe.HGetAll(ctx, dataKey)
	}

	// Pipeline execution may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Build complete merged states for each timestamp using pipelined data
	type resultKey struct {
		keyIdx    int
		resultIdx int
	}
	resultMerged := make(map[resultKey]map[string]string)
	keyHasError := make([]bool, len(keys))

	for i, spec := range hgetSpecs {
		if keyHasError[spec.keyIdx] {
			continue
		}

		fields, err := hgetCmds[i].Result()
		if err != nil && err != redis.Nil {
			be.Add(keys[spec.keyIdx], fmt.Errorf("failed to get data at %d: %w", spec.mergeTsMicros, err))
			keyHasError[spec.keyIdx] = true
			continue
		}

		rk := resultKey{keyIdx: spec.keyIdx, resultIdx: spec.resultIdx}
		if resultMerged[rk] == nil {
			resultMerged[rk] = make(map[string]string)
		}
		// Merge fields in chronological order
		for k, v := range fields {
			resultMerged[rk][k] = v
		}
	}

	// Build final result array
	for i := range keys {
		if keyHasError[i] {
			continue
		}
		if keyRangeTimestamps[i] == nil {
			continue
		}

		var tvs []*model.TimeValue
		for resultIdx, tsStr := range keyRangeTimestamps[i] {
			tsMicros := mustParseInt64(tsStr)
			rk := resultKey{keyIdx: i, resultIdx: resultIdx}
			tvs = append(tvs, &model.TimeValue{
				Time:  time.UnixMicro(tsMicros),
				Value: resultMerged[rk],
			})
		}
		results[i] = tvs
	}

	return results, be.OrNil()
}

// GetLatest returns the complete merged state at the most recent timestamp for each key.
func (t *redisTimeline) GetLatest(ctx context.Context, keys []string) ([]map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	results := make([]map[string]string, len(keys))
	be := model.NewBatchError(len(keys))

	// Phase 1: Pipeline all ZRange commands to fetch timestamps
	pipe := redisCli.Pipeline()
	zrangeCmds := make([]*redis.StringSliceCmd, len(keys))
	for i, key := range keys {
		tsKey := formatTimelineTS(t.name, key)
		zrangeCmds[i] = pipe.ZRange(ctx, tsKey, 0, -1)
	}

	// Pipeline execution may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Phase 2: Collect all timestamp results and build flat list of HGetAll operations
	type hgetSpec struct {
		keyIdx int
		tsStr  string
	}
	var hgetSpecs []hgetSpec

	for i, cmd := range zrangeCmds {
		timestamps, err := cmd.Result()
		if err != nil && err != redis.Nil {
			be.Add(keys[i], fmt.Errorf("failed to query timestamps: %w", err))
			continue
		}
		if len(timestamps) == 0 {
			results[i] = nil
			continue
		}
		// Add all HGetAll operations needed for this key
		for _, tsStr := range timestamps {
			hgetSpecs = append(hgetSpecs, hgetSpec{keyIdx: i, tsStr: tsStr})
		}
	}

	if len(hgetSpecs) == 0 {
		return results, be.OrNil()
	}

	// Phase 3: Pipeline all HGetAll commands for data fetching
	pipe = redisCli.Pipeline()
	hgetCmds := make([]*redis.MapStringStringCmd, len(hgetSpecs))
	for i, spec := range hgetSpecs {
		dataKey := formatTimelineData(t.name, keys[spec.keyIdx], mustParseInt64(spec.tsStr))
		hgetCmds[i] = pipe.HGetAll(ctx, dataKey)
	}

	// Pipeline execution may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Phase 4: Map HGetAll results back to keys, building merged state per key
	keyMerged := make([]map[string]string, len(keys))
	for i, spec := range hgetSpecs {
		fields, err := hgetCmds[i].Result()
		if err != nil && err != redis.Nil {
			be.Add(keys[spec.keyIdx], fmt.Errorf("failed to get data: %w", err))
			// Mark this key as having an error so we don't set incomplete results
			if keyMerged[spec.keyIdx] == nil {
				keyMerged[spec.keyIdx] = map[string]string{} // Use empty map as error marker
			}
			continue
		}
		if keyMerged[spec.keyIdx] == nil {
			keyMerged[spec.keyIdx] = make(map[string]string)
		}
		// Merge fields in chronological order
		for k, v := range fields {
			keyMerged[spec.keyIdx][k] = v
		}
	}

	// Copy merged results to final results (only for keys without errors)
	for i := range keys {
		if len(keyMerged[i]) > 0 {
			results[i] = keyMerged[i]
		}
	}

	return results, be.OrNil()
}

// Timeline returns all complete states for the key in chronological order.
func (t *redisTimeline) Timeline(ctx context.Context, key string) ([]*model.TimeValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	redisCli := t.cli.getRedisCli()
	tsKey := formatTimelineTS(t.name, key)

	timestamps, err := redisCli.ZRange(ctx, tsKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query timestamps: %w", err)
	}

	var result []*model.TimeValue
	for i, tsStr := range timestamps {
		tsMicros := mustParseInt64(tsStr)

		merged := make(map[string]string)
		for j := 0; j <= i; j++ {
			dataKey := formatTimelineData(t.name, key, mustParseInt64(timestamps[j]))
			fields, err := redisCli.HGetAll(ctx, dataKey).Result()
			if err != nil {
				continue
			}
			for k, v := range fields {
				merged[k] = v
			}
		}

		result = append(result, &model.TimeValue{
			Time:  time.UnixMicro(tsMicros),
			Value: merged,
		})
	}

	return result, nil
}

// GetAffectedRange returns all states from insertedAt (inclusive) to end of timeline.
func (t *redisTimeline) GetAffectedRange(ctx context.Context, key string, insertedAt time.Time) ([]*model.TimeValue, error) {
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
	tsKey := formatTimelineTS(t.name, key)

	// Phase 1: Pipeline both ZRangeArgs (affected range) and ZRange (all timestamps) commands
	pipe := redisCli.Pipeline()
	affectedCmd := pipe.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     tsKey,
		Start:   fmt.Sprintf("%d", insertedMicros),
		Stop:    "+inf",
		ByScore: true,
	})
	allCmd := pipe.ZRange(ctx, tsKey, 0, -1)

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to query timestamps: %w", err)
	}

	timestamps, err := affectedCmd.Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to query timestamps: %w", err)
	}

	allTimestamps, err := allCmd.Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to query all timestamps: %w", err)
	}

	if len(timestamps) == 0 {
		return nil, nil
	}

	// Phase 2: Collect results and determine all HGetAll operations needed for merging
	type hgetSpec struct {
		resultIdx     int   // Index in the result array
		tsMicros      int64 // Result timestamp
		mergeTsMicros int64 // Data timestamp to merge
	}
	var hgetSpecs []hgetSpec

	for resultIdx, tsStr := range timestamps {
		tsMicros := mustParseInt64(tsStr)
		// For each result timestamp, collect all timestamps ≤ it for merging
		for _, allTsStr := range allTimestamps {
			allTsMicros := mustParseInt64(allTsStr)
			if allTsMicros <= tsMicros {
				hgetSpecs = append(hgetSpecs, hgetSpec{
					resultIdx:     resultIdx,
					tsMicros:      tsMicros,
					mergeTsMicros: allTsMicros,
				})
			}
		}
	}

	// Phase 3: Pipeline all HGetAll commands for data fetching
	pipe = redisCli.Pipeline()
	hgetCmds := make([]*redis.MapStringStringCmd, len(hgetSpecs))
	for i, spec := range hgetSpecs {
		dataKey := formatTimelineData(t.name, key, spec.mergeTsMicros)
		hgetCmds[i] = pipe.HGetAll(ctx, dataKey)
	}

	// Pipeline execution may fail, but individual commands may have succeeded
	_, _ = pipe.Exec(ctx)

	// Phase 4: Build merged state for each affected timestamp using pipelined data
	result := make([]*model.TimeValue, len(timestamps))
	resultMerged := make([]map[string]string, len(timestamps))

	for i, spec := range hgetSpecs {
		fields, err := hgetCmds[i].Result()
		if err != nil && err != redis.Nil {
			continue // Skip failed fetches
		}

		if resultMerged[spec.resultIdx] == nil {
			resultMerged[spec.resultIdx] = make(map[string]string)
		}
		// Merge fields in chronological order
		for k, v := range fields {
			resultMerged[spec.resultIdx][k] = v
		}
	}

	// Build final result array
	for i, tsStr := range timestamps {
		tsMicros := mustParseInt64(tsStr)
		result[i] = &model.TimeValue{
			Time:  time.UnixMicro(tsMicros),
			Value: resultMerged[i],
		}
	}

	return result, nil
}

// Keys returns all logical keys, optionally filtered by labels.
func (t *redisTimeline) Keys(ctx context.Context, labelFilters ...[]string) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)

	if len(labelFilters) == 0 {
		result, err := redisCli.SMembers(ctx, keysKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get keys: %w", err)
		}
		return result, nil
	}

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

	ret := make([]string, 0, len(base))
	for key := range base {
		ret = append(ret, key)
	}
	return ret, nil
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
		pipe.SAdd(ctx, labelsKey, label)    // T@{name}/L/ ← label name
		pipe.SAdd(ctx, labelKey, key)       // T@{name}/L/{label} ← key (inverted index)
		pipe.SAdd(ctx, keyLabelsKey, label) // T@{name}/K/{key}/L ← label (forward index)
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
		pipe.SRem(ctx, labelKey, key)       // T@{name}/L/{label} ← remove key
		pipe.SRem(ctx, keyLabelsKey, label) // T@{name}/K/{key}/L ← remove label
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

// Remove removes the specified keys from the timeline, cleaning up label indexes.
func (t *redisTimeline) Remove(ctx context.Context, keys []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)

	for _, key := range keys {
		// Read forward label index to clean up inverted index.
		keyLabelsKey := formatTimelineKeyLabels(t.name, key)
		labels, err := redisCli.SMembers(ctx, keyLabelsKey).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("failed to read labels for key %s: %w", key, err)
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
		// Delete timestamps ZSET.
		pipe.Del(ctx, formatTimelineTS(t.name, key))

		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			return fmt.Errorf("failed to remove key %s: %w", key, err)
		}
	}

	return nil
}

// Clear removes all data from the timeline but keeps the timeline instance.
func (t *redisTimeline) Clear(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)

	// Get all keys and remove them (which also cleans up label indexes).
	keys, err := redisCli.SMembers(ctx, keysKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get keys: %w", err)
	}
	if len(keys) > 0 {
		if err := t.Remove(ctx, keys); err != nil {
			return err
		}
	}

	// Clear label name set and all inverted label sets.
	labelsKey := formatTimelineLabels(t.name)
	labelNames, err := redisCli.SMembers(ctx, labelsKey).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("failed to get label names: %w", err)
	}
	if len(labelNames) > 0 {
		pipe := redisCli.Pipeline()
		for _, label := range labelNames {
			pipe.Del(ctx, formatTimelineLabel(t.name, label))
		}
		pipe.Del(ctx, labelsKey)
		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			return fmt.Errorf("failed to delete label sets: %w", err)
		}
	} else {
		_ = redisCli.Del(ctx, labelsKey)
	}

	// Clear retention policies.
	retentionKey := formatTimelineRetention(t.name)
	if err := redisCli.Del(ctx, retentionKey).Err(); err != nil {
		return fmt.Errorf("failed to delete retention: %w", err)
	}

	return nil
}

// Delete removes the timeline instance from the client.
func (t *redisTimeline) Delete(ctx context.Context) error {
	if err := t.Clear(ctx); err != nil {
		return err
	}
	return t.cli.RemoveTimeline(t.name)
}

// SetRetention sets the retention policy for the timeline.
func (t *redisTimeline) SetRetention(policy model.RetentionPolicy) error {
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

	redisCli := t.cli.getRedisCli()
	retentionKey := formatTimelineRetention(t.name)

	ctx := context.Background()
	_, err := redisCli.HSet(ctx, retentionKey,
		"max_count", fmt.Sprintf("%d", policy.MaxCount),
		"max_duration", fmt.Sprintf("%d", policy.MaxDuration.Microseconds()),
		"strategy", fmt.Sprintf("%d", policy.Strategy),
	).Result()
	if err != nil {
		return fmt.Errorf("failed to set retention: %w", err)
	}

	t.retention = policy
	return nil
}

// GetRetention returns the timeline's default retention policy.
func (t *redisTimeline) GetRetention() model.RetentionPolicy {
	t.mu.RLock()
	defer t.mu.RUnlock()

	redisCli := t.cli.getRedisCli()
	retentionKey := formatTimelineRetention(t.name)

	ctx := context.Background()
	data, err := redisCli.HGetAll(ctx, retentionKey).Result()

	if err == nil && len(data) > 0 {
		policy := model.RetentionPolicy{}
		if maxCount, ok := data["max_count"]; ok {
			policy.MaxCount = int(mustParseInt64(maxCount))
		}
		if maxDuration, ok := data["max_duration"]; ok {
			policy.MaxDuration = time.Duration(mustParseInt64(maxDuration)) * time.Microsecond
		}
		if strategy, ok := data["strategy"]; ok {
			policy.Strategy = model.RetentionStrategy(mustParseInt64(strategy))
		}
		t.retention = policy
		return policy
	}

	return t.retention
}

// SetKeyRetention sets the retention policy for a specific key.
func (t *redisTimeline) SetKeyRetention(key string, policy model.RetentionPolicy) error {
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

	redisCli := t.cli.getRedisCli()
	retentionKey := formatTimelineKeyRetention(t.name, key)

	ctx := context.Background()
	_, err := redisCli.HSet(ctx, retentionKey,
		"max_count", fmt.Sprintf("%d", policy.MaxCount),
		"max_duration", fmt.Sprintf("%d", policy.MaxDuration.Microseconds()),
		"strategy", fmt.Sprintf("%d", policy.Strategy),
	).Result()
	if err != nil {
		return fmt.Errorf("failed to set key retention: %w", err)
	}

	t.keyRetentions[key] = policy
	return nil
}

// GetKeyRetention returns the retention policy for a specific key.
func (t *redisTimeline) GetKeyRetention(key string) model.RetentionPolicy {
	t.mu.RLock()
	defer t.mu.RUnlock()

	redisCli := t.cli.getRedisCli()
	retentionKey := formatTimelineKeyRetention(t.name, key)

	ctx := context.Background()
	data, err := redisCli.HGetAll(ctx, retentionKey).Result()

	if err == nil && len(data) > 0 {
		policy := model.RetentionPolicy{}
		if maxCount, ok := data["max_count"]; ok {
			policy.MaxCount = int(mustParseInt64(maxCount))
		}
		if maxDuration, ok := data["max_duration"]; ok {
			policy.MaxDuration = time.Duration(mustParseInt64(maxDuration)) * time.Microsecond
		}
		if strategy, ok := data["strategy"]; ok {
			policy.Strategy = model.RetentionStrategy(mustParseInt64(strategy))
		}
		t.keyRetentions[key] = policy
		return policy
	}

	if policy, ok := t.keyRetentions[key]; ok {
		return policy
	}
	return t.retention
}

// Helper function to parse int64 from string
func mustParseInt64(s string) int64 {
	var result int64
	_, _ = fmt.Sscanf(s, "%d", &result)
	return result
}
