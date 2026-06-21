package test

import (
	"context"
	"testing"
	"time"

	"github.com/leonkaihao/cache/pkg/model"
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
	t.Run("GetExact", suite.TestGetExact)
	t.Run("GetRange", suite.TestGetRange)
	t.Run("GetLatest", suite.TestGetLatest)
	t.Run("Timeline", suite.TestTimeline)
	t.Run("GetAffectedRange", suite.TestGetAffectedRange)
	t.Run("ManagementOperations", suite.TestManagementOperations)
	t.Run("RetentionPolicy", suite.TestRetentionPolicy)
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

	state, err := tl.GetAt(ctx, []string{"key1"}, now)
	require.NoError(t, err)
	assert.Equal(t, "value1", state[0]["field1"])
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

	results, err := tl.GetAt(ctx, []string{"key1"}, t2)
	require.NoError(t, err)
	assert.Equal(t, "updated", results[0]["field1"])
	assert.Equal(t, "value2", results[0]["field2"])
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
	normResults, err := tl.GetExact(ctx, []string{"key1"}, ts)
	require.NoError(t, err)
	assert.Equal(t, "value1", normResults[0]["field1"])
}

func (suite *TimelineTestSuite) TestGetExact(t *testing.T) {
	tl := suite.CreateTimeline("test_get_exact")
	defer suite.Cleanup()

	ctx := context.Background()
	t1 := time.Now()
	t2 := t1.Add(time.Second)

	_ = tl.Append(ctx, "key1", t1, map[string]string{"field1": "value1"}, false)
	_ = tl.Append(ctx, "key1", t2, map[string]string{"field2": "value2"}, false)

	// Get exact t2 (should only have field2)
	exactResults, err := tl.GetExact(ctx, []string{"key1"}, t2)
	require.NoError(t, err)
	assert.Len(t, exactResults[0], 1)
	assert.Equal(t, "value2", exactResults[0]["field2"])
}

func (suite *TimelineTestSuite) TestGetRange(t *testing.T) {
	tl := suite.CreateTimeline("test_get_range")
	defer suite.Cleanup()

	ctx := context.Background()
	base := time.Now()

	_ = tl.Append(ctx, "key1", base, map[string]string{"v": "1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"v": "2"}, false)
	_ = tl.Append(ctx, "key1", base.Add(2*time.Second), map[string]string{"v": "3"}, false)

	rangeResults, err := tl.GetRange(ctx, []string{"key1"}, base, base.Add(2*time.Second))
	require.NoError(t, err)
	assert.Len(t, rangeResults[0], 3)
}

func (suite *TimelineTestSuite) TestGetLatest(t *testing.T) {
	tl := suite.CreateTimeline("test_get_latest")
	defer suite.Cleanup()

	ctx := context.Background()
	base := time.Now()

	_ = tl.Append(ctx, "key1", base, map[string]string{"f1": "v1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"f2": "v2"}, false)

	latestResults, err := tl.GetLatest(ctx, []string{"key1"})
	require.NoError(t, err)
	assert.Equal(t, "v1", latestResults[0]["f1"])
	assert.Equal(t, "v2", latestResults[0]["f2"])
}

func (suite *TimelineTestSuite) TestTimeline(t *testing.T) {
	tl := suite.CreateTimeline("test_timeline")
	defer suite.Cleanup()

	ctx := context.Background()
	base := time.Now()

	_ = tl.Append(ctx, "key1", base, map[string]string{"v": "1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"v": "2"}, false)

	results, err := tl.Timeline(ctx, "key1")
	require.NoError(t, err)
	assert.Len(t, results, 2)
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

	affected, err := tl.GetAffectedRange(ctx, "key1", base.Add(time.Second))
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

	keys, err := tl.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 2)

	_ = tl.Remove(ctx, []string{"key1"})
	keys, err = tl.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 1)
}

func (suite *TimelineTestSuite) TestRetentionPolicy(t *testing.T) {
	tl := suite.CreateTimeline("test_retention")
	defer suite.Cleanup()

	ctx := context.Background()

	_ = tl.SetRetention(model.RetentionPolicy{
		MaxCount:    2,
		MaxDuration: 0,
		Strategy:    model.RetentionMax,
	})

	base := time.Now()
	_ = tl.Append(ctx, "key1", base, map[string]string{"v": "1"}, false)
	_ = tl.Append(ctx, "key1", base.Add(time.Second), map[string]string{"v": "2"}, false)
	_ = tl.Append(ctx, "key1", base.Add(2*time.Second), map[string]string{"v": "3"}, false)

	timeline, err := tl.Timeline(ctx, "key1")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(timeline), 2)
}

func (suite *TimelineTestSuite) TestContextCancellation(t *testing.T) {
	tl := suite.CreateTimeline("test_context")
	defer suite.Cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tl.Append(ctx, "key1", time.Now(), map[string]string{"f": "v"}, false)
	assert.Error(t, err)
}
