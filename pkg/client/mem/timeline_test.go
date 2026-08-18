package mem

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTimeline_SkiplistBasicOperations tests skiplist-specific behavior
func TestTimeline_SkiplistBasicOperations(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline").(*memTimeline)

	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	// Test that skiplist is used for field storage
	err := tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value1",
		"field2": "value2",
	}, false)
	require.NoError(t, err)

	// Verify internal structure uses skiplist
	tl.mu.RLock()
	td := tl.data["key1"]
	require.NotNil(t, td)
	require.NotNil(t, td.fields["field1"])
	require.NotNil(t, td.fields["field1"].points)
	assert.Equal(t, 1, td.fields["field1"].points.Len())
	tl.mu.RUnlock()

	// Verify queries work
	results, err := tl.GetAt(ctx, []string{"key1"}, now, model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, results[0])
	assert.Equal(t, "value1", results[0]["field1"].Value)
	assert.Equal(t, "value2", results[0]["field2"].Value)
}

// TestTimeline_SkiplistOutOfOrderInserts tests skiplist's out-of-order insert performance
func TestTimeline_SkiplistOutOfOrderInserts(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline").(*memTimeline)

	ctx := context.Background()
	base := time.Now().Truncate(time.Microsecond)

	// Insert in reverse order
	for i := 100; i >= 0; i-- {
		err := tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Second), map[string]string{
			"value": fmt.Sprintf("%d", i),
		}, false)
		require.NoError(t, err)
	}

	// Verify all 101 points are stored
	tl.mu.RLock()
	td := tl.data["key1"]
	assert.Equal(t, 101, td.fields["value"].points.Len())
	tl.mu.RUnlock()

	// Verify correct order
	timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
	require.NoError(t, err)
	require.Contains(t, timeline, "value")
	assert.Len(t, timeline["value"], 101)
	assert.Equal(t, "0", timeline["value"][0].Value)
	assert.Equal(t, "100", timeline["value"][100].Value)
}

// TestTimeline_PerFieldRetentionInternal tests internal retention logic per field
func TestTimeline_PerFieldRetentionInternal(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline").(*memTimeline)

	ctx := context.Background()
	base := time.Now().Truncate(time.Microsecond)

	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    3,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	}})

	// Field 'a' gets 5 updates
	for i := 0; i < 5; i++ {
		err := tl.Append(ctx, "key1", base.Add(time.Duration(i)*time.Second), map[string]string{
			"a": fmt.Sprintf("a%d", i),
		}, false)
		require.NoError(t, err)
	}

	// Field 'b' gets only 2 updates
	err := tl.Append(ctx, "key1", base, map[string]string{"b": "b0"}, false)
	require.NoError(t, err)
	err = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"b": "b1"}, false)
	require.NoError(t, err)

	// Verify field 'a' has only 3 points (MaxCount=3)
	tl.mu.RLock()
	td := tl.data["key1"]
	assert.Equal(t, 3, td.fields["a"].points.Len(), "field 'a' should have 3 points")
	assert.Equal(t, 2, td.fields["b"].points.Len(), "field 'b' should have 2 points (not truncated)")
	tl.mu.RUnlock()

	// Verify values are correct
	latest, err := tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, "a4", latest[0]["a"].Value)
	assert.Equal(t, "b1", latest[0]["b"].Value)
}

