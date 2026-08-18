//go:build integration
// +build integration

package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/coding"
	"github.com/leonkaihao/cache/v2/pkg/consts"
	"github.com/leonkaihao/cache/v2/pkg/model"
	redis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to get redis client for direct key manipulation
func getTestRedisClient(t *testing.T) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})
}

// Test scanAndDeleteByPrefix with matching keys
func TestScanAndDeleteByPrefix_SuccessfulClear(t *testing.T) {
	ctx := context.Background()
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	// Create test keys
	pattern := "test-scan-success-*"
	keys := []string{
		"test-scan-success-key1",
		"test-scan-success-key2",
		"test-scan-success-key3",
	}

	for _, key := range keys {
		err := redisCli.Set(ctx, key, "value", 0).Err()
		require.NoError(t, err)
	}

	// Verify keys exist
	for _, key := range keys {
		exists, err := redisCli.Exists(ctx, key).Result()
		require.NoError(t, err)
		assert.Equal(t, int64(1), exists)
	}

	// Clear using scanAndDeleteByPrefix
	err := scanAndDeleteByPrefix(ctx, redisCli, pattern)
	require.NoError(t, err)

	// Verify all keys deleted
	for _, key := range keys {
		exists, err := redisCli.Exists(ctx, key).Result()
		require.NoError(t, err)
		assert.Equal(t, int64(0), exists, "Key %s should be deleted", key)
	}
}

// Test scanAndDeleteByPrefix with empty result set
func TestScanAndDeleteByPrefix_EmptyResultSet(t *testing.T) {
	ctx := context.Background()
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	// Use a pattern that matches nothing
	pattern := "test-nonexistent-pattern-*"

	err := scanAndDeleteByPrefix(ctx, redisCli, pattern)
	require.NoError(t, err, "Should succeed even with no matching keys")
}

// Test scanAndDeleteByPrefix with large key set (>100 keys)
func TestScanAndDeleteByPrefix_LargeKeySet(t *testing.T) {
	ctx := context.Background()
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	// Create 150 keys to test batching
	pattern := "test-scan-large-*"
	keyCount := 150

	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("test-scan-large-%d", i)
		err := redisCli.Set(ctx, key, "value", 0).Err()
		require.NoError(t, err)
	}

	// Clear using scanAndDeleteByPrefix
	err := scanAndDeleteByPrefix(ctx, redisCli, pattern)
	require.NoError(t, err)

	// Verify all keys deleted
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 200).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All keys should be deleted")
}

// Test scanAndDeleteByPrefix error handling for SCAN failure
func TestScanAndDeleteByPrefix_ScanFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context immediately

	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	pattern := "test-scan-fail-*"
	err := scanAndDeleteByPrefix(ctx, redisCli, pattern)
	assert.Error(t, err, "Should return error when SCAN fails")
}

// Test bucket clear removes orphaned document keys
func TestBucketClear_RemovesOrphanedDocKeys(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	bucketName := "test-bucket-orphan-docs"
	bkt, err := NewBucket[testData](cli, bucketName, coding.NewJsonCoder())
	require.NoError(t, err)

	// Create normal document
	doc, err := bkt.Update(ctx, "normal-key", &testData{Data: "normal"})
	require.NoError(t, err)
	require.NotNil(t, doc)

	// Manually create orphaned document key (not in tracking set)
	orphanKey := fmt.Sprintf("%s%s/%sorphaned-doc", consts.BUCKET_PREFIX, bucketName, consts.KEYS_PREFIX)
	err = redisCli.HSet(ctx, orphanKey, "field", "orphaned").Err()
	require.NoError(t, err)

	// Verify orphan exists
	exists, err := redisCli.Exists(ctx, orphanKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists)

	// Clear bucket
	err = bkt.Clear(ctx)
	require.NoError(t, err)

	// Verify all keys deleted (including orphan)
	pattern := fmt.Sprintf("%s%s/*", consts.BUCKET_PREFIX, bucketName)
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 100).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All bucket keys including orphans should be deleted")
}

// Test bucket clear removes orphaned label keys
func TestBucketClear_RemovesOrphanedLabelKeys(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	bucketName := "test-bucket-orphan-labels"
	bkt, err := NewBucket[testData](cli, bucketName, coding.NewJsonCoder())
	require.NoError(t, err)

	// Create normal document with label
	doc, err := bkt.Update(ctx, "key1", &testData{Data: "value1"})
	require.NoError(t, err)
	err = doc.AddLabels(ctx, []string{"label1"})
	require.NoError(t, err)

	// Manually create orphaned label key (not in tracking set)
	orphanLabelKey := fmt.Sprintf("%s%s/%sorphaned-label", consts.BUCKET_PREFIX, bucketName, consts.LABELS_PREFIX)
	err = redisCli.SAdd(ctx, orphanLabelKey, "key1").Err()
	require.NoError(t, err)

	// Clear bucket
	err = bkt.Clear(ctx)
	require.NoError(t, err)

	// Verify all keys deleted (including orphaned label)
	pattern := fmt.Sprintf("%s%s/*", consts.BUCKET_PREFIX, bucketName)
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 100).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All bucket keys including orphaned labels should be deleted")
}

