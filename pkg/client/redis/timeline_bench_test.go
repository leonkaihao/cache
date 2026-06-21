package redis

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkRedisTimeline_Append(b *testing.B) {
	b.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("bench_timeline")
	defer tl.Delete(context.Background())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tl.Append(ctx, "key1", time.Now(), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}
}

func BenchmarkRedisTimeline_GetAt(b *testing.B) {
	b.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("bench_timeline")
	defer tl.Delete(context.Background())
	ctx := context.Background()

	// Setup: Add 1000 points
	now := time.Now()
	for i := 0; i < 1000; i++ {
		tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tl.GetAt(ctx, []string{"key1"}, now.Add(500*time.Millisecond))
	}
}

func BenchmarkRedisTimeline_GetAt_MultiKey(b *testing.B) {
	b.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("bench_timeline_getat")
	defer tl.Delete(context.Background())
	ctx := context.Background()

	// Setup: Add data for 5 keys with 10 timestamps each
	now := time.Now()
	keys := make([]string, 5)
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		keys[i] = key
		for j := 0; j < 10; j++ {
			tl.Append(ctx, key, now.Add(time.Duration(j)*time.Millisecond), map[string]string{
				"field1": fmt.Sprintf("value1_%d_%d", i, j),
				"field2": fmt.Sprintf("value2_%d_%d", i, j),
				"field3": fmt.Sprintf("value3_%d_%d", i, j),
			}, false)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tl.GetAt(ctx, keys, now.Add(5*time.Millisecond))
	}
}

func BenchmarkRedisTimeline_GetExact(b *testing.B) {
	b.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("bench_timeline_exact")
	defer tl.Delete(context.Background())
	ctx := context.Background()

	// Setup: Add data for 10 keys at a specific timestamp
	ts := time.Now()
	keys := make([]string, 10)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		keys[i] = key
		tl.Append(ctx, key, ts, map[string]string{
			"field1": fmt.Sprintf("value1_%d", i),
			"field2": fmt.Sprintf("value2_%d", i),
			"field3": fmt.Sprintf("value3_%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tl.GetExact(ctx, keys, ts)
	}
}

func BenchmarkRedisTimeline_GetLatest(b *testing.B) {
	b.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("bench_timeline_latest")
	defer tl.Delete(context.Background())
	ctx := context.Background()

	// Setup: Add data for 5 keys with 10 timestamps each
	now := time.Now()
	keys := make([]string, 5)
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		keys[i] = key
		for j := 0; j < 10; j++ {
			tl.Append(ctx, key, now.Add(time.Duration(j)*time.Millisecond), map[string]string{
				"field1": fmt.Sprintf("value1_%d_%d", i, j),
				"field2": fmt.Sprintf("value2_%d_%d", i, j),
				"field3": fmt.Sprintf("value3_%d_%d", i, j),
			}, false)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tl.GetLatest(ctx, keys)
	}
}

func BenchmarkRedisTimeline_GetAffectedRange(b *testing.B) {
	b.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("bench_timeline_affected")
	defer tl.Delete(context.Background())
	ctx := context.Background()

	// Setup: Add 20 timestamps to a single key
	now := time.Now()
	key := "key1"
	for i := 0; i < 20; i++ {
		tl.Append(ctx, key, now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field1": fmt.Sprintf("value1_%d", i),
			"field2": fmt.Sprintf("value2_%d", i),
			"field3": fmt.Sprintf("value3_%d", i),
		}, false)
	}

	// Query affected range from middle of timeline
	insertedAt := now.Add(10 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tl.GetAffectedRange(ctx, key, insertedAt)
	}
}

func BenchmarkRedisTimeline_GetRange(b *testing.B) {
	b.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("bench_timeline_range")
	defer tl.Delete(context.Background())
	ctx := context.Background()

	// Setup: Add data for 3 keys with 10 timestamps each
	now := time.Now()
	keys := make([]string, 3)
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key%d", i)
		keys[i] = key
		for j := 0; j < 10; j++ {
			tl.Append(ctx, key, now.Add(time.Duration(j)*time.Millisecond), map[string]string{
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
		tl.GetRange(ctx, keys, start, end)
	}
}
