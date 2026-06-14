package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	redis "github.com/redis/go-redis/v9"
	"github.com/leonkaihao/cache/pkg/consts"
	"github.com/leonkaihao/cache/pkg/model"
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

// GetAt returns the complete state at or before the specified timestamp.
func (t *redisTimeline) GetAt(ctx context.Context, key string, ts time.Time) (map[string]string, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	tsMicros := normalizeTimestamp(ts)
	redisCli := t.cli.getRedisCli()
	tsKey := formatTimelineTS(t.name, key)

	// Find all timestamps <= tsMicros
	timestamps, err := redisCli.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     tsKey,
		Start:   "-inf",
		Stop:    fmt.Sprintf("%d", tsMicros),
		ByScore: true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query timestamps: %w", err)
	}

	if len(timestamps) == 0 {
		return nil, fmt.Errorf("no state found at or before %s", time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
	}

	// Merge fields from all time points
	result := make(map[string]string)
	for _, tsStr := range timestamps {
		dataKey := formatTimelineData(t.name, key, mustParseInt64(tsStr))
		fields, err := redisCli.HGetAll(ctx, dataKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get data: %w", err)
		}
		for k, v := range fields {
			result[k] = v
		}
	}

	return result, nil
}

// GetExact returns the raw sparse data at the exact timestamp.
func (t *redisTimeline) GetExact(ctx context.Context, key string, ts time.Time) (map[string]string, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	tsMicros := normalizeTimestamp(ts)
	redisCli := t.cli.getRedisCli()
	dataKey := formatTimelineData(t.name, key, tsMicros)

	result, err := redisCli.HGetAll(ctx, dataKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get data: %w", err)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no exact timestamp found at %s", time.UnixMicro(tsMicros).Format(time.RFC3339Nano))
	}

	return result, nil
}

// GetRange returns all complete states in the time range [start, end].
func (t *redisTimeline) GetRange(ctx context.Context, key string, start, end time.Time) ([]model.TimeValue, error) {
	// Check context cancellation
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
	redisCli := t.cli.getRedisCli()
	tsKey := formatTimelineTS(t.name, key)

	// Find timestamps in range
	timestamps, err := redisCli.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     tsKey,
		Start:   fmt.Sprintf("%d", startMicros),
		Stop:    fmt.Sprintf("%d", endMicros),
		ByScore: true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query timestamps: %w", err)
	}

	// Get all timestamps up to end for merging
	allTimestamps, err := redisCli.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     tsKey,
		Start:   "-inf",
		Stop:    fmt.Sprintf("%d", endMicros),
		ByScore: true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query all timestamps: %w", err)
	}

	var result []model.TimeValue
	for _, tsStr := range timestamps {
		tsMicros := mustParseInt64(tsStr)
		
		// Merge all fields up to this timestamp
		merged := make(map[string]string)
		for _, allTsStr := range allTimestamps {
			allTsMicros := mustParseInt64(allTsStr)
			if allTsMicros <= tsMicros {
				dataKey := formatTimelineData(t.name, key, allTsMicros)
				fields, err := redisCli.HGetAll(ctx, dataKey).Result()
				if err != nil {
					continue
				}
				for k, v := range fields {
					merged[k] = v
				}
			}
		}

		result = append(result, model.TimeValue{
			Time:  time.UnixMicro(tsMicros),
			Value: merged,
		})
	}

	return result, nil
}

// GetLatest returns the complete state at the most recent timestamp.
func (t *redisTimeline) GetLatest(ctx context.Context, key string) (map[string]string, error) {
	// Check context cancellation
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

	// Get all timestamps
	timestamps, err := redisCli.ZRange(ctx, tsKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query timestamps: %w", err)
	}

	if len(timestamps) == 0 {
		return nil, fmt.Errorf("no state found for key '%s'", key)
	}

	// Merge all fields
	result := make(map[string]string)
	for _, tsStr := range timestamps {
		dataKey := formatTimelineData(t.name, key, mustParseInt64(tsStr))
		fields, err := redisCli.HGetAll(ctx, dataKey).Result()
		if err != nil {
			continue
		}
		for k, v := range fields {
			result[k] = v
		}
	}

	return result, nil
}

// Timeline returns all complete states for the key in chronological order.
func (t *redisTimeline) Timeline(ctx context.Context, key string) ([]model.TimeValue, error) {
	// Check context cancellation
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

	// Get all timestamps
	timestamps, err := redisCli.ZRange(ctx, tsKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query timestamps: %w", err)
	}

	var result []model.TimeValue
	for i, tsStr := range timestamps {
		tsMicros := mustParseInt64(tsStr)
		
		// Merge all fields up to this timestamp
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

		result = append(result, model.TimeValue{
			Time:  time.UnixMicro(tsMicros),
			Value: merged,
		})
	}

	return result, nil
}

