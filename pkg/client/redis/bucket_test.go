//go:build integration

package redis

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"github.com/leonkaihao/cache/pkg/coding"
	"github.com/leonkaihao/cache/pkg/model"
)

type testData struct {
	Data string `json:"data"`
}

func expectFilter(t *testing.T, bkt model.CacheBucket, filters [][]string, sz int) {
	real := len(bkt.Filter(filters...))
	if real != sz {
		t.Errorf("expect get %v results from filter %v, but got %v", sz, filters, real)
	}
}
func TestBucket(t *testing.T) {
	cli := NewClient("localhost:6379", "admin", 1)
	bkt := cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	defer bkt.Clear()
	bkt.Update("1", &testData{"data"}).AddLabels([]string{"foo", "bar"})
	bkt.Update("2", &testData{"data"}).AddLabels([]string{"bar"})
	bkt.Update("3", &testData{"data"}).AddLabels([]string{"bar"})
	bkt.Update("4", &testData{"data"}).AddLabels([]string{"bar", "foo"})
	bkt.Update("5", &testData{"data"}).AddLabels([]string{"foo"})
	bkt.Update("6", &testData{"data"}).AddLabels([]string{"foo"})
	bkt.Update("7", &testData{"data"}).AddLabels([]string{"foo"})
	expectFilter(t, bkt, [][]string{{}}, 7)               // 1,2,3,4,5,6,7
	expectFilter(t, bkt, nil, 7)                          // 1,2,3,4,5,6,7
	expectFilter(t, bkt, [][]string{nil}, 7)              // 1,2,3,4,5,6,7
	expectFilter(t, bkt, [][]string{{"foo"}}, 5)          // 1,4,5,6,7
	expectFilter(t, bkt, [][]string{{"bar"}}, 4)          // 1,2,3,4
	expectFilter(t, bkt, [][]string{{"foo"}, {"bar"}}, 2) // 1,4
	bkt.Remove([]string{"1", "3", "5"})
	docs := bkt.Docs([]string{"1", "2", "4", "6"})
	if len(docs) != 4 {
		t.Errorf("expect 4 docs but got %v", len(docs))
	}
	if docs[0] != nil {
		t.Error("doc '1' should not exist")
	}
	if docs[3] == nil {
		t.Error("doc '3' should exist")
	}
	expectFilter(t, bkt, [][]string{{"foo"}}, 3)          //4,6,7
	expectFilter(t, bkt, [][]string{{"bar"}}, 2)          //2,4
	expectFilter(t, bkt, [][]string{{"foo"}, {"bar"}}, 1) //4
	expectFilter(t, bkt, [][]string{{"foo", "bar"}}, 4)   //4,6,7,2
	ts := timestamppb.Now().AsTime()
	doc, _ := bkt.UpdateWithTs("7", &testData{"data2"}, ts) // existing key
	if doc.Time() != ts {
		t.Errorf("Key %v expect time %v but got %v", doc.Key(), ts, doc.Time())
	}
	doc, _ = bkt.UpdateWithTs("8888", &testData{"data2"}, ts) // not existing key
	if doc.Time() != ts {
		t.Errorf("Key %v expect time %v but got %v", doc.Key(), ts, doc.Time())
	}
	doc = bkt.Docs([]string{"notexist"})[0]
	if doc != nil {
		t.Errorf("doc expect to be nil but got %v", doc.Key())
	}
	val := bkt.Values([]string{"notexist"})[0]
	if val != nil {
		t.Errorf("value expect to be empty but got %v", val)
	}
}

func TestDoc(t *testing.T) {
	cli := NewClient("localhost:6379", "admin", 1)
	cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	bkt := cli.Bucket("TST")
	defer bkt.Clear()
	doc1 := bkt.Update("1", &testData{"11"})
	doc1.AddLabels([]string{"foo"})
	doc2 := bkt.Update("2", &testData{"22"})
	doc2.AddLabels([]string{"bar", "foo"})
	doc3 := bkt.Update("3", &testData{"33"})
	doc3.AddLabels([]string{"bar", "foo", "new"})
	doc4 := bkt.Update("4", &testData{"44"})
	doc4.AddLabels([]string{"bar", "foo", "new", "bee"})

	values := bkt.Values([]string{"1", "2", "3", "4"})
	if len(values) != 4 {
		t.Errorf("expect 4 items but got %v", len(values))
	}
	data := values[2].(*testData).Data
	if data != "33" {
		t.Errorf("expect value '33' but got %v", data)
	}

	doc1.RemoveLabels([]string{"bar"})           // not allowed
	expectFilter(t, bkt, [][]string{{"bar"}}, 3) // 2,3,4
	doc2.RemoveLabels([]string{"bar"})
	expectFilter(t, bkt, [][]string{{"bar"}}, 2) // 3,4
	doc3.AddLabels([]string{"bar"})
	expectFilter(t, bkt, [][]string{{"bar"}}, 2) // 3,4
	doc4.RemoveLabels([]string{"bar", "foo", "new", "bee"})
	expectFilter(t, bkt, [][]string{{"bar"}, {"foo"}, {"new"}}, 1) // 3
	doc3.Delete()
	expectFilter(t, bkt, [][]string{{"foo"}}, 2) // 1, 2
	ts := timestamppb.Now().AsTime()
	doc4.SetValueWithTs(&testData{"newVal"}, ts)
	if doc4.Time() != ts {
		t.Errorf("Key %v expect time %v but got %v", doc4.Key(), ts, doc4.Time())
	}
	doc1.Expire(time.Second, func(doc model.CacheDoc) { doc.Delete() })
	time.Sleep(2 * time.Second)
	expectFilter(t, bkt, [][]string{{"foo"}}, 1) // 2
}

