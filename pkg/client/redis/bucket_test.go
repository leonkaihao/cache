//go:build integration

package redis

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
	"github.com/leonkaihao/cache/pkg/coding"
	"github.com/leonkaihao/cache/pkg/model"
)

type testData struct {
	Data string `json:"data"`
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
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, err := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	require.NoError(t, err)
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)

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

	docs, err := bkt.Docs(ctx, []string{"1", "2", "4", "6"})
	require.NoError(t, err)
	require.Len(t, docs, 4)
	assert.Nil(t, docs[0], "doc '1' should not exist")
	assert.NotNil(t, docs[2], "doc '4' should exist")

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

	docs, err = bkt.Docs(ctx, []string{"notexist"})
	require.NoError(t, err)
	assert.Nil(t, docs[0])

	vals, err := bkt.Values(ctx, []string{"notexist"})
	require.NoError(t, err)
	assert.Nil(t, vals[0])
}

func TestDoc(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, err := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	require.NoError(t, err)
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)

	doc1, err := bkt.Update(ctx, "1", &testData{"11"})
	require.NoError(t, err)
	require.NoError(t, doc1.AddLabels(ctx, []string{"foo"}))

	doc2, err := bkt.Update(ctx, "2", &testData{"22"})
	require.NoError(t, err)
	require.NoError(t, doc2.AddLabels(ctx, []string{"bar", "foo"}))

	doc3, err := bkt.Update(ctx, "3", &testData{"33"})
	require.NoError(t, err)
	require.NoError(t, doc3.AddLabels(ctx, []string{"bar", "foo", "new"}))

	doc4, err := bkt.Update(ctx, "4", &testData{"44"})
	require.NoError(t, err)
	require.NoError(t, doc4.AddLabels(ctx, []string{"bar", "foo", "new", "bee"}))

	values, err := bkt.Values(ctx, []string{"1", "2", "3", "4"})
	require.NoError(t, err)
	require.Len(t, values, 4)
	data := values[2].(*testData).Data
	assert.Equal(t, "33", data)

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

func TestScan(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, err := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	require.NoError(t, err)
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)

	_, err = bkt.Update(ctx, "org$1001:000000000001", &testData{"11"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1001:000000000002", &testData{"12"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1001:000000000003", &testData{"13"})
	require.NoError(t, err)

	_, err = bkt.Update(ctx, "org$1002:000000000001", &testData{"21"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1002:000000000002", &testData{"22"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1002:000000000003", &testData{"23"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1002:000000000004", &testData{"24"})
	require.NoError(t, err)

	_, err = bkt.Update(ctx, "org$1003:000000000001", &testData{"31"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1003:000000000002", &testData{"32"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1003:000000000003", &testData{"33"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1003:000000000004", &testData{"34"})
	require.NoError(t, err)
	_, err = bkt.Update(ctx, "org$1003:000000000005", &testData{"35"})
	require.NoError(t, err)

	result, err := bkt.Scan(ctx, "org$*:000000000006")
	require.NoError(t, err)
	assert.Len(t, result, 0)

	result, err = bkt.Scan(ctx, "org$1001*")
	require.NoError(t, err)
	assert.Len(t, result, 3)

	result, err = bkt.Scan(ctx, "org$1002*")
	require.NoError(t, err)
	assert.Len(t, result, 4)

	result, err = bkt.Scan(ctx, "org$1003*")
	require.NoError(t, err)
	assert.Len(t, result, 5)

	result, err = bkt.Scan(ctx, "org$*:000000000001")
	require.NoError(t, err)
	assert.Len(t, result, 3)

	result, err = bkt.Scan(ctx, "org$*:000000000004")
	require.NoError(t, err)
	assert.Len(t, result, 2)

	result, err = bkt.Scan(ctx, "org$*:000000000005")
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

type testInt struct {
	data int
}

func TestBucketConcurrent(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, err := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	require.NoError(t, err)
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)

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
		time.Sleep(time.Millisecond * 3000)
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
	assert.Len(t, result, 0, "%v docs left, expect 0", len(result))
}

// Test configurable timeout with WithTimeout option
func TestCustomTimeout(t *testing.T) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1, WithTimeout(5*time.Second))
	bkt, err := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	require.NoError(t, err)
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)

	// Should work with custom timeout
	_, err = bkt.Update(ctx, "key1", &testData{"data"})
	require.NoError(t, err)
}

// Test context deadline override
func TestContextDeadlineOverride(t *testing.T) {
	cli := NewClient("localhost:6379", "admin", 1, WithTimeout(10*time.Second))
	bkt, err := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	require.NoError(t, err)
	cli.WithBucket(bkt)
	defer bkt.Clear(context.Background())

	// Use context with very short deadline
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // Ensure context is expired

	// Should fail due to context deadline
	_, err = bkt.Update(ctx, "key1", &testData{"data"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

// Test Redis error handling (no Fatal)
func TestRedisErrorHandling(t *testing.T) {
	ctx := context.Background()
	// Use invalid Redis address
	cli := NewClient("invalid:9999", "admin", 1, WithTimeout(100*time.Millisecond))
	bkt, err := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	require.NoError(t, err)

	// Operations should return errors, not panic with Fatal
	_, err = bkt.Update(ctx, "key1", &testData{"data"})
	assert.Error(t, err, "should fail to connect to invalid Redis")
}

func BenchmarkUpdateData(b *testing.B) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, _ := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)
	defer b.StopTimer()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		key := fmt.Sprintf("%v", n)
		doc, _ := bkt.Update(ctx, key, &testInt{n})
		_ = doc.AddLabels(ctx, []string{key})
	}
	b.StopTimer()
}

func BenchmarkUpdateDataWithTs(b *testing.B) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, _ := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		key := fmt.Sprintf("%v", n)
		doc, _, _ := bkt.UpdateWithTs(ctx, key, &testInt{n}, time.Now())
		_ = doc.AddLabels(ctx, []string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.StopTimer()
}

func BenchmarkFilter1000Label1(b *testing.B) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, _ := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _, _ := bkt.UpdateWithTs(ctx, key, &testInt{i}, time.Now())
		_ = doc.AddLabels(ctx, []string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		results, _ := bkt.Filter(ctx, []string{"label1"})
		if len(results) != 1000 {
			b.Errorf("expect 1000 got %v", len(results))
		}
	}
	b.StopTimer()
}

func BenchmarkFilter1000Label8(b *testing.B) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, _ := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _, _ := bkt.UpdateWithTs(ctx, key, &testInt{i}, time.Now())
		_ = doc.AddLabels(ctx, []string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		results, _ := bkt.Filter(ctx, []string{"label1", "label2"}, []string{"label3", "label4"}, []string{"label5", "label6"}, []string{"label7", "label8"})
		if len(results) != 1000 {
			b.Errorf("expect 1000 got %v", len(results))
		}
	}
	b.StopTimer()
}

func BenchmarkFetchValue(b *testing.B) {
	ctx := context.Background()
	cli := NewClient("localhost:6379", "admin", 1)
	bkt, _ := NewBucket[testData](cli, "TST", coding.NewJsonCoder())
	cli.WithBucket(bkt)
	defer bkt.Clear(ctx)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _ := bkt.Update(ctx, key, &testInt{i})
		_ = doc.AddLabels(ctx, []string{key})
		time.Sleep(time.Millisecond)
	}
	keys := []string{}

	for i := 0; i < 1000; i++ {
		keys = append(keys, fmt.Sprintf("%v", i))
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = bkt.Values(ctx, keys[:1000])
	}
	b.StopTimer()
}
