package test

import (
	"context"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TimelineTestSuite defines behavioral tests for CacheTimeline implementations.
type TimelineTestSuite struct {
	CreateTimeline func(name string) model.CacheTimeline
	Cleanup        func()
}

// RunAllTests runs all timeline behavioral tests.
func (suite *TimelineTestSuite) RunAllTests(t *testing.T) {
	t.Run("AppendAndGetAt", suite.TestAppendAndGetAt)
	t.Run("SparseFieldUpdates", suite.TestSparseFieldUpdates)
	t.Run("FieldConflicts", suite.TestFieldConflicts)
	t.Run("TimestampNormalization", suite.TestTimestampNormalization)
	t.Run("GetRange", suite.TestGetRange)
	t.Run("GetLatest", suite.TestGetLatest)
	t.Run("Timeline", suite.TestTimeline)
	t.Run("GetAffectedRange", suite.TestGetAffectedRange)
	t.Run("ManagementOperations", suite.TestManagementOperations)
	t.Run("RetentionPolicy", suite.TestRetentionPolicy)
	t.Run("FieldLevelRetention", suite.TestFieldLevelRetention)
	t.Run("PerFieldTimestamps", suite.TestPerFieldTimestamps)
	t.Run("QueryOptionsFiltering", suite.TestQueryOptionsFiltering)
	t.Run("KeysWithAfterTs", suite.TestKeysWithAfterTs)
	t.Run("TimelineFieldGrouping", suite.TestTimelineFieldGrouping)
	t.Run("ContextCancellation", suite.TestContextCancellation)
}

func (suite *TimelineTestSuite) TestAppendAndGetAt(t *testing.T) {
	tl := suite.CreateTimeline("test_append")
	defer suite.Cleanup()

	ctx := context.Background()
	now := time.Now()

	err := tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value1",
	}, false)
	require.NoError(t, err)

	state, err := tl.GetAt(ctx, []string{"key1"}, now, model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, state[0])
	assert.Equal(t, "value1", state[0]["field1"].Value)
}

func (suite *TimelineTestSuite) TestSparseFieldUpdates(t *testing.T) {
	tl := suite.CreateTimeline("test_sparse")
	defer suite.Cleanup()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	_ = tl.Append(ctx, "key1", t1, map[string]string{
		"field1": "value1",
		"field2": "value2",
	}, false)

	_ = tl.Append(ctx, "key1", t2, map[string]string{
		"field1": "updated",
	}, false)

	results, err := tl.GetAt(ctx, []string{"key1"}, t2, model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, results[0])
	assert.Equal(t, "updated", results[0]["field1"].Value)
	assert.Equal(t, "value2", results[0]["field2"].Value)
}

func (suite *TimelineTestSuite) TestFieldConflicts(t *testing.T) {
	tl := suite.CreateTimeline("test_conflicts")
	defer suite.Cleanup()

	ctx := context.Background()
	now := time.Now()

	_ = tl.Append(ctx, "key1", now, map[string]string{"field1": "value1"}, false)

	// Should fail
	err := tl.Append(ctx, "key1", now, map[string]string{"field1": "value2"}, false)
	assert.Error(t, err)

	// Should succeed with force
	err = tl.Append(ctx, "key1", now, map[string]string{"field1": "value2"}, true)
	assert.NoError(t, err)
}

func (suite *TimelineTestSuite) TestTimestampNormalization(t *testing.T) {
	tl := suite.CreateTimeline("test_normalization")
	defer suite.Cleanup()

	ctx := context.Background()
	ts := time.Date(2024, 6, 13, 10, 0, 0, 123456789, time.UTC)

	_ = tl.Append(ctx, "key1", ts, map[string]string{"field1": "value1"}, false)

	// Query with same time (nanoseconds should be truncated)
	normResults, err := tl.GetAt(ctx, []string{"key1"}, ts, model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, normResults[0])
	assert.Equal(t, "value1", normResults[0]["field1"].Value)
}