// Test bucket operations work after clear without empty sets
func TestBucketClear_OperationsWorkAfterClear(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)

	bucketName := "test-bucket-after-clear"
	bkt, err := NewBucket[testData](cli, bucketName, coding.NewJsonCoder())
	require.NoError(t, err)

	// Add data
	doc, err := bkt.Update(ctx, "key1", &testData{Data: "value1"})
	require.NoError(t, err)
	require.NotNil(t, doc)

	// Clear bucket
	err = bkt.Clear(ctx)
	require.NoError(t, err)

	// Add data again (should work without pre-existing empty sets)
	_, err = bkt.Update(ctx, "key2", &testData{Data: "value2"})
	require.NoError(t, err)

	// Verify new data exists
	vals, err := bkt.Values(ctx, []string{"key2"})
	require.NoError(t, err)
	require.NotNil(t, vals[0])
	assert.Equal(t, "value2", vals[0].(*testData).Data)
}

// Test collection clear removes orphaned member keys
func TestCollectionClear_RemovesOrphanedKeys(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	collectionName := "test-clt-orphan"
	clt := cli.Collection(collectionName)

	// Add normal member
	err := clt.Add(ctx, "key1", []string{"member1"})
	require.NoError(t, err)

	// Manually create orphaned member key (not in tracking set)
	orphanKey := fmt.Sprintf("%s%s/%sorphaned-member", consts.CLT_PREFIX, collectionName, consts.KEYS_PREFIX)
	err = redisCli.SAdd(ctx, orphanKey, "member1").Err()
	require.NoError(t, err)

	// Clear collection
	err = clt.ClearAll(ctx)
	require.NoError(t, err)

	// Verify all keys deleted (including orphan)
	pattern := fmt.Sprintf("%s%s/*", consts.CLT_PREFIX, collectionName)
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 100).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All collection keys including orphans should be deleted")
}

// Test collection operations work after clear
func TestCollectionClear_OperationsWorkAfterClear(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)

	collectionName := "test-clt-after-clear"
	clt := cli.Collection(collectionName)

	// Add data
	err := clt.Add(ctx, "key1", []string{"member1"})
	require.NoError(t, err)

	// Clear collection
	err = clt.ClearAll(ctx)
	require.NoError(t, err)

	// Add data again (should work without pre-existing empty sets)
	err = clt.Add(ctx, "key2", []string{"member2"})
	require.NoError(t, err)

	// Verify new data exists
	members, err := clt.MembersMap(ctx, "key2")
	require.NoError(t, err)
	_, ok := members["member2"]
	assert.True(t, ok)
}

// Test timeline clear removes orphaned data hashes
func TestTimelineClear_RemovesOrphanedDataHashes(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	timelineName := "test-tl-orphan-data"
	tl := cli.Timeline(timelineName)

	// Add normal data
	ts := time.Now()
	err := tl.Append(ctx, "key1", ts, map[string]string{"field1": "value1"}, false)
	require.NoError(t, err)

	// Manually create orphaned data hash (not tracked)
	orphanDataKey := fmt.Sprintf("%s%s/%sorphaned-key/%d", consts.TIMELINE_PREFIX, timelineName, consts.KEYS_PREFIX, time.Now().UnixMicro())
	err = redisCli.HSet(ctx, orphanDataKey, "field", "orphaned").Err()
	require.NoError(t, err)

	// Clear timeline
	err = tl.Clear(ctx)
	require.NoError(t, err)

	// Verify all keys deleted (including orphan)
	pattern := fmt.Sprintf("%s%s/*", consts.TIMELINE_PREFIX, timelineName)
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 100).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All timeline keys including orphaned data should be deleted")
}

// Test timeline clear removes orphaned timestamp indexes
func TestTimelineClear_RemovesOrphanedTimestampIndexes(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	timelineName := "test-tl-orphan-ts"
	tl := cli.Timeline(timelineName)

	// Add normal data
	ts := time.Now()
	err := tl.Append(ctx, "key1", ts, map[string]string{"field1": "value1"}, false)
	require.NoError(t, err)

	// Manually create orphaned timestamp index
	orphanTSKey := fmt.Sprintf("%s%s/%sorphaned-key/%s", consts.TIMELINE_PREFIX, timelineName, consts.KEYS_PREFIX, consts.TS_PREFIX)
	err = redisCli.ZAdd(ctx, orphanTSKey, redis.Z{Score: float64(time.Now().UnixMicro()), Member: "ts1"}).Err()
	require.NoError(t, err)

	// Clear timeline
	err = tl.Clear(ctx)
	require.NoError(t, err)

	// Verify all keys deleted
	pattern := fmt.Sprintf("%s%s/*", consts.TIMELINE_PREFIX, timelineName)
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 100).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All timeline keys including orphaned indexes should be deleted")
}

