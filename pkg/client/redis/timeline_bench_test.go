//go:build integration
// +build integration

package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
)

func BenchmarkRedisTimeline_Append(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tl.Append(ctx, "key1", time.Now(), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}
}

func BenchmarkRedisTimeline_GetAt(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: Add 1000 points
	now := time.Now()
	for i := 0; i < 1000; i++ {
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetAt(ctx, []string{"key1"}, now.Add(500*time.Millisecond))
	}
}

func BenchmarkRedisTimeline_GetAt_MultiKey(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: Add data for 5 keys with 10 timestamps each
	now := time.Now()
	keys := make([]string, 5)
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		keys[i] = key
		for j := 0; j < 10; j++ {
			_ = tl.Append(ctx, key, now.Add(time.Duration(j)*time.Millisecond), map[string]string{
				"field1": fmt.Sprintf("value1_%d_%d", i, j),
				"field2": fmt.Sprintf("value2_%d_%d", i, j),
				"field3": fmt.Sprintf("value3_%d_%d", i, j),
			}, false)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetAt(ctx, keys, now.Add(5*time.Millisecond))
	}
}

func BenchmarkRedisTimeline_GetExact(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: Add data for 10 keys at a specific timestamp
	ts := time.Now()
	keys := make([]string, 10)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		keys[i] = key
		_ = tl.Append(ctx, key, ts, map[string]string{
			"field1": fmt.Sprintf("value1_%d", i),
			"field2": fmt.Sprintf("value2_%d", i),
			"field3": fmt.Sprintf("value3_%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetExact(ctx, keys, ts)
	}
}

func BenchmarkRedisTimeline_GetLatest(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline_latest")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: Add data for 5 keys with 10 timestamps each
	now := time.Now()
	keys := make([]string, 5)
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		keys[i] = key
		for j := 0; j < 10; j++ {
			_ = tl.Append(ctx, key, now.Add(time.Duration(j)*time.Millisecond), map[string]string{
				"field1": fmt.Sprintf("value1_%d_%d", i, j),
				"field2": fmt.Sprintf("value2_%d_%d", i, j),
				"field3": fmt.Sprintf("value3_%d_%d", i, j),
			}, false)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetLatest(ctx, keys)
	}
}

func BenchmarkRedisTimeline_GetAffectedRange(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline_affected")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: Add 20 timestamps to a single key
	now := time.Now()
	key := "key1"
	for i := 0; i < 20; i++ {
		_ = tl.Append(ctx, key, now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field1": fmt.Sprintf("value1_%d", i),
			"field2": fmt.Sprintf("value2_%d", i),
			"field3": fmt.Sprintf("value3_%d", i),
		}, false)
	}

	// Query affected range from middle of timeline
	insertedAt := now.Add(10 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetAffectedRange(ctx, key, insertedAt)
	}
}

func BenchmarkRedisTimeline_GetRange(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline_exact")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: Add data for 3 keys with 10 timestamps each
	now := time.Now()
	keys := make([]string, 3)
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key%d", i)
		keys[i] = key
		for j := 0; j < 10; j++ {
			_ = tl.Append(ctx, key, now.Add(time.Duration(j)*time.Millisecond), map[string]string{
				"field1": fmt.Sprintf("value1_%d_%d", i, j),
				"field2": fmt.Sprintf("value2_%d_%d", i, j),
				"field3": fmt.Sprintf("value3_%d_%d", i, j),
			}, false)
		}
	}

	// Query range covering most timestamps
	start := now
	end := now.Add(9 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetRange(ctx, keys, start, end)
	}
}

// --- GetUpdatedKeys benchmarks ---

func BenchmarkRedisTimeline_GetUpdatedKeys_vs_KeysGetLatest(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: Add 100 keys with updates at different times
	now := time.Now()
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		_ = tl.Append(ctx, key, now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	queryAfter := now.Add(50 * time.Millisecond)

	b.Run("GetUpdatedKeys", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = tl.GetUpdatedKeys(ctx, queryAfter)
		}
	})

	b.Run("Keys+GetLatest", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			keys, _ := tl.Keys(ctx, model.FilterOptions{})
			// For each key, need to get latest timestamp to filter
			// This is the inefficient approach GetUpdatedKeys avoids
			for _, key := range keys {
				_, _ = tl.GetLatest(ctx, []string{key})
			}
		}
	})
}

func BenchmarkRedisTimeline_Append_WithGlobalIndex(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Measure Append overhead with global index maintenance
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tl.Append(ctx, "key1", time.Now(), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}
}

// --- Retention benchmarks ---

