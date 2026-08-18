package mem

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
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
		_, _ = tl.GetAt(ctx, []string{"key1"}, now.Add(500*time.Millisecond), model.QueryOptions{})
	}
}

func BenchmarkTimeline_GetAt_LargeTimeline(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline_large")
	ctx := context.Background()

	// Setup: Add 10K points to stress-test skiplist lookups
	now := time.Now()
	for i := 0; i < 10000; i++ {
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetAt(ctx, []string{"key1"}, now.Add(5000*time.Millisecond), model.QueryOptions{})
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
		_, _ = tl.GetRange(ctx, []string{"key1"}, now, now.Add(100*time.Millisecond), model.QueryOptions{})
	}
}

func BenchmarkTimeline_GetRange_MultipleFields(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline_fields")
	ctx := context.Background()

	// Setup: Add 1000 points with 10 fields each
	now := time.Now()
	for i := 0; i < 1000; i++ {
		data := make(map[string]string)
		for j := 0; j < 10; j++ {
			data[fmt.Sprintf("field%d", j)] = fmt.Sprintf("value%d_%d", i, j)
		}
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), data, false)
	}

	// Query a 100-point range
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetRange(ctx, []string{"key1"}, now, now.Add(100*time.Millisecond), model.QueryOptions{})
	}
}

// BenchmarkTimeline_GetLatest measures performance of GetLatest calls
func BenchmarkTimeline_GetLatest(b *testing.B) {
	cli := NewClient().(*client)
	tl := cli.Timeline("bench_timeline_latest")
	ctx := context.Background()

	// Setup: Add 100 points with multiple fields
	now := time.Now()
	for i := 0; i < 100; i++ {
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field1": fmt.Sprintf("value%d", i),
			"field2": fmt.Sprintf("value%d", i),
			"field3": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
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

// BenchmarkTimeline_KeysWithAfterTs benchmarks time-based key filtering
func BenchmarkTimeline_KeysWithAfterTs_100Keys(b *testing.B) {
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
		_, _ = tl.Keys(ctx, model.FilterOptions{AfterTs: &queryAfter})
	}
}

func BenchmarkTimeline_KeysWithAfterTs_1KKeys(b *testing.B) {
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
		_, _ = tl.Keys(ctx, model.FilterOptions{AfterTs: &queryAfter})
	}
}

func BenchmarkTimeline_KeysWithAfterTs_10KKeys(b *testing.B) {
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
		_, _ = tl.Keys(ctx, model.FilterOptions{AfterTs: &queryAfter})
	}
}