func (suite *TimelineTestSuite) TestGetRange(t *testing.T) {
	tl := suite.CreateTimeline("test_get_range")
	defer suite.Cleanup()

	ctx := context.Background()
	base := time.Now()

	_ = tl.Append(ctx, "key1", base, map[string]string{"v": "1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"v": "2"}, false)
	_ = tl.Append(ctx, "key1", base.Add(2*time.Second), map[string]string{"v": "3"}, false)

	rangeResults, err := tl.GetRange(ctx, []string{"key1"}, base, base.Add(2*time.Second), model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, rangeResults[0])
	assert.Len(t, rangeResults[0], 3)
}

func (suite *TimelineTestSuite) TestGetLatest(t *testing.T) {
	tl := suite.CreateTimeline("test_get_latest")
	defer suite.Cleanup()

	ctx := context.Background()
	base := time.Now()

	_ = tl.Append(ctx, "key1", base, map[string]string{"f1": "v1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"f2": "v2"}, false)

	latestResults, err := tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, latestResults[0])
	assert.Equal(t, "v1", latestResults[0]["f1"].Value)
	assert.Equal(t, "v2", latestResults[0]["f2"].Value)
}

func (suite *TimelineTestSuite) TestTimeline(t *testing.T) {
	tl := suite.CreateTimeline("test_timeline")
	defer suite.Cleanup()

	ctx := context.Background()
	base := time.Now()

	_ = tl.Append(ctx, "key1", base, map[string]string{"v": "1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"v": "2"}, false)

	results, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, results)
	require.Contains(t, results, "v")
	assert.Len(t, results["v"], 2)
}

func (suite *TimelineTestSuite) TestGetAffectedRange(t *testing.T) {
	tl := suite.CreateTimeline("test_affected")
	defer suite.Cleanup()

	ctx := context.Background()
	base := time.Now()

	_ = tl.Append(ctx, "key1", base, map[string]string{"v": "1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(2*time.Second), map[string]string{"v": "3"}, false)

	// Insert in between
	_ = tl.Insert(ctx, "key1", base.Add(time.Second), map[string]string{"v": "2"}, false)

	affected, err := tl.GetAffectedRange(ctx, "key1", base.Add(time.Second), model.QueryOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(affected), 2)
}

func (suite *TimelineTestSuite) TestManagementOperations(t *testing.T) {
	tl := suite.CreateTimeline("test_mgmt")
	defer suite.Cleanup()

	ctx := context.Background()
	now := time.Now()

	_ = tl.Append(ctx, "key1", now, map[string]string{"f": "v"}, false)
	_ = tl.Append(ctx, "key2", now, map[string]string{"f": "v"}, false)

	keys, err := tl.Keys(ctx, model.FilterOptions{})
	require.NoError(t, err)
	assert.Len(t, keys, 2)

	_ = tl.Remove(ctx, []string{"key1"})
	keys, err = tl.Keys(ctx, model.FilterOptions{})
	require.NoError(t, err)
	assert.Len(t, keys, 1)
}

func (suite *TimelineTestSuite) TestRetentionPolicy(t *testing.T) {
	tl := suite.CreateTimeline("test_retention")
	defer suite.Cleanup()

	ctx := context.Background()

	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    2,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	}})

	base := time.Now()
	_ = tl.Append(ctx, "key1", base, map[string]string{"v": "1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"v": "2"}, false)
	_ = tl.Append(ctx, "key1", base.Add(2*time.Second), map[string]string{"v": "3"}, false)

	timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
	require.NoError(t, err)

	// With field-level retention, each field keeps its last 2 updates
	require.Contains(t, timeline, "v")
	assert.Equal(t, 2, len(timeline["v"]), "retention should keep exactly 2 points per field")

	// Verify correct points remain (last 2)
	if len(timeline["v"]) == 2 {
		assert.Equal(t, "2", timeline["v"][0].Value, "first point should be '2'")
		assert.Equal(t, "3", timeline["v"][1].Value, "second point should be '3'")
	}
}

// TestFieldLevelRetention tests that low-frequency fields survive retention when high-frequency fields trigger cleanup
func (suite *TimelineTestSuite) TestFieldLevelRetention(t *testing.T) {
	tl := suite.CreateTimeline("test_field_retention")
	defer suite.Cleanup()

	ctx := context.Background()

	tl.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    2,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	}})

	base := time.Now()
	
	// t1: temp=20, voltage=12.5, status=on, location=A
	_ = tl.Append(ctx, "key1", base, map[string]string{
		"temp":     "20",
		"voltage":  "12.5",
		"status":   "on",
		"location": "A",
	}, false)
	
	// t2: only temp updates (high frequency)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{
		"temp": "21",
	}, false)
	
	// t3: only status updates
	_ = tl.Append(ctx, "key1", base.Add(2*time.Second), map[string]string{
		"status": "off",
	}, false)

	// With MaxCount=2 per field:
	// - temp: [t1:20, t2:21] (keeps 2)
	// - voltage: [t1:12.5] (only 1 update, keeps it)
	// - status: [t1:on, t3:off] (keeps 2)
	// - location: [t1:A] (only 1 update, keeps it)

	latest, err := tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, latest[0])
	
	// All fields should be present
	assert.Equal(t, "21", latest[0]["temp"].Value, "temp should have latest value")
	assert.Equal(t, "12.5", latest[0]["voltage"].Value, "voltage should survive retention")
	assert.Equal(t, "off", latest[0]["status"].Value, "status should have latest value")
	assert.Equal(t, "A", latest[0]["location"].Value, "location should survive retention")
}

