package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedisTimeline_FieldKeyPattern tests the per-field ZSET key pattern
func TestRedisTimeline_FieldKeyPattern(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Append multiple fields
	err := tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value1",
		"field2": "value2",
		"field3": "value3",
	}, false)
	require.NoError(t, err)

	// Verify Redis keys exist with correct pattern: T@{name}/K/{key}/F/{field}
	redisCli := cli.(*client).getRedisCli()

	field1Key := "T@test_timeline/K/key1/F/field1"
	field2Key := "T@test_timeline/K/key1/F/field2"
	field3Key := "T@test_timeline/K/key1/F/field3"

	exists, err := redisCli.Exists(ctx, field1Key, field2Key, field3Key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(3), exists, "all 3 field keys should exist")

	// Verify ZSET contents (member format: "{ts}:{value}")
	members, err := redisCli.ZRange(ctx, field1Key, 0, -1).Result()
	require.NoError(t, err)
	assert.Len(t, members, 1)

	// Decode member
	_, value, err := decodeMember(members[0])
	require.NoError(t, err)
	assert.Equal(t, "value1", value)
}

// TestRedisTimeline_MemberEncodingDecoding tests the "{ts}:{value}" format
func TestRedisTimeline_MemberEncodingDecoding(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	// Append data
	err := tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value:with:colons",
	}, false)
	require.NoError(t, err)

	// Read back and verify encoding works with colons in value
	results, err := tl.GetAt(ctx, []string{"key1"}, now, model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, results[0])
	assert.Equal(t, "value:with:colons", results[0]["field1"].Value)
	assert.Equal(t, now, results[0]["field1"].Time)
}

// TestRedisTimeline_PerFieldRetentionCleanup tests Redis ZSET cleanup per field
func TestRedisTimeline_PerFieldRetentionCleanup(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	base := time.Now().Truncate(time.Microsecond)

	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    2,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	}})

	// Field 'a' gets 4 updates
	for i := 0; i < 4; i++ {
		err := tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Second), map[string]string{
			"a": fmt.Sprintf("a%d", i),
		}, false)
		require.NoError(t, err)
	}

	// Field 'b' gets only 1 update
	err := tl.Append(ctx, "key1", base, map[string]string{"b": "b0"}, false)
	require.NoError(t, err)

	// Verify Redis ZSETs have correct counts
	redisCli := cli.(*client).getRedisCli()

	fieldAKey := "T@test_timeline/K/key1/F/a"
	fieldBKey := "T@test_timeline/K/key1/F/b"

	countA, err := redisCli.ZCard(ctx, fieldAKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), countA, "field 'a' should have 2 entries after retention")

	countB, err := redisCli.ZCard(ctx, fieldBKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), countB, "field 'b' should have 1 entry (not affected by retention)")

	// Verify values are correct
	latest, err := tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, "a3", latest[0]["a"].Value)
	assert.Equal(t, "b0", latest[0]["b"].Value)
}

// TestRedisTimeline_RetentionDurationOnly tests duration-based retention
func TestRedisTimeline_RetentionDurationOnly(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	base := time.Now().Truncate(time.Microsecond)

	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    0,
		MaxDuration: 3 * time.Second,
		Strategy:    model.RetentionMax,
	}})

	// Add 5 points spread over 5 seconds
	for i := 0; i < 5; i++ {
		err := tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Second), map[string]string{
			"v": fmt.Sprintf("%d", i),
		}, false)
		require.NoError(t, err)
	}

	// Only last 4 points should remain (within 3 seconds of most recent)
	// Most recent is at base+4s, so cutoff is base+1s
	// Points at base+1s, base+2s, base+3s, base+4s should remain
	timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
	require.NoError(t, err)
	require.Contains(t, timeline, "v")
	assert.Equal(t, 4, len(timeline["v"]))
	assert.Equal(t, "1", timeline["v"][0].Value)
	assert.Equal(t, "4", timeline["v"][3].Value)
}

// TestRedisTimeline_RetentionStrategies tests RetentionMax vs RetentionMin
func TestRedisTimeline_RetentionStrategies(t *testing.T) {
	skipIfNoRedis(t)

	// Test RetentionMax (keep more data)
	t.Run("RetentionMax", func(t *testing.T) {
		cli := newTestRedisClient(t)
		tl := cli.Timeline("test_max")
		defer func() { _ = tl.Delete(context.Background()) }()

		ctx := context.Background()
		base := time.Now().Truncate(time.Microsecond)

		tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
			MaxCount:    3,
			MaxDuration: 2 * time.Second,
			Strategy:    model.RetentionMax,
		}})

		// Add 5 points
		for i := 0; i < 5; i++ {
			_ = tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Second), map[string]string{
				"v": fmt.Sprintf("%d", i),
			}, false)
		}

		// RetentionMax: keep min(3 by count, 3 by duration) = 3
		// Most recent is base+4s, duration cutoff is base+2s
		// By count: keep last 3 (indices 2,3,4)
		// By duration: keep >= base+2s (indices 2,3,4)
		// Result: keep 3 points
		timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
		require.NoError(t, err)
		assert.Len(t, timeline["v"], 3)
	})

	// Test RetentionMin (keep less data)
	t.Run("RetentionMin", func(t *testing.T) {
		cli := newTestRedisClient(t)
		tl := cli.Timeline("test_min")
		defer func() { _ = tl.Delete(context.Background()) }()

		ctx := context.Background()
		base := time.Now().Truncate(time.Microsecond)

		tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
			MaxCount:    4,
			MaxDuration: 2 * time.Second,
			Strategy:    model.RetentionMin,
		}})

		// Add 5 points
		for i := 0; i < 5; i++ {
			_ = tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Second), map[string]string{
				"v": fmt.Sprintf("%d", i),
			}, false)
		}

		// RetentionMin: keep max(1 by count, 3 by duration) = remove max
		// By count: remove first 1 (keep indices 1,2,3,4)
		// By duration: remove first 2 (keep indices 2,3,4)
		// Result: keep 3 points (indices 2,3,4)
		timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
		require.NoError(t, err)
		assert.Len(t, timeline["v"], 3)
	})
}

