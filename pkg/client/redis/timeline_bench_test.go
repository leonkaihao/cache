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
