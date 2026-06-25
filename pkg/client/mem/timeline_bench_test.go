package mem

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkTimeline_Append(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tl.Append(ctx, "key1", time.Now(), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}
}

func BenchmarkTimeline_GetAt(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline")
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

func BenchmarkTimeline_GetRange(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline")
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
		_, _ = tl.GetRange(ctx, []string{"key1"}, now, now.Add(100*time.Millisecond))
	}
}

func BenchmarkTimeline_SparseUpdates(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline")
	ctx := context.Background()
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i/10)*time.Second), map[string]string{
			fmt.Sprintf("field%d", i%10): "value",
		}, false)
	}
}

// --- GetUpdatedKeys benchmarks ---

func BenchmarkTimeline_GetUpdatedKeys_100Keys(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline")
	ctx := context.Background()

	// Setup: Add 100 keys
	now := time.Now()
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		_ = tl.Append(ctx, key, now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	queryAfter := now.Add(50 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetUpdatedKeys(ctx, queryAfter)
	}
}

func BenchmarkTimeline_GetUpdatedKeys_1KKeys(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline")
	ctx := context.Background()

	// Setup: Add 1000 keys
	now := time.Now()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key%d", i)
		_ = tl.Append(ctx, key, now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	queryAfter := now.Add(500 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetUpdatedKeys(ctx, queryAfter)
	}
}

func BenchmarkTimeline_GetUpdatedKeys_10KKeys(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline")
	ctx := context.Background()

	// Setup: Add 10000 keys
	now := time.Now()
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key%d", i)
		_ = tl.Append(ctx, key, now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	queryAfter := now.Add(5000 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetUpdatedKeys(ctx, queryAfter)
	}
}

