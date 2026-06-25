//go:build integration
// +build integration

package redis

import (
	"context"
	"fmt"
	"testing"
	"time"
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
			keys, _ := tl.Keys(ctx)
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