// TestRedisTimeline_GlobalTSZSET tests the global timestamp ZSET
func TestRedisTimeline_GlobalTSZSET(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	t1 := time.Now().Truncate(time.Microsecond)
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)

	// Add keys at different times
	_ = tl.Append(ctx, "k1", t1, map[string]string{"f": "v1"}, false)
	_ = tl.Append(ctx, "k2", t2, map[string]string{"f": "v2"}, false)
	_ = tl.Append(ctx, "k3", t3, map[string]string{"f": "v3"}, false)

	// Verify Redis ZSET exists
	redisCli := cli.(*client).getRedisCli()
	globalTSKey := formatTimelineGlobalTS("test_timeline")

	count, err := redisCli.ZCard(ctx, globalTSKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	// Verify scores (timestamps)
	scores, err := redisCli.ZRangeWithScores(ctx, globalTSKey, 0, -1).Result()
	require.NoError(t, err)
	assert.Equal(t, float64(normalizeTimestamp(t1)), scores[0].Score)
	assert.Equal(t, float64(normalizeTimestamp(t2)), scores[1].Score)
	assert.Equal(t, float64(normalizeTimestamp(t3)), scores[2].Score)

	// Test Keys with AfterTs using the ZSET
	keys, err := tl.Keys(ctx, model.FilterOptions{AfterTs: &t1})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k2", "k3"}, keys)
}

// TestRedisTimeline_ScanFieldDiscovery tests SCAN for discovering field keys
func TestRedisTimeline_ScanFieldDiscovery(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Append with 10 fields
	data := make(map[string]string)
	for i := 0; i < 10; i++ {
		data[fmt.Sprintf("field%d", i)] = fmt.Sprintf("value%d", i)
	}
	err := tl.Append(ctx, "key1", now, data, false)
	require.NoError(t, err)

	// Verify SCAN discovers all 10 fields
	redisCli := cli.(*client).getRedisCli()
	pattern := "T@test_timeline/K/key1/F/*"

	var keys []string
	iter := redisCli.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	require.NoError(t, iter.Err())
	assert.Len(t, keys, 10)

	// Verify Timeline returns all 10 fields
	timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, timeline, 10)
}

// TestRedisTimeline_RemoveCleanupFieldKeys tests that Remove deletes all field ZSETs
func TestRedisTimeline_RemoveCleanupFieldKeys(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Append multiple fields
	err := tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value1",
		"field2": "value2",
		"field3": "value3",
	}, false)
	require.NoError(t, err)

	// Verify keys exist
	redisCli := cli.(*client).getRedisCli()
	pattern := "T@test_timeline/K/key1/F/*"

	var keysBefore []string
	iter := redisCli.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keysBefore = append(keysBefore, iter.Val())
	}
	require.Len(t, keysBefore, 3)

	// Remove the key
	err = tl.Remove(ctx, []string{"key1"})
	require.NoError(t, err)

	// Verify all field keys are deleted
	var keysAfter []string
	iter = redisCli.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keysAfter = append(keysAfter, iter.Val())
	}
	assert.Empty(t, keysAfter)
}

// TestRedisTimeline_Pipelining tests that queries use Redis pipelines
func TestRedisTimeline_Pipelining(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	base := time.Now().Truncate(time.Microsecond)

	// Create data with multiple fields and timestamps
	for i := 0; i < 10; i++ {
		data := make(map[string]string)
		for j := 0; j < 5; j++ {
			data[fmt.Sprintf("field%d", j)] = fmt.Sprintf("v%d_%d", i, j)
		}
		err := tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Second), data, false)
		require.NoError(t, err)
	}

	// GetRange should use pipelining (hard to test directly, but verify it works)
	results, err := tl.GetRange(ctx, []string{"key1"}, base, base.Add(9*time.Second), model.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, results[0], 10)

	// GetLatest should use pipelining for multiple fields
	latest, err := tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, latest[0], 5)

	// Timeline should pipeline all field queries
	timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, timeline, 5)
	for _, series := range timeline {
		assert.Len(t, series, 10)
	}
}

// TestRedisTimeline_ContextCancellation tests context handling
func TestRedisTimeline_ContextCancellation(t *testing.T) {
	skipIfNoRedis(t)

	cli := newTestRedisClient(t)
	tl := cli.Timeline("test_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tl.Append(ctx, "key1", time.Now(), map[string]string{"f": "v"}, false)
	assert.Error(t, err)
}

// Helper function to skip test if Redis is not available
func skipIfNoRedis(t *testing.T) {
	// This would check if Redis is available
	// For now, we assume it's available in test environment
}

// Helper function to create a test Redis client
func newTestRedisClient(t *testing.T) model.CacheClient {
	cli := NewClient("localhost:6379", "", 0)
	return cli
}
