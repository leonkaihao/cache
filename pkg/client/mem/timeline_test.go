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
	results, err := tl.GetAt(ctx, []string{"key1"}, now)
	require.NoError(t, err)
	require.Len(t, results, 1)
	state := results[0]
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
	results, err := tl.GetAt(ctx, []string{"key1"}, t2)
	require.NoError(t, err)
	require.Len(t, results, 1)
	state := results[0]
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

	state, err := tl.GetExact(ctx, []string{"key1"}, now)
	require.NoError(t, err)
	require.Len(t, state, 1)
	assert.Equal(t, "value2", state[0]["field1"])
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
	results, err := tl.GetExact(ctx, []string{"key1"}, ts)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "value1", results[0]["field1"])
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

	// Key not found → nil result at index 0, no error
	results, err := tl.GetAt(ctx, []string{"nonexistent"}, time.Now())
	assert.NoError(t, err)
	assert.Nil(t, results[0])
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
	_ = tl.Append(ctx, "key1", t1, map[string]string{"v": "1"}, false)
	_ = tl.Append(ctx, "key1", t1.Add(time.Second), map[string]string{"v": "2"}, false)
	_ = tl.Append(ctx, "key1", t1.Add(2*time.Second), map[string]string{"v": "3"}, false)

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
			_ = tl.Append(ctx, "key1", now, map[string]string{
				fmt.Sprintf("field%d", n): "value",
			}, false)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// All fields should be present
	results, err := tl.GetExact(ctx, []string{"key1"}, now)
	require.NoError(t, err)
	assert.Len(t, results[0], 10)
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
	_ = tl.Append(ctx, "key1", now, map[string]string{"f": "v"}, false)
	_ = tl.Append(ctx, "key2", now, map[string]string{"f": "v"}, false)

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

// --- 8.1: AddKeyLabels / RemoveKeyLabels / KeyLabels ---

func TestTimeline_Labels(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, tl.Append(ctx, "key1", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "key2", now, map[string]string{"f": "v"}, false))

	// Add labels to key1
	require.NoError(t, tl.AddKeyLabels(ctx, "key1", []string{"alpha", "beta"}))

	ls, err := tl.KeyLabels(ctx, "key1")
	require.NoError(t, err)
	assert.True(t, ls["alpha"])
	assert.True(t, ls["beta"])
	assert.False(t, ls["gamma"])

	// key2 has no labels → empty LabelSet
	ls2, err := tl.KeyLabels(ctx, "key2")
	require.NoError(t, err)
	assert.Empty(t, ls2)

	// Add a duplicate label — no-op, no error
	require.NoError(t, tl.AddKeyLabels(ctx, "key1", []string{"alpha"}))
	ls, _ = tl.KeyLabels(ctx, "key1")
	assert.Len(t, ls, 2) // still 2

	// Empty string labels are ignored
	require.NoError(t, tl.AddKeyLabels(ctx, "key1", []string{"", "gamma"}))
	ls, _ = tl.KeyLabels(ctx, "key1")
	assert.True(t, ls["gamma"])
	assert.False(t, ls[""])

	// Remove one label
	require.NoError(t, tl.RemoveKeyLabels(ctx, "key1", []string{"beta"}))
	ls, _ = tl.KeyLabels(ctx, "key1")
	assert.False(t, ls["beta"])
	assert.True(t, ls["alpha"])

	// Remove non-existent label — no-op
	require.NoError(t, tl.RemoveKeyLabels(ctx, "key1", []string{"nosuchlabel"}))
}

// --- 8.2: Keys with label filters ---

