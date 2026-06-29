//go:build integration
// +build integration

package redis

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: These tests require a running Redis instance
// Run with: go test -tags=integration

func TestRedisTimeline_BasicOperations(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	now := time.Now()

	// Test Append
	err := tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value1",
		"field2": "value2",
	}, false)
	require.NoError(t, err)

	// Test GetAt
	results, err := tl.GetAt(ctx, []string{"key1"}, now)
	require.NoError(t, err)
	require.Len(t, results, 1)
	state := results[0]
	assert.Equal(t, "value1", state["field1"])
	assert.Equal(t, "value2", state["field2"])
}

func TestRedisTimeline_SparseUpdates(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	// First update
	err := tl.Append(ctx, "key1", t1, map[string]string{
		"field1": "value1",
		"field2": "value2",
	}, false)
	require.NoError(t, err)

	// Second update (sparse)
	err = tl.Append(ctx, "key1", t2, map[string]string{
		"field1": "updated",
	}, false)
	require.NoError(t, err)

	// Get merged state
	results, err := tl.GetAt(ctx, []string{"key1"}, t2)
	require.NoError(t, err)
	require.Len(t, results, 1)
	state := results[0]
	assert.Equal(t, "updated", state["field1"])
	assert.Equal(t, "value2", state["field2"])
}

func TestRedisTimeline_RetentionPolicy(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()

	// Test MaxCount retention
	tl.WithRetention(model.RetentionPolicy{
		MaxCount:    2,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	})

	// Write 3 points
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(1 * time.Hour)
	t3 := t2.Add(1 * time.Hour)

	var err error
	err = tl.Append(ctx, "k1", t1, map[string]string{"v": "1"}, false)
	require.NoError(t, err)
	err = tl.Append(ctx, "k1", t2, map[string]string{"v": "2"}, false)
	require.NoError(t, err)
	err = tl.Append(ctx, "k1", t3, map[string]string{"v": "3"}, false)
	require.NoError(t, err)

	// Verify: only 2 points remain (oldest removed)
	timeline, err := tl.Timeline(ctx, "k1")
	require.NoError(t, err)
	assert.Equal(t, 2, len(timeline), "should keep exactly 2 points")
	assert.Equal(t, t2.UnixMicro(), timeline[0].Time.UnixMicro())
	assert.Equal(t, "2", timeline[0].Value["v"])
	assert.Equal(t, t3.UnixMicro(), timeline[1].Time.UnixMicro())
	assert.Equal(t, "3", timeline[1].Value["v"])
}

func TestRedisTimeline_RetentionDurationOnly(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_retention_duration")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()

	// Test duration-only retention
	tl.WithRetention(model.RetentionPolicy{
		MaxCount:    0,
		MaxDuration: 2 * time.Hour,
		Strategy:    model.RetentionMax,
	})

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := base
	t2 := base.Add(1 * time.Hour)
	t3 := base.Add(2 * time.Hour)
	t4 := base.Add(3 * time.Hour) // This should trigger removal of t1

	var err error
	err = tl.Append(ctx, "k1", t1, map[string]string{"v": "1"}, false)
	require.NoError(t, err)
	err = tl.Append(ctx, "k1", t2, map[string]string{"v": "2"}, false)
	require.NoError(t, err)
	err = tl.Append(ctx, "k1", t3, map[string]string{"v": "3"}, false)
	require.NoError(t, err)
	err = tl.Append(ctx, "k1", t4, map[string]string{"v": "4"}, false)
	require.NoError(t, err)

	// Verify: only points within 2 hours of most recent remain
	timeline, err := tl.Timeline(ctx, "k1")
	require.NoError(t, err)
	assert.Equal(t, 3, len(timeline), "should keep points within 2h duration")
	assert.Equal(t, "2", timeline[0].Value["v"])
	assert.Equal(t, "3", timeline[1].Value["v"])
	assert.Equal(t, "4", timeline[2].Value["v"])
}