// TestTimeline_FieldIndependence tests that fields are truly independent
func TestTimeline_FieldIndependence(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline").(*memTimeline)

	ctx := context.Background()
	base := time.Now().Truncate(time.Microsecond)

	// Update field1 at t1, t2, t3
	_ = tl.Append(ctx, "key1", base, map[string]string{"field1": "v1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"field1": "v2"}, false)
	_ = tl.Append(ctx, "key1", base.Add(2*time.Second), map[string]string{"field1": "v3"}, false)

	// Update field2 only at t1
	_ = tl.Append(ctx, "key1", base, map[string]string{"field2": "w1"}, false)

	// Verify internal structure
	tl.mu.RLock()
	td := tl.data["key1"]
	assert.Equal(t, 3, td.fields["field1"].points.Len())
	assert.Equal(t, 1, td.fields["field2"].points.Len())
	tl.mu.RUnlock()

	// Verify Timeline shows independent series
	timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, timeline["field1"], 3)
	assert.Len(t, timeline["field2"], 1)
}

// TestTimeline_LabelOperations tests label functionality (implementation-specific)
func TestTimeline_LabelOperations(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline").(*memTimeline)

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, tl.Append(ctx, "k1", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k2", now, map[string]string{"f": "v"}, false))
	require.NoError(t, tl.Append(ctx, "k3", now, map[string]string{"f": "v"}, false))

	require.NoError(t, tl.AddKeyLabels(ctx, "k1", []string{"alpha", "beta"}))
	require.NoError(t, tl.AddKeyLabels(ctx, "k2", []string{"alpha", "gamma"}))
	require.NoError(t, tl.AddKeyLabels(ctx, "k3", []string{"beta"}))

	// Verify internal label indexes
	tl.mu.RLock()
	assert.True(t, tl.keyLabels["k1"]["alpha"])
	assert.True(t, tl.keyLabels["k1"]["beta"])
	assert.Equal(t, 2, len(tl.labelIndex["alpha"])) // k1, k2
	assert.Equal(t, 2, len(tl.labelIndex["beta"]))  // k1, k3
	tl.mu.RUnlock()

	// Test label filtering
	keys, err := tl.Keys(ctx, model.FilterOptions{
		LabelFilter: [][]string{{"alpha"}},
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2"}, keys)

	// Test label removal
	require.NoError(t, tl.RemoveKeyLabels(ctx, "k1", []string{"alpha"}))
	
	tl.mu.RLock()
	assert.False(t, tl.keyLabels["k1"]["alpha"])
	assert.Equal(t, 1, len(tl.labelIndex["alpha"])) // only k2 now
	tl.mu.RUnlock()
}

// TestTimeline_GlobalTSIndex tests the globalTS index maintenance
func TestTimeline_GlobalTSIndex(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline").(*memTimeline)

	ctx := context.Background()
	t1 := time.Now().Truncate(time.Microsecond)
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)

	// Add updates at different times
	_ = tl.Append(ctx, "k1", t1, map[string]string{"f": "v1"}, false)
	_ = tl.Append(ctx, "k2", t2, map[string]string{"f": "v2"}, false)
	_ = tl.Append(ctx, "k3", t3, map[string]string{"f": "v3"}, false)

	// Verify globalTS index
	tl.mu.RLock()
	assert.Equal(t, normalizeTimestamp(t1), tl.globalTS["k1"])
	assert.Equal(t, normalizeTimestamp(t2), tl.globalTS["k2"])
	assert.Equal(t, normalizeTimestamp(t3), tl.globalTS["k3"])
	tl.mu.RUnlock()

	// Test Keys with AfterTs
	keys, err := tl.Keys(ctx, model.FilterOptions{AfterTs: &t1})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k2", "k3"}, keys)

	// Update k1 with a newer timestamp
	t4 := t3.Add(time.Second)
	_ = tl.Append(ctx, "k1", t4, map[string]string{"f": "v4"}, false)

	// globalTS should be updated
	tl.mu.RLock()
	assert.Equal(t, normalizeTimestamp(t4), tl.globalTS["k1"])
	tl.mu.RUnlock()

	keys, err = tl.Keys(ctx, model.FilterOptions{AfterTs: &t3})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1"}, keys)
}

// TestTimeline_OutOfOrderGlobalTS tests that out-of-order inserts maintain correct globalTS
func TestTimeline_OutOfOrderGlobalTS(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline").(*memTimeline)

	ctx := context.Background()
	t1 := time.Now().Truncate(time.Microsecond)
	t2 := t1.Add(2 * time.Second)
	t3 := t1.Add(4 * time.Second)

	// Insert in order: t3, t1, t2
	_ = tl.Append(ctx, "k1", t3, map[string]string{"f": "v3"}, false)
	_ = tl.Append(ctx, "k1", t1, map[string]string{"f": "v1"}, false)
	_ = tl.Append(ctx, "k1", t2, map[string]string{"f": "v2"}, false)

	// globalTS should be t3 (the maximum)
	tl.mu.RLock()
	assert.Equal(t, normalizeTimestamp(t3), tl.globalTS["k1"])
	tl.mu.RUnlock()

	// Query after t2.5 should include k1
	t25 := t2.Add(500 * time.Millisecond)
	keys, err := tl.Keys(ctx, model.FilterOptions{AfterTs: &t25})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1"}, keys)
}

// TestTimeline_ContextCancellation tests context handling (implementation-specific)
func TestTimeline_ContextCancellation(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tl.Append(ctx, "key1", time.Now(), map[string]string{"f": "v"}, false)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	_, err = tl.GetAt(ctx, []string{"key1"}, time.Now(), model.QueryOptions{})
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestTimeline_EmptyQueryResults tests nil handling
func TestTimeline_EmptyQueryResults(t *testing.T) {
	cli := NewClient().(*client)
	tl := cli.Timeline("test_timeline")

	ctx := context.Background()

	// Query non-existent key
	results, err := tl.GetAt(ctx, []string{"nonexistent"}, time.Now(), model.QueryOptions{})
	assert.NoError(t, err)
	assert.Nil(t, results[0])

	latest, err := tl.GetLatest(ctx, []string{"nonexistent"}, model.QueryOptions{})
	assert.NoError(t, err)
	assert.Nil(t, latest[0])

	rangeResults, err := tl.GetRange(ctx, []string{"nonexistent"}, time.Now(), time.Now().Add(time.Hour), model.QueryOptions{})
	assert.NoError(t, err)
	assert.Nil(t, rangeResults[0])
}