// TestPerFieldTimestamps tests that each field includes its actual update timestamp
func (suite *TimelineTestSuite) TestPerFieldTimestamps(t *testing.T) {
	tl := suite.CreateTimeline("test_field_timestamps")
	defer suite.Cleanup()

	ctx := context.Background()
	t1 := time.Now().Truncate(time.Microsecond)
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)

	// Sparse updates at different times
	_ = tl.Append(ctx, "key1", t1, map[string]string{
		"temp":    "20",
		"voltage": "12.5",
	}, false)
	
	_ = tl.Append(ctx, "key1", t2, map[string]string{
		"temp": "21",
	}, false)
	
	_ = tl.Append(ctx, "key1", t3, map[string]string{
		"voltage": "12.3",
	}, false)

	// Query at t3
	results, err := tl.GetAt(ctx, []string{"key1"}, t3, model.QueryOptions{})
	require.NoError(t, err)
	require.NotNil(t, results[0])
	
	// temp's timestamp should be t2 (last update)
	assert.Equal(t, "21", results[0]["temp"].Value)
	assert.Equal(t, t2, results[0]["temp"].Time)
	
	// voltage's timestamp should be t3 (last update)
	assert.Equal(t, "12.3", results[0]["voltage"].Value)
	assert.Equal(t, t3, results[0]["voltage"].Time)
}

// TestQueryOptionsFiltering tests that Fields filtering works in all query methods
func (suite *TimelineTestSuite) TestQueryOptionsFiltering(t *testing.T) {
	tl := suite.CreateTimeline("test_query_options")
	defer suite.Cleanup()

	ctx := context.Background()
	now := time.Now()

	_ = tl.Append(ctx, "key1", now, map[string]string{
		"field1": "value1",
		"field2": "value2",
		"field3": "value3",
	}, false)

	// Test GetAt with field filtering
	results, err := tl.GetAt(ctx, []string{"key1"}, now, model.QueryOptions{
		Fields: []string{"field1", "field3"},
	})
	require.NoError(t, err)
	require.NotNil(t, results[0])
	assert.Len(t, results[0], 2)
	assert.Contains(t, results[0], "field1")
	assert.Contains(t, results[0], "field3")
	assert.NotContains(t, results[0], "field2")

	// Test GetLatest with field filtering
	latest, err := tl.GetLatest(ctx, []string{"key1"}, model.QueryOptions{
		Fields: []string{"field2"},
	})
	require.NoError(t, err)
	require.NotNil(t, latest[0])
	assert.Len(t, latest[0], 1)
	assert.Contains(t, latest[0], "field2")

	// Test Timeline with field filtering
	timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{
		Fields: []string{"field1"},
	})
	require.NoError(t, err)
	assert.Len(t, timeline, 1)
	assert.Contains(t, timeline, "field1")
	assert.NotContains(t, timeline, "field2")
}

