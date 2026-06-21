package redis

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: These tests require a running Redis instance
// Run with: go test -tags=redis

func TestRedisTimeline_BasicOperations(t *testing.T) {
	t.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer tl.Delete(context.Background())

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
	t.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer tl.Delete(context.Background())

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
	t.Skip("Requires Redis instance and retention implementation")

	// Placeholder for retention tests
}

// --- 8.5: Redis timeline label operations ---

func TestRedisTimeline_Labels(t *testing.T) {
	t.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer tl.Delete(context.Background())

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
	t.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer tl.Delete(context.Background())

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
	t.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer tl.Delete(context.Background())

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
	t.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer tl.Delete(context.Background())

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
	t.Skip("Requires Redis instance")

	cli := NewClient("localhost:6379", "", 0).(*client)
	tl := cli.Timeline("test_timeline")
	defer tl.Delete(context.Background())

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
