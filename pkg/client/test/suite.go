package test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestData is a simple test data structure
type TestData struct {
	Value string `json:"value"`
}

// BucketFactory creates a bucket for testing
type BucketFactory func() (model.CacheBucket, error)

// ClientFactory creates a cache client for testing
type ClientFactory func() model.CacheClient

// TestSuite runs a comprehensive test suite against any CacheClient implementation
type TestSuite struct {
	Name          string
	BucketFactory BucketFactory
	ClientFactory ClientFactory
}

// RunAll runs all tests in the suite
func (s *TestSuite) RunAll(t *testing.T) {
	t.Run(s.Name+"/BasicCRUD", s.TestBasicCRUD)
	t.Run(s.Name+"/Labels", s.TestLabels)
	t.Run(s.Name+"/Timestamps", s.TestTimestamps)
	t.Run(s.Name+"/Expiration", s.TestExpiration)
	t.Run(s.Name+"/Collections", s.TestCollections)
	t.Run(s.Name+"/Concurrency", s.TestConcurrency)
	t.Run(s.Name+"/ErrorCases", s.TestErrorCases)
	t.Run(s.Name+"/ContextCancellation", s.TestContextCancellation)
}

// TestBasicCRUD tests basic create, read, update, delete operations
func (s *TestSuite) TestBasicCRUD(t *testing.T) {
	ctx := context.Background()
	bkt, err := s.BucketFactory()
	require.NoError(t, err)
	defer func() { _ = bkt.Delete(ctx) }()

	// Create
	doc, err := bkt.Update(ctx, "key1", &TestData{"value1"})
	require.NoError(t, err)
	assert.Equal(t, "key1", doc.Key())

	// Read
	val, err := doc.Val(ctx)
	require.NoError(t, err)
	assert.Equal(t, "value1", val.(*TestData).Value)

	// Update
	err = doc.SetValue(ctx, &TestData{"value2"})
	require.NoError(t, err)
	val, err = doc.Val(ctx)
	require.NoError(t, err)
	assert.Equal(t, "value2", val.(*TestData).Value)

	// Delete
	err = doc.Delete(ctx)
	require.NoError(t, err)

	// Verify deletion
	docs, err := bkt.Docs(ctx, []string{"key1"})
	require.NoError(t, err)
	assert.Nil(t, docs[0])
}

// TestLabels tests label operations
func (s *TestSuite) TestLabels(t *testing.T) {
	ctx := context.Background()
	bkt, err := s.BucketFactory()
	require.NoError(t, err)
	defer func() { _ = bkt.Delete(ctx) }()

	// Create docs with labels
	doc1, err := bkt.Update(ctx, "doc1", &TestData{"val1"})
	require.NoError(t, err)
	require.NoError(t, doc1.AddLabels(ctx, []string{"foo", "bar"}))

	doc2, err := bkt.Update(ctx, "doc2", &TestData{"val2"})
	require.NoError(t, err)
	require.NoError(t, doc2.AddLabels(ctx, []string{"foo"}))

	doc3, err := bkt.Update(ctx, "doc3", &TestData{"val3"})
	require.NoError(t, err)
	require.NoError(t, doc3.AddLabels(ctx, []string{"bar"}))

	// Filter by single label
	keys, err := bkt.Filter(ctx, []string{"foo"})
	require.NoError(t, err)
	assert.Len(t, keys, 2) // doc1, doc2

	keys, err = bkt.Filter(ctx, []string{"bar"})
	require.NoError(t, err)
	assert.Len(t, keys, 2) // doc1, doc3

	// Filter by multiple labels (AND logic)
	keys, err = bkt.Filter(ctx, []string{"foo"}, []string{"bar"})
	require.NoError(t, err)
	assert.Len(t, keys, 1) // doc1

	// Remove labels
	require.NoError(t, doc1.RemoveLabels(ctx, []string{"foo"}))
	keys, err = bkt.Filter(ctx, []string{"foo"})
	require.NoError(t, err)
	assert.Len(t, keys, 1) // doc2

	// Get labels
	labels, err := doc1.Labels(ctx)
	require.NoError(t, err)
	assert.Contains(t, labels, "bar")
	assert.NotContains(t, labels, "foo")
}

// TestTimestamps tests timestamp operations and equal timestamp rejection
func (s *TestSuite) TestTimestamps(t *testing.T) {
	ctx := context.Background()
	bkt, err := s.BucketFactory()
	require.NoError(t, err)
	defer func() { _ = bkt.Delete(ctx) }()

	// Create with timestamp
	// Note: Use UTC and strip monotonic clock with Truncate(0) for consistent
	// timestamp handling across different environments and persistence layers
	ts1 := time.Now().UTC().Truncate(0)
	doc, updated, err := bkt.UpdateWithTs(ctx, "key1", &TestData{"val1"}, ts1)
	require.NoError(t, err)
	require.True(t, updated)

	docTime, err := doc.Time(ctx)
	require.NoError(t, err)
	assert.Equal(t, ts1, docTime)

	// Update with newer timestamp
	ts2 := ts1.Add(time.Second)
	updated, err = doc.SetValueWithTs(ctx, &TestData{"val2"}, ts2)
	require.NoError(t, err)
	require.True(t, updated)

	val, err := doc.Val(ctx)
	require.NoError(t, err)
	assert.Equal(t, "val2", val.(*TestData).Value)

	// Try to update with equal timestamp (should be rejected)
	updated, err = doc.SetValueWithTs(ctx, &TestData{"val3"}, ts2)
	require.NoError(t, err)
	require.False(t, updated, "equal timestamp should be rejected")

	val, err = doc.Val(ctx)
	require.NoError(t, err)
	assert.Equal(t, "val2", val.(*TestData).Value, "value should not change")

	// Try to update with older timestamp (should be rejected)
	updated, err = doc.SetValueWithTs(ctx, &TestData{"val4"}, ts1)
	require.NoError(t, err)
	require.False(t, updated, "older timestamp should be rejected")

	val, err = doc.Val(ctx)
	require.NoError(t, err)
	assert.Equal(t, "val2", val.(*TestData).Value, "value should not change")
}