func BenchmarkRedisTimeline_AppendNoRetention(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_no_retention")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// No retention policy set
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tl.Append(ctx, "key1", time.Now().Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}
}

func BenchmarkRedisTimeline_AppendWithOptions_NoCleanup(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_retention_no_cleanup")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Set retention but within limits (no cleanup needed)
	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    1000,
		MaxDuration: 1 * time.Hour,
		Strategy:    model.RetentionMax,
	}})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tl.Append(ctx, "key1", time.Now().Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}
}

func BenchmarkRedisTimeline_AppendWithOptions_WithCleanup(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_retention_cleanup")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Set strict retention that triggers cleanup on every write
	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    10,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	}})

	// Pre-populate to ensure cleanup happens on each iteration
	base := time.Now()
	for i := 0; i < 15; i++ {
		_ = tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := base.Add(time.Duration(15+i) * time.Millisecond)
		_ = tl.Append(ctx, "key1", ts, map[string]string{
			"field": fmt.Sprintf("value%d", 15+i),
		}, false)
	}
}

func BenchmarkRedisTimeline_RetentionCleanup_SmallDataset(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_retention_small")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: 10 points with MaxCount=5
	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    5,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	}})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		_ = tl.Delete(ctx)
		base := time.Now()
		for j := 0; j < 10; j++ {
			_ = tl.Append(ctx, "key1", base.Add(time.Duration(j)*time.Millisecond), map[string]string{
				"field": fmt.Sprintf("value%d", j),
			}, false)
		}
		b.StartTimer()

		// This write triggers cleanup of 5 points
		_ = tl.Append(ctx, "key1", base.Add(11*time.Millisecond), map[string]string{
			"field": "value11",
		}, false)
	}
}

func BenchmarkRedisTimeline_RetentionCleanup_LargeDataset(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_retention_large")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: 1000 points with MaxCount=100
	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    100,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	}})

	base := time.Now()
	for i := 0; i < 1000; i++ {
		_ = tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := base.Add(time.Duration(1000+i) * time.Millisecond)
		_ = tl.Append(ctx, "key1", ts, map[string]string{
			"field": fmt.Sprintf("value%d", 1000+i),
		}, false)
	}
}

func BenchmarkRedisTimeline_RetentionStrategies(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)

	b.Run("RetentionMax", func(b *testing.B) {
		tl := cli.Timeline("bench_retention_max")
		defer func() {
			_ = tl.Delete(context.Background())
		}()
		ctx := context.Background()

		tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
			MaxCount:    10,
			MaxDuration: 100 * time.Millisecond,
			Strategy:    model.RetentionMax,
	}})

		base := time.Now()
		for i := 0; i < 20; i++ {
			_ = tl.Append(ctx, "key1", base.Add(time.Duration(i)*10*time.Millisecond), map[string]string{
				"field": fmt.Sprintf("value%d", i),
			}, false)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ts := base.Add(time.Duration(20+i) * 10 * time.Millisecond)
			_ = tl.Append(ctx, "key1", ts, map[string]string{
				"field": fmt.Sprintf("value%d", 20+i),
			}, false)
		}
	})

	b.Run("RetentionMin", func(b *testing.B) {
		tl := cli.Timeline("bench_retention_min")
		defer func() {
			_ = tl.Delete(context.Background())
		}()
		ctx := context.Background()

		tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
			MaxCount:    10,
			MaxDuration: 100 * time.Millisecond,
			Strategy:    model.RetentionMin,
	}})

		base := time.Now()
		for i := 0; i < 20; i++ {
			_ = tl.Append(ctx, "key1", base.Add(time.Duration(i)*10*time.Millisecond), map[string]string{
				"field": fmt.Sprintf("value%d", i),
			}, false)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ts := base.Add(time.Duration(20+i) * 10 * time.Millisecond)
			_ = tl.Append(ctx, "key1", ts, map[string]string{
				"field": fmt.Sprintf("value%d", 20+i),
			}, false)
		}
	})
}

// BenchmarkRedisTimeline_Timeline measures pipelined Timeline performance
func BenchmarkRedisTimeline_Timeline(b *testing.B) {
	cli := NewClient(getRedisAddr(), "", 0).(*client)
	tl := cli.Timeline("bench_timeline_timeline")
	defer func() {
		_ = tl.Delete(context.Background())
	}()
	ctx := context.Background()

	// Setup: Add 100 points with multiple fields to stress-test pipelining
	base := time.Now()
	for i := 0; i < 100; i++ {
		_ = tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field1": fmt.Sprintf("value%d", i),
			"field2": fmt.Sprintf("value%d", i),
			"field3": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.Timeline(ctx, "key1")
	}
}