func TestRedisTimeline_RetentionStrategies(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)

	t.Run("RetentionMax", func(t *testing.T) {
		tl := cli.Timeline("test_retention_max")
		defer func() {
			_ = tl.Delete(context.Background())
		}()

		ctx := context.Background()

		// RetentionMax: keep MORE data (union of constraints)
		tl.WithRetention(model.RetentionPolicy{
			MaxCount:    3,
			MaxDuration: 90 * time.Minute,
			Strategy:    model.RetentionMax,
		})

		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		var err error
		for i := 0; i < 5; i++ {
			ts := base.Add(time.Duration(i) * 30 * time.Minute)
			err = tl.Append(ctx, "k1", ts, map[string]string{"v": fmt.Sprintf("%d", i)}, false)
			require.NoError(t, err)
		}

		// Count boundary: keep last 3 (remove first 2)
		// Duration boundary: keep last 90min = last 4 (remove first 1)
		// RetentionMax: min(2, 1) = 1, so keep last 4 points
		timeline, err := tl.Timeline(ctx, "k1")
		require.NoError(t, err)
		assert.Equal(t, 4, len(timeline), "RetentionMax should keep MORE data")
	})

	t.Run("RetentionMin", func(t *testing.T) {
		tl := cli.Timeline("test_retention_min")
		defer func() {
			_ = tl.Delete(context.Background())
		}()

		ctx := context.Background()

		// RetentionMin: keep LESS data (intersection of constraints)
		tl.WithRetention(model.RetentionPolicy{
			MaxCount:    3,
			MaxDuration: 90 * time.Minute,
			Strategy:    model.RetentionMin,
		})

		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		var err error
		for i := 0; i < 5; i++ {
			ts := base.Add(time.Duration(i) * 30 * time.Minute)
			err = tl.Append(ctx, "k1", ts, map[string]string{"v": fmt.Sprintf("%d", i)}, false)
			require.NoError(t, err)
		}

		// Count boundary: keep last 3 (remove first 2)
		// Duration boundary: keep last 90min = last 4 (remove first 1)
		// RetentionMin: max(2, 1) = 2, so keep last 3 points
		timeline, err := tl.Timeline(ctx, "k1")
		require.NoError(t, err)
		assert.Equal(t, 3, len(timeline), "RetentionMin should keep LESS data")
	})
}

func TestRedisTimeline_RetentionBoundaryEdgeCases(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)

	t.Run("ExactCount", func(t *testing.T) {
		tl := cli.Timeline("test_exact_count")
		defer func() {
			_ = tl.Delete(context.Background())
		}()

		ctx := context.Background()

		tl.WithRetention(model.RetentionPolicy{
			MaxCount:    3,
			MaxDuration: 0,
			Strategy:    model.RetentionMax,
		})

		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		var err error
		for i := 0; i < 3; i++ {
			ts := base.Add(time.Duration(i) * time.Hour)
			err = tl.Append(ctx, "k1", ts, map[string]string{"v": fmt.Sprintf("%d", i)}, false)
			require.NoError(t, err)
		}

		// Exactly at limit - no cleanup should occur
		timeline, err := tl.Timeline(ctx, "k1")
		require.NoError(t, err)
		assert.Equal(t, 3, len(timeline), "should keep all points when at exact limit")
	})

	t.Run("SinglePoint", func(t *testing.T) {
		tl := cli.Timeline("test_single_point")
		defer func() {
			_ = tl.Delete(context.Background())
		}()

		ctx := context.Background()

		tl.WithRetention(model.RetentionPolicy{
			MaxCount:    1,
			MaxDuration: 0,
			Strategy:    model.RetentionMax,
		})

		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		var err error
		for i := 0; i < 3; i++ {
			ts := base.Add(time.Duration(i) * time.Hour)
			err = tl.Append(ctx, "k1", ts, map[string]string{"v": fmt.Sprintf("%d", i)}, false)
			require.NoError(t, err)
		}

		// Should keep only the latest point
		timeline, err := tl.Timeline(ctx, "k1")
		require.NoError(t, err)
		assert.Equal(t, 1, len(timeline), "should keep only 1 point")
		assert.Equal(t, "2", timeline[0].Value["v"], "should keep the latest point")
	})

	t.Run("EmptyTimeline", func(t *testing.T) {
		tl := cli.Timeline("test_empty")
		defer func() {
			_ = tl.Delete(context.Background())
		}()

		ctx := context.Background()

		tl.WithRetention(model.RetentionPolicy{
			MaxCount:    2,
			MaxDuration: 1 * time.Hour,
			Strategy:    model.RetentionMax,
		})

		// Query empty timeline - should not error
		timeline, err := tl.Timeline(ctx, "k1")
		require.NoError(t, err)
		assert.Equal(t, 0, len(timeline), "empty timeline should remain empty")
	})
}

func TestRedisTimeline_RetentionConcurrency(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_retention_concurrent")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()

	// Set retention
	tl.WithRetention(model.RetentionPolicy{
		MaxCount:    10,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	})

	// Concurrent writes to same key
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ts := base.Add(time.Duration(idx) * time.Minute)
			_ = tl.Append(ctx, "k1", ts, map[string]string{"v": fmt.Sprintf("%d", idx)}, false)
		}(i)
	}
	wg.Wait()

	// Verify retention was enforced (may have slight variations due to concurrency)
	timeline, err := tl.Timeline(ctx, "k1")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(timeline), 10, "retention should limit to MaxCount even with concurrent writes")
}