// TestKeysWithAfterTs tests time-based key filtering
func (suite *TimelineTestSuite) TestKeysWithAfterTs(t *testing.T) {
	tl := suite.CreateTimeline("test_keys_after_ts")
	defer suite.Cleanup()

	ctx := context.Background()
	t1 := time.Now().Truncate(time.Microsecond)
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)

	// Add keys at different times
	_ = tl.Append(ctx, "k1", t1, map[string]string{"f": "v1"}, false)
	_ = tl.Append(ctx, "k2", t2, map[string]string{"f": "v2"}, false)
	_ = tl.Append(ctx, "k3", t3, map[string]string{"f": "v3"}, false)

	// Query keys updated after t1 (exclusive)
	keys, err := tl.Keys(ctx, model.FilterOptions{AfterTs: &t1})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k2", "k3"}, keys)

	// Query keys updated after t2 (exclusive)
	keys, err = tl.Keys(ctx, model.FilterOptions{AfterTs: &t2})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k3"}, keys)

	// Query keys updated after t3 (exclusive) - should be empty
	keys, err = tl.Keys(ctx, model.FilterOptions{AfterTs: &t3})
	require.NoError(t, err)
	assert.Empty(t, keys)
}

// TestTimelineFieldGrouping tests that Timeline returns per-field time series
func (suite *TimelineTestSuite) TestTimelineFieldGrouping(t *testing.T) {
	tl := suite.CreateTimeline("test_timeline_grouping")
	defer suite.Cleanup()

	ctx := context.Background()
	base := time.Now().Truncate(time.Microsecond)

	// Create timeline with multiple fields updating at different times
	_ = tl.Append(ctx, "key1", base, map[string]string{
		"temp":    "20",
		"voltage": "12.5",
	}, false)
	
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{
		"temp": "21",
	}, false)
	
	_ = tl.Append(ctx, "key1", base.Add(2*time.Second), map[string]string{
		"voltage": "12.3",
		"temp":    "22",
	}, false)

	// Query Timeline - should return per-field time series
	timeline, err := tl.Timeline(ctx, "key1", model.QueryOptions{})
	require.NoError(t, err)
	
	// Should have 2 fields
	assert.Len(t, timeline, 2)
	assert.Contains(t, timeline, "temp")
	assert.Contains(t, timeline, "voltage")
	
	// temp has 3 updates
	assert.Len(t, timeline["temp"], 3)
	assert.Equal(t, "20", timeline["temp"][0].Value)
	assert.Equal(t, base, timeline["temp"][0].Time)
	assert.Equal(t, "21", timeline["temp"][1].Value)
	assert.Equal(t, "22", timeline["temp"][2].Value)
	
	// voltage has 2 updates
	assert.Len(t, timeline["voltage"], 2)
	assert.Equal(t, "12.5", timeline["voltage"][0].Value)
	assert.Equal(t, base, timeline["voltage"][0].Time)
	assert.Equal(t, "12.3", timeline["voltage"][1].Value)
}

func (suite *TimelineTestSuite) TestContextCancellation(t *testing.T) {
	tl := suite.CreateTimeline("test_context")
	defer suite.Cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tl.Append(ctx, "key1", time.Now(), map[string]string{"f": "v"}, false)
	assert.Error(t, err)
}