// Test timeline clear removes orphaned label indexes
func TestTimelineClear_RemovesOrphanedLabelIndexes(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	timelineName := "test-tl-orphan-labels"
	tl := cli.Timeline(timelineName)

	// Add normal data with label
	ts := time.Now()
	err := tl.Append(ctx, "key1", ts, map[string]string{"field1": "value1"}, false)
	require.NoError(t, err)
	err = tl.AddKeyLabels(ctx, "key1", []string{"label1"})
	require.NoError(t, err)

	// Manually create orphaned label index
	orphanLabelKey := fmt.Sprintf("%s%s/%sorphaned-label", consts.TIMELINE_PREFIX, timelineName, consts.LABELS_PREFIX)
	err = redisCli.ZAdd(ctx, orphanLabelKey, redis.Z{Score: float64(time.Now().UnixMicro()), Member: "key1"}).Err()
	require.NoError(t, err)

	// Clear timeline
	err = tl.Clear(ctx)
	require.NoError(t, err)

	// Verify all keys deleted
	pattern := fmt.Sprintf("%s%s/*", consts.TIMELINE_PREFIX, timelineName)
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 100).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All timeline keys including orphaned labels should be deleted")
}

// Test timeline clear removes orphaned global timestamp index
func TestTimelineClear_RemovesOrphanedGlobalTSIndex(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	timelineName := "test-tl-orphan-global-ts"
	tl := cli.Timeline(timelineName)

	// Add normal data
	ts := time.Now()
	err := tl.Append(ctx, "key1", ts, map[string]string{"field1": "value1"}, false)
	require.NoError(t, err)

	// Manually ensure global TS index exists (it should be created automatically)
	globalTSKey := fmt.Sprintf("%s%s/%s", consts.TIMELINE_PREFIX, timelineName, consts.GLOBAL_TS_SUFFIX)

	// Clear timeline
	err = tl.Clear(ctx)
	require.NoError(t, err)

	// Verify global TS index is deleted
	exists, err := redisCli.Exists(ctx, globalTSKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists, "Global timestamp index should be deleted")

	// Verify all keys deleted
	pattern := fmt.Sprintf("%s%s/*", consts.TIMELINE_PREFIX, timelineName)
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 100).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All timeline keys should be deleted")
}

// Test timeline operations work after clear
func TestTimelineClear_OperationsWorkAfterClear(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)

	timelineName := "test-tl-after-clear"
	tl := cli.Timeline(timelineName)

	// Add data
	ts := time.Now()
	err := tl.Append(ctx, "key1", ts, map[string]string{"field1": "value1"}, false)
	require.NoError(t, err)

	// Clear timeline
	err = tl.Clear(ctx)
	require.NoError(t, err)

	// Add data again (should work without pre-existing empty sets)
	ts2 := time.Now().Add(time.Second)
	err = tl.Append(ctx, "key2", ts2, map[string]string{"field2": "value2"}, false)
	require.NoError(t, err)

	// Verify new data exists
	results, err := tl.GetLatest(ctx, []string{"key2"}, model.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, "value2", results[0]["field2"].Value)
}

// Test clearing with special characters in name
func TestClear_SpecialCharactersInName(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)
	redisCli := getTestRedisClient(t)
	defer redisCli.Close()

	// Test bucket with special characters
	bucketName := "test-bucket-special-chars_123"
	bkt, err := NewBucket[testData](cli, bucketName, coding.NewJsonCoder())
	require.NoError(t, err)

	doc, err := bkt.Update(ctx, "key1", &testData{Data: "value1"})
	require.NoError(t, err)
	require.NotNil(t, doc)

	err = bkt.Clear(ctx)
	require.NoError(t, err)

	pattern := fmt.Sprintf("%s%s/*", consts.BUCKET_PREFIX, bucketName)
	keys, _, err := redisCli.Scan(ctx, 0, pattern, 100).Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "All keys should be deleted despite special characters")
}

// Test clearing already-empty data structures
func TestClear_EmptyDataStructures(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "", 0)

	// Test bucket
	bucketName := "test-bucket-empty"
	bkt, err := NewBucket[testData](cli, bucketName, coding.NewJsonCoder())
	require.NoError(t, err)

	err = bkt.Clear(ctx)
	require.NoError(t, err, "Clearing empty bucket should succeed")

	// Test collection
	collectionName := "test-clt-empty"
	clt := cli.Collection(collectionName)

	err = clt.ClearAll(ctx)
	require.NoError(t, err, "Clearing empty collection should succeed")

	// Test timeline
	timelineName := "test-tl-empty"
	tl := cli.Timeline(timelineName)

	err = tl.Clear(ctx)
	require.NoError(t, err, "Clearing empty timeline should succeed")
}