func TestRedisTimeline_RetentionRedisCleanup(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_retention_cleanup")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()

	// Set retention
	tl.WithRetention(model.RetentionPolicy{
		MaxCount:    2,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	})

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := base
	t2 := base.Add(1 * time.Hour)
	t3 := base.Add(2 * time.Hour)

	// Write 3 points
	var err error
	err = tl.Append(ctx, "k1", t1, map[string]string{"v": "1"}, false)
	require.NoError(t, err)
	err = tl.Append(ctx, "k1", t2, map[string]string{"v": "2"}, false)
	require.NoError(t, err)
	err = tl.Append(ctx, "k1", t3, map[string]string{"v": "3"}, false)
	require.NoError(t, err)

	// Get Redis client to verify actual data deletion
	redisCli := cli.getRedisCli()

	// Verify timestamp ZSET has only 2 entries
	tsKey := fmt.Sprintf("T@test_retention_cleanup/K/k1/TS/")
	tsCount, err := redisCli.ZCard(ctx, tsKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), tsCount, "ZSET should have only 2 timestamps")

	// Verify old data HASH was deleted
	t1Micros := t1.Truncate(time.Microsecond).UnixMicro()
	oldDataKey := fmt.Sprintf("T@test_retention_cleanup/K/k1/%d", t1Micros)
	exists, err := redisCli.Exists(ctx, oldDataKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists, "old data HASH should be deleted from Redis")

	// Verify newer data HASHes still exist
	t2Micros := t2.Truncate(time.Microsecond).UnixMicro()
	t3Micros := t3.Truncate(time.Microsecond).UnixMicro()
	newDataKey2 := fmt.Sprintf("T@test_retention_cleanup/K/k1/%d", t2Micros)
	newDataKey3 := fmt.Sprintf("T@test_retention_cleanup/K/k1/%d", t3Micros)

	exists2, err := redisCli.Exists(ctx, newDataKey2).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists2, "newer data HASH should still exist")

	exists3, err := redisCli.Exists(ctx, newDataKey3).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists3, "newest data HASH should still exist")

	// Verify ZRANGE returns correct timestamps
	timestamps, err := redisCli.ZRangeWithScores(ctx, tsKey, 0, -1).Result()
	require.NoError(t, err)
	assert.Equal(t, 2, len(timestamps), "should have 2 timestamps in ZSET")
	assert.Equal(t, float64(t2Micros), timestamps[0].Score)
	assert.Equal(t, float64(t3Micros), timestamps[1].Score)
}

// --- 8.5: Redis timeline label operations ---

func TestRedisTimeline_Labels(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k2", now, map[string]string{"f": "v"}, false))

	require.NoError(t, tl.AddKeyLabels(ctx, "k1", []string{"alpha", "beta"}))

	ls, err := tl.KeyLabels(ctx, "k1")
	require.NoError(t, err)
	assert.True(t, ls["alpha"])
	assert.True(t, ls["beta"])

	ls2, err := tl.KeyLabels(ctx, "k2")
	require.NoError(t, err)
	assert.Empty(t, ls2)

	require.NoError(t, tl.RemoveKeyLabels(ctx, "k1", []string{"beta"}))
	ls, _ = tl.KeyLabels(ctx, "k1")
	assert.False(t, ls["beta"])
	assert.True(t, ls["alpha"])
}