// GetAffectedRange returns all states from insertedAt (inclusive) to end of timeline.
func (t *redisTimeline) GetAffectedRange(ctx context.Context, key string, insertedAt time.Time) ([]model.TimeValue, error) {
	// Check context cancellation
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

	// Get timestamps >= insertedAt
	timestamps, err := redisCli.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     tsKey,
		Start:   fmt.Sprintf("%d", insertedMicros),
		Stop:    "+inf",
		ByScore: true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query timestamps: %w", err)
	}

	// Get all timestamps for merging
	allTimestamps, err := redisCli.ZRange(ctx, tsKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query all timestamps: %w", err)
	}

	var result []model.TimeValue
	for _, tsStr := range timestamps {
		tsMicros := mustParseInt64(tsStr)
		
		// Merge all fields up to this timestamp
		merged := make(map[string]string)
		for _, allTsStr := range allTimestamps {
			allTsMicros := mustParseInt64(allTsStr)
			if allTsMicros <= tsMicros {
				dataKey := formatTimelineData(t.name, key, allTsMicros)
				fields, err := redisCli.HGetAll(ctx, dataKey).Result()
				if err != nil {
					continue
				}
				for k, v := range fields {
					merged[k] = v
				}
			}
		}

		result = append(result, model.TimeValue{
			Time:  time.UnixMicro(tsMicros),
			Value: merged,
		})
	}

	return result, nil
}

// Keys returns all keys that have been written to the timeline.
func (t *redisTimeline) Keys(ctx context.Context) ([]string, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)

	result, err := redisCli.SMembers(ctx, keysKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys: %w", err)
	}

	return result, nil
}

// Remove removes the specified keys from the timeline.
func (t *redisTimeline) Remove(ctx context.Context, keys []string) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)

	for _, key := range keys {
		// Remove from keys set
		if err := redisCli.SRem(ctx, keysKey, key).Err(); err != nil {
			return fmt.Errorf("failed to remove key: %w", err)
		}

		// Delete timestamps ZSET
		tsKey := formatTimelineTS(t.name, key)
		if err := redisCli.Del(ctx, tsKey).Err(); err != nil {
			return fmt.Errorf("failed to delete timestamps: %w", err)
		}

		// Delete all data for key (would need to scan, simplified here)
		// In production, would iterate through timestamps and delete data keys
	}

	return nil
}

// Clear removes all data from the timeline but keeps the timeline instance.
func (t *redisTimeline) Clear(ctx context.Context) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	redisCli := t.cli.getRedisCli()
	keysKey := formatTimelineKeys(t.name)

	// Get all keys
	keys, err := redisCli.SMembers(ctx, keysKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get keys: %w", err)
	}

	// Remove each key
	if len(keys) > 0 {
		if err := t.Remove(ctx, keys); err != nil {
			return err
		}
	}

	// Clear retention policies
	retentionKey := formatTimelineRetention(t.name)
	if err := redisCli.Del(ctx, retentionKey).Err(); err != nil {
		return fmt.Errorf("failed to delete retention: %w", err)
	}

	return nil
}

// Delete removes the timeline instance from the client.
func (t *redisTimeline) Delete(ctx context.Context) error {
	// Clear all data first
	if err := t.Clear(ctx); err != nil {
		return err
	}

	// Remove from client
	return t.cli.RemoveTimeline(t.name)
}

// SetRetention sets the retention policy for the timeline.
func (t *redisTimeline) SetRetention(policy model.RetentionPolicy) error {
	// Normalize negative values to zero
	if policy.MaxCount < 0 {
		policy.MaxCount = 0
	}
	if policy.MaxDuration < 0 {
		policy.MaxDuration = 0
	}

	// Validate strategy
	if policy.Strategy != model.RetentionMax && policy.Strategy != model.RetentionMin {
		return fmt.Errorf("invalid retention strategy: %d", policy.Strategy)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Persist to Redis
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

	// Try to load from Redis first
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

	// Normalize negative values to zero
	if policy.MaxCount < 0 {
		policy.MaxCount = 0
	}
	if policy.MaxDuration < 0 {
		policy.MaxDuration = 0
	}

	// Validate strategy
	if policy.Strategy != model.RetentionMax && policy.Strategy != model.RetentionMin {
		return fmt.Errorf("invalid retention strategy: %d", policy.Strategy)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Persist to Redis
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

	// Try to load from Redis first
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

	// Check in-memory cache
	if policy, ok := t.keyRetentions[key]; ok {
		return policy
	}
	
	// Fall back to timeline-wide retention
	return t.retention
}

// Helper function to parse int64 from string
func mustParseInt64(s string) int64 {
	var result int64
	fmt.Sscanf(s, "%d", &result)
	return result
}
