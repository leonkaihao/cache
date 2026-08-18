package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
)

// BenchmarkRedisTimeline_Append measures write performance
func BenchmarkRedisTimeline_Append(b *testing.B) {
	cli := NewClient("localhost:6379", "", 0)
	tl := cli.Timeline("bench_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tl.Append(ctx, "key1", time.Now(), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}
}

func BenchmarkRedisTimeline_GetAt(b *testing.B) {
	cli := NewClient("localhost:6379", "", 0)
	tl := cli.Timeline("bench_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Setup: Add 100 points
	for i := 0; i < 100; i++ {
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetAt(ctx, []string{"key1"}, now.Add(50*time.Millisecond), model.QueryOptions{})
	}
}

func BenchmarkRedisTimeline_GetAt_MultiKey(b *testing.B) {
	cli := NewClient("localhost:6379", "", 0)
	tl := cli.Timeline("bench_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Setup: Add 10 keys with 10 points each
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		for j := 0; j < 10; j++ {
			_ = tl.Append(ctx, key, now.Add(time.Duration(j)*time.Millisecond), map[string]string{
				"field": fmt.Sprintf("value%d", j),
			}, false)
		}
	}

	keys := make([]string, 10)
	for i := 0; i < 10; i++ {
		keys[i] = fmt.Sprintf("key%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetAt(ctx, keys, now.Add(5*time.Millisecond), model.QueryOptions{})
	}
}

func BenchmarkRedisTimeline_GetLatest(b *testing.B) {
	cli := NewClient("localhost:6379", "", 0)
	tl := cli.Timeline("bench_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Setup: Add 10 keys with 10 points each and 5 fields
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		for j := 0; j < 10; j++ {
			data := make(map[string]string)
			for f := 0; f < 5; f++ {
				data[fmt.Sprintf("field%d", f)] = fmt.Sprintf("value%d_%d", j, f)
			}
			_ = tl.Append(ctx, key, now.Add(time.Duration(j)*time.Millisecond), data, false)
		}
	}

	keys := make([]string, 10)
	for i := 0; i < 10; i++ {
		keys[i] = fmt.Sprintf("key%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetLatest(ctx, keys, model.QueryOptions{})
	}
}

func BenchmarkRedisTimeline_GetRange(b *testing.B) {
	cli := NewClient("localhost:6379", "", 0)
	tl := cli.Timeline("bench_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Setup: Add 100 points
	for i := 0; i < 100; i++ {
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), map[string]string{
			"field": fmt.Sprintf("value%d", i),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.GetRange(ctx, []string{"key1"}, now, now.Add(50*time.Millisecond), model.QueryOptions{})
	}
}

func BenchmarkRedisTimeline_Timeline(b *testing.B) {
	cli := NewClient("localhost:6379", "", 0)
	tl := cli.Timeline("bench_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Setup: Add 50 points with 3 fields
	for i := 0; i < 50; i++ {
		data := make(map[string]string)
		for j := 0; j < 3; j++ {
			data[fmt.Sprintf("field%d", j)] = fmt.Sprintf("value%d_%d", i, j)
		}
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), data, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tl.Timeline(ctx, "key1", model.QueryOptions{})
	}
}

// BenchmarkRedisTimeline_KeysWithAfterTs benchmarks time-based key filtering using globalTS ZSET
func BenchmarkRedisTimeline_KeysWithAfterTs(b *testing.B) {
	cli := NewClient("localhost:6379", "", 0)
	tl := cli.Timeline("bench_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Setup: Add 100 keys
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

// BenchmarkRedisTimeline_FieldLevelOperations benchmarks per-field operations
func BenchmarkRedisTimeline_FieldLevelOperations(b *testing.B) {
	b.Run("SingleField", func(b *testing.B) {
		cli := NewClient("localhost:6379", "", 0)
		tl := cli.Timeline("bench_timeline")
		defer func() { _ = tl.Delete(context.Background()) }()

		ctx := context.Background()
		now := time.Now()

		// Setup: 100 points, 1 field
		for i := 0; i < 100; i++ {
			_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), map[string]string{
				"field1": fmt.Sprintf("v%d", i),
			}, false)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
		}
	})

	b.Run("TenFields", func(b *testing.B) {
		cli := NewClient("localhost:6379", "", 0)
		tl := cli.Timeline("bench_timeline")
		defer func() { _ = tl.Delete(context.Background()) }()

		ctx := context.Background()
		now := time.Now()

		// Setup: 100 points, 10 fields
		for i := 0; i < 100; i++ {
			data := make(map[string]string)
			for j := 0; j < 10; j++ {
				data[fmt.Sprintf("field%d", j)] = fmt.Sprintf("v%d", i)
			}
			_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), data, false)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
		}
	})

	b.Run("TwentyFields", func(b *testing.B) {
		cli := NewClient("localhost:6379", "", 0)
		tl := cli.Timeline("bench_timeline")
		defer func() { _ = tl.Delete(context.Background()) }()

		ctx := context.Background()
		now := time.Now()

		// Setup: 100 points, 20 fields
		for i := 0; i < 100; i++ {
			data := make(map[string]string)
			for j := 0; j < 20; j++ {
				data[fmt.Sprintf("field%d", j)] = fmt.Sprintf("v%d", i)
			}
			_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), data, false)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
		}
	})
}

// BenchmarkRedisTimeline_FieldFiltering benchmarks QueryOptions field filtering
func BenchmarkRedisTimeline_FieldFiltering(b *testing.B) {
	cli := NewClient("localhost:6379", "", 0)
	tl := cli.Timeline("bench_timeline")
	defer func() { _ = tl.Delete(context.Background()) }()

	ctx := context.Background()
	now := time.Now()

	// Setup: 100 points with 10 fields
	for i := 0; i < 100; i++ {
		data := make(map[string]string)
		for j := 0; j < 10; j++ {
			data[fmt.Sprintf("field%d", j)] = fmt.Sprintf("v%d", i)
		}
		_ = tl.Append(ctx, "key1", now.Add(time.Duration(i)*time.Millisecond), data, false)
	}

	b.Run("AllFields", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
		}
	})

	b.Run("OneField", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{
				Fields: []string{"field0"},
			})
		}
	})

	b.Run("ThreeFields", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{
				Fields: []string{"field0", "field1", "field2"},
			})
		}
	})
}