func TestTimeline_KeysWithLabelFilter(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")
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

	// Single label filter — OR within step
	keys, err = tl.Keys(ctx, []string{"foo"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k3"}, keys)

	// OR within one step: "bar" OR "bee" → k2, k3
	keys, err = tl.Keys(ctx, []string{"bar", "bee"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k2", "k3"}, keys)

	// AND across two steps: ("foo" OR "bar") AND "new" → k1, k2
	keys, err = tl.Keys(ctx, []string{"foo", "bar"}, []string{"new"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2"}, keys)

	// AND with no intersection: "bee" AND "new" → empty
	keys, err = tl.Keys(ctx, []string{"bee"}, []string{"new"})
	require.NoError(t, err)
	assert.Empty(t, keys)

	// Unknown label → empty
	keys, err = tl.Keys(ctx, []string{"nosuchlabel"})
	require.NoError(t, err)
	assert.Empty(t, keys)
}

// --- 8.3: Label cleanup on Remove and Clear ---

func TestTimeline_LabelCleanupOnRemove(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k2", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.AddKeyLabels(ctx, "k1", []string{"alpha"}))
	require.NoError(t, tl.AddKeyLabels(ctx, "k2", []string{"alpha", "beta"}))

	// Remove k1 — inverted index for "alpha" must no longer contain k1
	require.NoError(t, tl.Remove(ctx, []string{"k1"}))

	// Keys(ctx, []string{"alpha"}) should return only k2
	keys, err := tl.Keys(ctx, []string{"alpha"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k2"}, keys)

	// k1 labels gone
	ls, err := tl.KeyLabels(ctx, "k1")
	require.NoError(t, err)
	assert.Empty(t, ls)
}

func TestTimeline_LabelCleanupOnClear(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.AddKeyLabels(ctx, "k1", []string{"alpha"}))

	require.NoError(t, tl.Clear(ctx))

	// After clear, Keys returns empty
	keys, err := tl.Keys(ctx)
	require.NoError(t, err)
	assert.Empty(t, keys)

	// Label filter also returns empty
	keys, err = tl.Keys(ctx, []string{"alpha"})
	require.NoError(t, err)
	assert.Empty(t, keys)
}

// --- 8.4: Batch GetLatest / GetAt / GetExact / GetRange ---

func TestTimeline_BatchGetLatest(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")
	ctx := context.Background()
	t1 := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", t1, map[string]string{"a": "1"}, false))
	require.NoError(t, tl.Append(ctx, "k1", t1.Add(time.Second), map[string]string{"b": "2"}, false))
	require.NoError(t, tl.Append(ctx, "k2", t1, map[string]string{"c": "3"}, false))

	results, err := tl.GetLatest(ctx, []string{"k1", "k2", "missing"})
	require.NoError(t, err)
	require.Len(t, results, 3)

	// k1: merged a+b
	assert.Equal(t, "1", results[0]["a"])
	assert.Equal(t, "2", results[0]["b"])
	// k2: only c
	assert.Equal(t, "3", results[1]["c"])
	// missing key → nil
	assert.Nil(t, results[2])
}

func TestTimeline_BatchGetAt(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")
	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	require.NoError(t, tl.Append(ctx, "k1", t1, map[string]string{"a": "old"}, false))
	require.NoError(t, tl.Append(ctx, "k1", t2, map[string]string{"a": "new"}, true))
	require.NoError(t, tl.Append(ctx, "k2", t2, map[string]string{"b": "v"}, false))

	// Query at t1: k1 has old value, k2 has no data yet, missing is nil
	results, err := tl.GetAt(ctx, []string{"k1", "k2", "missing"}, t1)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "old", results[0]["a"])
	assert.Nil(t, results[1]) // k2 has no point at or before t1
	assert.Nil(t, results[2]) // missing key
}

func TestTimeline_BatchGetExact(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")
	ctx := context.Background()
	ts := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", ts, map[string]string{"x": "1"}, false))

	results, err := tl.GetExact(ctx, []string{"k1", "missing"}, ts)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "1", results[0]["x"])
	assert.Nil(t, results[1])
}

func TestTimeline_BatchGetRange(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")
	ctx := context.Background()
	t0 := time.Now()

	for i := 0; i < 5; i++ {
		require.NoError(t, tl.Append(ctx, "k1", t0.Add(time.Duration(i)*time.Second), map[string]string{
			"n": fmt.Sprintf("%d", i),
		}, false))
	}
	require.NoError(t, tl.Append(ctx, "k2", t0, map[string]string{"z": "0"}, false))

	start := t0.Add(time.Second)
	end := t0.Add(3 * time.Second)

	results, err := tl.GetRange(ctx, []string{"k1", "k2", "missing"}, start, end)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// k1: 3 points in range (t+1s, t+2s, t+3s), each with merged state
	require.Len(t, results[0], 3)
	// earliest in-range point at t+1s has merged n=1 (from points 0 and 1)
	assert.Equal(t, "1", results[0][0].Value["n"])

	// k2 has only t0, which is before start → nil slice
	assert.Nil(t, results[1])

	// missing key → nil
	assert.Nil(t, results[2])
}