// TestExpiration tests expiration functionality
func (s *TestSuite) TestExpiration(t *testing.T) {
	ctx := context.Background()
	bkt, err := s.BucketFactory()
	require.NoError(t, err)
	defer func() { _ = bkt.Delete(ctx) }()

	// Test that callback fires after expiration (500ms timeout, 1s verification window)
	// Buffered channel for race-free callback coordination
	expiredCh := make(chan model.CacheDoc, 1)
	doc, err := bkt.Update(ctx, "key1", &TestData{"val1"})
	require.NoError(t, err)

	require.NoError(t, doc.Expire(500*time.Millisecond, func(d model.CacheDoc) {
		expiredCh <- d
	}))

	select {
	case d := <-expiredCh:
		assert.NotNil(t, d, "callback should receive non-nil document")
		assert.Equal(t, "key1", d.Key(), "callback should receive correct document")
	case <-time.After(1 * time.Second):
		t.Fatal("expiration callback did not fire within timeout")
	}

	// Test canceling expiration - separate channel to prevent cross-contamination
	canceledCh := make(chan model.CacheDoc, 1)
	doc2, err := bkt.Update(ctx, "key2", &TestData{"val2"})
	require.NoError(t, err)

	require.NoError(t, doc2.Expire(500*time.Millisecond, func(d model.CacheDoc) {
		canceledCh <- d
	}))

	require.NoError(t, doc2.CancelExpire())

	select {
	case <-canceledCh:
		t.Fatal("expiration callback should not fire after cancel")
	case <-time.After(1 * time.Second):
		// Success: callback did not fire
	}
}

// TestCollections tests collection operations
func (s *TestSuite) TestCollections(t *testing.T) {
	ctx := context.Background()
	cli := s.ClientFactory()
	clt := cli.Collection("test_collection")
	defer func() { _ = clt.Delete(ctx) }()

	// Add members
	require.NoError(t, clt.Add(ctx, "key1", []string{"mem1", "mem2", "mem3"}))
	require.NoError(t, clt.Add(ctx, "key2", []string{"mem4", "mem5"}))

	// Get keys
	keys, err := clt.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 2)

	// Get members map
	mm, err := clt.MembersMap(ctx, "key1")
	require.NoError(t, err)
	assert.Len(t, mm, 3)
	assert.Contains(t, mm, "mem1")

	// Remove members
	require.NoError(t, clt.Remove(ctx, "key1", []string{"mem1"}))
	mm, err = clt.MembersMap(ctx, "key1")
	require.NoError(t, err)
	assert.Len(t, mm, 2)

	// Clear key
	require.NoError(t, clt.Clear(ctx, "key2"))
	mm, err = clt.MembersMap(ctx, "key2")
	require.NoError(t, err)
	assert.Nil(t, mm)

	// Clear all
	require.NoError(t, clt.ClearAll(ctx))
	keys, err = clt.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 0)
}

// TestConcurrency tests concurrent operations
func (s *TestSuite) TestConcurrency(t *testing.T) {
	ctx := context.Background()
	bkt, err := s.BucketFactory()
	require.NoError(t, err)
	defer func() { _ = bkt.Delete(ctx) }()

	wg := sync.WaitGroup{}
	errors := make(chan error, 200)

	// Concurrent writes
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", idx)
			_, err := bkt.Update(ctx, key, &TestData{fmt.Sprintf("val%d", idx)})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent reads
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			defer wg.Done()
			keys := []string{fmt.Sprintf("key%d", idx)}
			_, err := bkt.Docs(ctx, keys)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent operation failed: %v", err)
	}
}

// TestErrorCases tests error handling for edge cases
func (s *TestSuite) TestErrorCases(t *testing.T) {
	ctx := context.Background()
	cli := s.ClientFactory()
	bkt, err := s.BucketFactory()
	require.NoError(t, err)
	defer func() { _ = bkt.Delete(ctx) }()

	// Empty key
	_, err = bkt.Update(ctx, "", &TestData{"val"})
	assert.Error(t, err)

	// Empty members in collection
	clt := cli.Collection("test_clt")
	defer func() { _ = clt.Delete(ctx) }()
	err = clt.Add(ctx, "key1", []string{})
	assert.Error(t, err)

	// Empty key in collection
	err = clt.Add(ctx, "", []string{"mem1"})
	assert.Error(t, err)
}

// TestContextCancellation tests context cancellation handling
func (s *TestSuite) TestContextCancellation(t *testing.T) {
	bkt, err := s.BucketFactory()
	require.NoError(t, err)
	defer func() { _ = bkt.Delete(context.Background()) }()

	// Create a doc with good context
	doc, err := bkt.Update(context.Background(), "key1", &TestData{"val1"})
	require.NoError(t, err)

	// Use cancelled context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // Ensure timeout

	// Operations should fail with context error
	err = doc.AddLabels(ctx, []string{"label1"})
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
