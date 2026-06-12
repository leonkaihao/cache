package mem

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
	"github.com/leonkaihao/cache/pkg/model"
)

type testData struct {
	data string
}

func expectFilter(t *testing.T, bkt model.CacheBucket, filters [][]string, sz int) {
	ctx := context.Background()
	result, err := bkt.Filter(ctx, filters...)
	require.NoError(t, err)
	if len(result) != sz {
		t.Errorf("expect get %v results from filter %v, but got %v", sz, filters, len(result))
	}
}

func TestBucket(t *testing.T) {
	ctx := context.Background()
	cli := NewClient()
	bkt, err := NewBucket[testData](cli, "111")
	require.NoError(t, err)

	doc1, err := bkt.Update(ctx, "1", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc1.AddLabels(ctx, []string{"foo", "bar"}))

	doc2, err := bkt.Update(ctx, "2", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc2.AddLabels(ctx, []string{"bar"}))

	doc3, err := bkt.Update(ctx, "3", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc3.AddLabels(ctx, []string{"bar"}))

	doc4, err := bkt.Update(ctx, "4", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc4.AddLabels(ctx, []string{"bar", "foo"}))

	doc5, err := bkt.Update(ctx, "5", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc5.AddLabels(ctx, []string{"foo"}))

	doc6, err := bkt.Update(ctx, "6", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc6.AddLabels(ctx, []string{"foo"}))

	doc7, err := bkt.Update(ctx, "7", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc7.AddLabels(ctx, []string{"foo"}))

	expectFilter(t, bkt, [][]string{{}}, 7)               // 1,2,3,4,5,6,7
	expectFilter(t, bkt, nil, 7)                          // 1,2,3,4,5,6,7
	expectFilter(t, bkt, [][]string{nil}, 7)              // 1,2,3,4,5,6,7
	expectFilter(t, bkt, [][]string{{"foo"}}, 5)          // 1,4,5,6,7
	expectFilter(t, bkt, [][]string{{"bar"}}, 4)          // 1,2,3,4
	expectFilter(t, bkt, [][]string{{"foo"}, {"bar"}}, 2) // 1,4

	require.NoError(t, bkt.Remove(ctx, []string{"1", "3", "5"}))
	expectFilter(t, bkt, [][]string{{"foo"}}, 3)          //4,6,7
	expectFilter(t, bkt, [][]string{{"bar"}}, 2)          //2,4
	expectFilter(t, bkt, [][]string{{"foo"}, {"bar"}}, 1) //4
	expectFilter(t, bkt, [][]string{{"foo", "bar"}}, 4)   //4,6,7,2

	ts := timestamppb.Now().AsTime()
	doc, updated, err := bkt.UpdateWithTs(ctx, "7", &testData{"data2"}, ts) // existing key
	require.NoError(t, err)
	require.True(t, updated)
	docTime, err := doc.Time(ctx)
	require.NoError(t, err)
	assert.Equal(t, ts, docTime)

	doc, updated, err = bkt.UpdateWithTs(ctx, "8888", &testData{"data2"}, ts) // not existing key
	require.NoError(t, err)
	require.True(t, updated)
	docTime, err = doc.Time(ctx)
	require.NoError(t, err)
	assert.Equal(t, ts, docTime)

	docs, err := bkt.Docs(ctx, []string{"notexist"})
	require.NoError(t, err)
	assert.Nil(t, docs[0])

	vals, err := bkt.Values(ctx, []string{"notexist"})
	require.NoError(t, err)
	assert.Nil(t, vals[0])
}