func TestScan(t *testing.T) {
	cli := NewClient("localhost:6379", "admin", 1)
	bkt := cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	defer bkt.Clear()

	bkt.Update("org$1001:000000000001", &testData{"11"})
	bkt.Update("org$1001:000000000002", &testData{"12"})
	bkt.Update("org$1001:000000000003", &testData{"13"})

	bkt.Update("org$1002:000000000001", &testData{"21"})
	bkt.Update("org$1002:000000000002", &testData{"22"})
	bkt.Update("org$1002:000000000003", &testData{"23"})
	bkt.Update("org$1002:000000000004", &testData{"24"})

	bkt.Update("org$1003:000000000001", &testData{"31"})
	bkt.Update("org$1003:000000000002", &testData{"32"})
	bkt.Update("org$1003:000000000003", &testData{"33"})
	bkt.Update("org$1003:000000000004", &testData{"34"})
	bkt.Update("org$1003:000000000005", &testData{"35"})

	result := bkt.Scan("org$*:000000000006")
	if len(result) != 0 {
		t.Errorf("expect 0 results but got %v(%v)", len(result), result)
	}

	result = bkt.Scan("org$1001*")
	if len(result) != 3 {
		t.Errorf("expect 3 results but got %v(%v)", len(result), result)
	}
	result = bkt.Scan("org$1002*")
	if len(result) != 4 {
		t.Errorf("expect 4 results but got %v(%v)", len(result), result)
	}
	result = bkt.Scan("org$1003*")
	if len(result) != 5 {
		t.Errorf("expect 5 results but got %v(%v)", len(result), result)
	}

	result = bkt.Scan("org$*:000000000001")
	if len(result) != 3 {
		t.Errorf("expect 3 results but got %v(%v)", len(result), result)
	}

	result = bkt.Scan("org$*:000000000004")
	if len(result) != 2 {
		t.Errorf("expect 2 results but got %v(%v)", len(result), result)
	}

	result = bkt.Scan("org$*:000000000005")
	if len(result) != 1 {
		t.Errorf("expect 1 results but got %v(%v)", len(result), result)
	}
}

type testInt struct {
	data int
}

func TestBucketConcurrent(t *testing.T) {
	cli := NewClient("localhost:6379", "admin", 1)
	cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	bkt := cli.Bucket("TST")
	defer bkt.Clear()
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("%v", i)
			doc := bkt.Update(key, &testInt{i})
			doc.AddLabels([]string{key})
			time.Sleep(time.Millisecond)
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond * 3000)
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("%v", i)
			bkt.Remove([]string{key})
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()
	sz := len(bkt.Filter(nil))
	if sz != 0 {
		t.Errorf("%v docs left, expect 0", sz)
	}
}

func BenchmarkUpdateData(b *testing.B) {
	cli := NewClient("localhost:6379", "admin", 1)
	cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	bkt := cli.Bucket("TST")
	defer bkt.Clear()
	defer b.StopTimer()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		key := fmt.Sprintf("%v", n)
		doc := bkt.Update(key, &testInt{n})
		doc.AddLabels([]string{key})
	}
	b.StopTimer()
}

func BenchmarkUpdateDataWithTs(b *testing.B) {
	cli := NewClient("localhost:6379", "admin", 1)
	cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	bkt := cli.Bucket("TST")
	defer bkt.Clear()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		key := fmt.Sprintf("%v", n)
		doc, _ := bkt.UpdateWithTs(key, &testInt{n}, time.Now())
		doc.AddLabels([]string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.StopTimer()
}

func BenchmarkFilter1000Label1(b *testing.B) {
	cli := NewClient("localhost:6379", "admin", 1)
	cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	bkt := cli.Bucket("TST")
	defer bkt.Clear()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _ := bkt.UpdateWithTs(key, &testInt{i}, time.Now())
		doc.AddLabels([]string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		results := bkt.Filter([]string{"label1"})
		if len(results) != 1000 {
			b.Errorf("expect 1000 got %v", len(results))
		}
	}
	b.StopTimer()
}

func BenchmarkFilter1000Label8(b *testing.B) {
	cli := NewClient("localhost:6379", "admin", 1)
	cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	bkt := cli.Bucket("TST")
	defer bkt.Clear()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _ := bkt.UpdateWithTs(key, &testInt{i}, time.Now())
		doc.AddLabels([]string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		results := bkt.Filter([]string{"label1", "label2"}, []string{"label3", "label4"}, []string{"label5", "label6"}, []string{"label7", "label8"})
		if len(results) != 1000 {
			b.Errorf("expect 1000 got %v", len(results))
		}
	}
	b.StopTimer()
}

func BenchmarkFetchValue(b *testing.B) {
	cli := NewClient("localhost:6379", "admin", 1)
	cli.WithBucket(NewBucket[testData](cli, "TST", coding.NewJsonCoder()))
	bkt := cli.Bucket("TST")
	defer bkt.Clear()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc := bkt.Update(key, &testInt{i})
		doc.AddLabels([]string{key})
		time.Sleep(time.Millisecond)
	}
	keys := []string{}

	for i := 0; i < 1000; i++ {
		keys = append(keys, fmt.Sprintf("%v", i))
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		bkt.Values(keys[:1000])
	}
	b.StopTimer()
}
