package mem

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leonkaihao/cache/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeline_BasicOperations(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()
	now := time.Now()

	// Test Append
	err := tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value1",
		"field2": "value2",
	}, false)
	require.NoError(t, err)

	// Test GetAt
	state, err := tl.GetAt(ctx, "key1", now)
	require.NoError(t, err)
	assert.Equal(t, "value1", state["field1"])
	assert.Equal(t, "value2", state["field2"])
}

func TestTimeline_SparseFieldUpdates(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	// First update
	err := tl.Append(ctx, "key1", t1, map[string]string{
		"field1": "value1",
		"field2": "value2",
	}, false)
	require.NoError(t, err)

	// Second update (sparse - only field1)
	err = tl.Append(ctx, "key1", t2, map[string]string{
		"field1": "updated",
	}, false)
	require.NoError(t, err)

	// Get merged state
	state, err := tl.GetAt(ctx, "key1", t2)
	require.NoError(t, err)
	assert.Equal(t, "updated", state["field1"])
	assert.Equal(t, "value2", state["field2"]) // field2 retained from t1
}

func TestTimeline_FieldConflict(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()
	now := time.Now()

	// First write
	err := tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value1",
	}, false)
	require.NoError(t, err)

	// Second write with force=false (should fail)
	err = tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value2",
	}, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// Third write with force=true (should succeed)
	err = tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value2",
	}, true)
	require.NoError(t, err)

	state, err := tl.GetExact(ctx, "key1", now)
	require.NoError(t, err)
	assert.Equal(t, "value2", state["field1"])
}

func TestTimeline_TimestampNormalization(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()
	
	// Create time with nanosecond precision
	ts := time.Date(2024, 6, 13, 10, 0, 0, 123456789, time.UTC)

	err := tl.Append(ctx, "key1", ts, map[string]string{
		"field1": "value1",
	}, false)
	require.NoError(t, err)

	// Query with same time (nanoseconds should be truncated)
	state, err := tl.GetExact(ctx, "key1", ts)
	require.NoError(t, err)
	assert.Equal(t, "value1", state["field1"])
}

func TestTimeline_EdgeCases(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()

	// Empty key
	err := tl.Append(ctx, "", time.Now(), map[string]string{"f": "v"}, false)
	assert.Error(t, err)

	// Empty data (no-op)
	err = tl.Append(ctx, "key1", time.Now(), map[string]string{}, false)
	assert.NoError(t, err)

	// Nil data (no-op)
	err = tl.Append(ctx, "key1", time.Now(), nil, false)
	assert.NoError(t, err)

	// Key not found
	_, err = tl.GetAt(ctx, "nonexistent", time.Now())
	assert.Error(t, err)
}

func TestTimeline_RetentionPolicy(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()

	// Set retention policy
	err := tl.SetRetention(model.RetentionPolicy{
		MaxCount:    2,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	})
	require.NoError(t, err)

	// Add 3 points
	t1 := time.Now()
	tl.Append(ctx, "key1", t1, map[string]string{"v": "1"}, false)
	tl.Append(ctx, "key1", t1.Add(time.Second), map[string]string{"v": "2"}, false)
	tl.Append(ctx, "key1", t1.Add(2*time.Second), map[string]string{"v": "3"}, false)

	// Should only have 2 points (last 2)
	timeline, err := tl.Timeline(ctx, "key1")
	require.NoError(t, err)
	assert.Len(t, timeline, 2)
}

func TestTimeline_Concurrency(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()
	now := time.Now()

	// Concurrent writes with different fields
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			tl.Append(ctx, "key1", now, map[string]string{
				fmt.Sprintf("field%d", n): "value",
			}, false)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// All fields should be present
	state, err := tl.GetExact(ctx, "key1", now)
	require.NoError(t, err)
	assert.Len(t, state, 10)
}

func TestTimeline_ContextCancellation(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := tl.Append(ctx, "key1", time.Now(), map[string]string{"f": "v"}, false)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestTimeline_ManagementOperations(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()
	now := time.Now()

	// Add data
	tl.Append(ctx, "key1", now, map[string]string{"f": "v"}, false)
	tl.Append(ctx, "key2", now, map[string]string{"f": "v"}, false)

	// Test Keys()
	keys, err := tl.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 2)

	// Test Remove()
	err = tl.Remove(ctx, []string{"key1"})
	require.NoError(t, err)

	keys, err = tl.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 1)

	// Test Clear()
	err = tl.Clear(ctx)
	require.NoError(t, err)

	keys, err = tl.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 0)
}