func TestDoc(t *testing.T) {
	ctx := context.Background()
	cli := NewClient()
	bkt, err := NewBucket[testData](cli, "111")
	require.NoError(t, err)

	doc1, err := bkt.Update(ctx, "1", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc1.AddLabels(ctx, []string{"foo"}))

	doc2, err := bkt.Update(ctx, "2", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc2.AddLabels(ctx, []string{"bar", "foo"}))

	doc3, err := bkt.Update(ctx, "3", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc3.AddLabels(ctx, []string{"bar", "foo", "new"}))

	doc4, err := bkt.Update(ctx, "4", &testData{"data"})
	require.NoError(t, err)
	require.NoError(t, doc4.AddLabels(ctx, []string{"bar", "foo", "new", "bee"}))

	require.NoError(t, doc1.RemoveLabels(ctx, []string{"bar"})) // not allowed (doesn't have bar)
	expectFilter(t, bkt, [][]string{{"bar"}}, 3)                // 2,3,4

	require.NoError(t, doc2.RemoveLabels(ctx, []string{"bar"}))
	expectFilter(t, bkt, [][]string{{"bar"}}, 2) // 3,4

	require.NoError(t, doc3.AddLabels(ctx, []string{"bar"}))
	expectFilter(t, bkt, [][]string{{"bar"}}, 2) // 3,4

	require.NoError(t, doc4.RemoveLabels(ctx, []string{"bar", "foo", "new", "bee"}))
	expectFilter(t, bkt, [][]string{{"bar"}, {"foo"}, {"new"}}, 1) // 3

	require.NoError(t, doc3.Delete(ctx))
	expectFilter(t, bkt, [][]string{{"foo"}}, 2) // 1, 2

	ts := timestamppb.Now().AsTime()
	updated, err := doc4.SetValueWithTs(ctx, &testData{"newVal"}, ts)
	require.NoError(t, err)
	require.True(t, updated)
	docTime, err := doc4.Time(ctx)
	require.NoError(t, err)
	assert.Equal(t, ts, docTime)

	require.NoError(t, doc1.Expire(time.Second, func(doc model.CacheDoc) {
		_ = doc.Delete(context.Background())
	}))
	time.Sleep(2 * time.Second)
	expectFilter(t, bkt, [][]string{{"foo"}}, 1) // 2
}

type testInt struct {
	data int
}