func TestRedisTimeline_KeysWithLabelFilter(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k2", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k3", now, map[string]string{"f": "v"}, false))

	require.NoError(t, tl.AddKeyLabels(ctx, "k1", []string{"foo", "new"}))
	require.NoError(t, tl.AddKeyLabels(ctx, "k2", []string{"bar", "new"}))
	require.NoError(t, tl.AddKeyLabels(ctx, "k3", []string{"foo", "bee"}))

	// No filter — all keys
	keys, err := tl.Keys(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2", "k3"}, keys)

	// Single label
	keys, err = tl.Keys(ctx, []string{"foo"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k3"}, keys)

	// AND across two steps
	keys, err = tl.Keys(ctx, []string{"foo", "bar"}, []string{"new"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2"}, keys)
}

func TestRedisTimeline_LabelCleanupOnRemove(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k2", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.AddKeyLabels(ctx, "k1", []string{"alpha"}))
	require.NoError(t, tl.AddKeyLabels(ctx, "k2", []string{"alpha"}))

	require.NoError(t, tl.Remove(ctx, []string{"k1"}))

	keys, err := tl.Keys(ctx, []string{"alpha"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k2"}, keys)
}

// --- 8.6: Redis timeline batch query ---

func TestRedisTimeline_BatchGetLatest(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", t1, map[string]string{"a": "1"}, false))
	require.NoError(t, tl.Append(ctx, "k2", t1, map[string]string{"b": "2"}, false))

	results, err := tl.GetLatest(ctx, []string{"k1", "k2", "missing"})
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "1", results[0]["a"])
	assert.Equal(t, "2", results[1]["b"])
	assert.Nil(t, results[2])
}

func TestRedisTimeline_BatchGetAt(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	require.NoError(t, tl.Append(ctx, "k1", t1, map[string]string{"a": "old"}, false))
	require.NoError(t, tl.Append(ctx, "k2", t2, map[string]string{"b": "v"}, false))

	results, err := tl.GetAt(ctx, []string{"k1", "k2", "missing"}, t1)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "old", results[0]["a"])
	assert.Nil(t, results[1]) // k2 has no data at t1
	assert.Nil(t, results[2])
}

// --- GetUpdatedKeys tests ---

func TestRedisTimeline_GetUpdatedKeys_NoUpdatesAfterTimestamp(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	// Add some data at t1
	require.NoError(t, tl.Append(ctx, "k1", t1, map[string]string{"f": "v"}, false))

	// Query for updates after t2 (later than any data)
	keys, err := tl.GetUpdatedKeys(ctx, t2)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestRedisTimeline_GetUpdatedKeys_SingleKeyUpdated(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	// Add data at t2
	require.NoError(t, tl.Append(ctx, "k1", t2, map[string]string{"f": "v"}, false))

	// Query for updates after t1
	keys, err := tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1"}, keys)
}

func TestRedisTimeline_GetUpdatedKeys_MultipleKeysUpdated(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	// Add multiple keys at t2
	require.NoError(t, tl.Append(ctx, "k1", t2, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k2", t2, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k3", t2, map[string]string{"f": "v"}, false))

	// Query for updates after t1
	keys, err := tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2", "k3"}, keys)
}

func TestRedisTimeline_GetUpdatedKeys_KeyUpdatedMultipleTimes(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)

	// Update same key multiple times
	require.NoError(t, tl.Append(ctx, "k1", t2, map[string]string{"f": "v1"}, false))
	require.NoError(t, tl.Append(ctx, "k1", t3, map[string]string{"f": "v2"}, false))

	// Query for updates after t1 - should return k1 only once
	keys, err := tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1"}, keys)
}

func TestRedisTimeline_GetUpdatedKeys_ExactTimestampBoundary(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	// Add data at exactly t1
	require.NoError(t, tl.Append(ctx, "k1", t1, map[string]string{"f": "v"}, false))

	// Query for updates after t1 (exclusive) - should not include k1
	keys, err := tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.Empty(t, keys)

	// Add data at t2
	require.NoError(t, tl.Append(ctx, "k2", t2, map[string]string{"f": "v"}, false))

	// Query again - should now include k2 but not k1
	keys, err = tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k2"}, keys)
}

func TestRedisTimeline_GetUpdatedKeys_ContextCancellation(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	t1 := time.Now()
	_, err := tl.GetUpdatedKeys(ctx, t1)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestRedisTimeline_GetUpdatedKeys_EmptyTimeline(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()

	// Query on empty timeline
	keys, err := tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestRedisTimeline_GetUpdatedKeys_GlobalIndexCleanupAfterRemove(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	// Add data
	require.NoError(t, tl.Append(ctx, "k1", t2, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k2", t2, map[string]string{"f": "v"}, false))

	// Verify both keys are in the index
	keys, err := tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2"}, keys)

	// Remove k1
	require.NoError(t, tl.Remove(ctx, []string{"k1"}))

	// Verify k1 is removed from index
	keys, err = tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k2"}, keys)
}

func TestRedisTimeline_GetUpdatedKeys_GlobalIndexCleanupAfterClear(t *testing.T) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	// Add data
	require.NoError(t, tl.Append(ctx, "k1", t2, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k2", t2, map[string]string{"f": "v"}, false))

	// Verify keys are in the index
	keys, err := tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2"}, keys)

	// Clear timeline
	require.NoError(t, tl.Clear(ctx))

	// Verify index is cleared
	keys, err = tl.GetUpdatedKeys(ctx, t1)
	require.NoError(t, err)
	assert.Empty(t, keys)
}