func TestBucketConcurrent(t *testing.T) {
	ctx := context.Background()
	cli := NewClient()
	bkt, err := NewBucket[testInt](cli, "TST")
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("%v", i)
			doc, err := bkt.Update(ctx, key, &testInt{i})
			if err != nil {
				t.Errorf("Update failed: %v", err)
				return
			}
			if err := doc.AddLabels(ctx, []string{key}); err != nil {
				t.Errorf("AddLabels failed: %v", err)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond * 300)
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("%v", i)
			if err := bkt.Remove(ctx, []string{key}); err != nil {
				t.Errorf("Remove failed: %v", err)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	result, err := bkt.Filter(ctx, nil)
	require.NoError(t, err)
	if len(result) != 0 {
		t.Errorf("%v docs left, expect 0", len(result))
	}
}

func Test_bucket_Scan(t *testing.T) {
	ctx := context.Background()
	cli := NewClient()
	bkt, err := NewBucket[testData](cli, "TST")
	require.NoError(t, err)

	_, err = bkt.Update(ctx, "foo", &testData{"foo"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "bar", &testData{"bar"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "foobar", &testData{"foobar"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "foo:bar", &testData{"foo:bar"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "foo@bar", &testData{"foo@bar"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "foo$bar", &testData{"foo$bar"})
	require.NoError(t, err)

	type TestItem struct {
		expression string
		expect     int
	}
	testExps := []TestItem{
		{"foo", 1},
		{"bar", 1},
		{"foo*", 5},
		{"*bar", 5},
		{"*:*", 1},
		{"*@*", 1},
		{"foo*bar", 4},
		{"foo$bar", 1},
	}
	for _, item := range testExps {
		result, err := bkt.Scan(ctx, item.expression)
		require.NoError(t, err)
		assert.Equal(t, item.expect, len(result))
	}
}

// Test for concurrent Delete + AddLabels race condition
func TestDocDeleteRaceCondition(t *testing.T) {
	ctx := context.Background()
	cli := NewClient()
	bkt, err := NewBucket[testData](cli, "TestRace")
	require.NoError(t, err)

	doc, err := bkt.Update(ctx, "testkey", &testData{"data"})
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(2)

	// Goroutine 1: Delete document
	go func() {
		defer wg.Done()
		_ = doc.Delete(ctx)
	}()

	// Goroutine 2: Add labels
	go func() {
		defer wg.Done()
		_ = doc.AddLabels(ctx, []string{"testlabel"})
	}()

	wg.Wait()
	// No panic should occur - test passes if it completes
}

// Test for Clear stopping expiration timers
func TestClearStopsExpiration(t *testing.T) {
	ctx := context.Background()
	cli := NewClient()
	bkt, err := NewBucket[testData](cli, "TestExpire")
	require.NoError(t, err)

	expired := false
	doc, err := bkt.Update(ctx, "testkey", &testData{"data"})
	require.NoError(t, err)

	require.NoError(t, doc.Expire(2*time.Second, func(doc model.CacheDoc) {
		expired = true
	}))

	// Clear bucket before expiration
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, bkt.Clear(ctx))

	// Wait past expiration time
	time.Sleep(2 * time.Second)

	// Timer should have been stopped, so expired should be false
	assert.False(t, expired, "Expiration callback should not fire after Clear")
}

// Test for context cancellation handling
func TestContextCancellation(t *testing.T) {
	cli := NewClient()
	bkt, err := NewBucket[testData](cli, "TestCtx")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Add a document first with good context
	doc, err := bkt.Update(context.Background(), "key", &testData{"data"})
	require.NoError(t, err)

	// Wait for context timeout
	<-ctx.Done()

	// Operations with cancelled/timeout context should return error
	err = doc.AddLabels(ctx, []string{"label"})
	assert.Error(t, err, "should fail with cancelled context")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func BenchmarkUpdateData1000(b *testing.B) {
	ctx := context.Background()
	cli := NewClient()
	bkt, _ := NewBucket[testInt](cli, "TST")
	defer func() { _ = bkt.Clear(ctx) }()
	defer b.StopTimer()
	b.ResetTimer()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _ := bkt.Update(ctx, key, &testInt{i})
		_ = doc.AddLabels(ctx, []string{key})
	}
}

func BenchmarkUpdateDataWithTs1000(b *testing.B) {
	ctx := context.Background()
	cli := NewClient()
	bkt, _ := NewBucket[testInt](cli, "TST")
	defer func() { _ = bkt.Clear(ctx) }()
	defer b.StopTimer()
	b.ResetTimer()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _, _ := bkt.UpdateWithTs(ctx, key, &testInt{i}, time.Now())
		_ = doc.AddLabels(ctx, []string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
}

func BenchmarkFilter1000Label1(b *testing.B) {
	ctx := context.Background()
	cli := NewClient()
	bkt, _ := NewBucket[testInt](cli, "TST")
	defer func() { _ = bkt.Clear(ctx) }()
	defer b.StopTimer()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _, _ := bkt.UpdateWithTs(ctx, key, &testInt{i}, time.Now())
		_ = doc.AddLabels(ctx, []string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.ResetTimer()
	results, _ := bkt.Filter(ctx, []string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	if len(results) != 1000 {
		b.Errorf("expect 1000 got %v", len(results))
	}
}

func BenchmarkFilter1000Label8(b *testing.B) {
	ctx := context.Background()
	cli := NewClient()
	bkt, _ := NewBucket[testInt](cli, "TST")
	defer func() { _ = bkt.Clear(ctx) }()
	defer b.StopTimer()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _, _ := bkt.UpdateWithTs(ctx, key, &testInt{i}, time.Now())
		_ = doc.AddLabels(ctx, []string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.ResetTimer()
	results, _ := bkt.Filter(ctx, []string{"label1", "label2"}, []string{"label3", "label4"}, []string{"label5", "label6"}, []string{"label7", "label8"})
	if len(results) != 1000 {
		b.Errorf("expect 1000 got %v", len(results))
	}
}
