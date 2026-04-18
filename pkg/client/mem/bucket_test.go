package mem

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
	"github.com/leonkaihao/cache/pkg/model"
)

type testData struct {
	data string
}

func expectFilter(t *testing.T, bkt model.CacheBucket, filters [][]string, sz int) {
	real := len(bkt.Filter(filters...))
	if real != sz {
		t.Errorf("expect get %v results from filter %v, but got %v", sz, filters, real)
	}
}
func TestBucket(t *testing.T) {
	bkt := NewBucket[testData](nil, "111")
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
	bkt := NewBucket[testData](nil, "111")
	doc1 := bkt.Update("1", &testData{"data"})
	doc1.AddLabels([]string{"foo"})
	doc2 := bkt.Update("2", &testData{"data"})
	doc2.AddLabels([]string{"bar", "foo"})
	doc3 := bkt.Update("3", &testData{"data"})
	doc3.AddLabels([]string{"bar", "foo", "new"})
	doc4 := bkt.Update("4", &testData{"data"})
	doc4.AddLabels([]string{"bar", "foo", "new", "bee"})
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

type testInt struct {
	data int
}

func TestBucketConcurrent(t *testing.T) {
	bkt := NewBucket[testInt](nil, "TST")
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
		time.Sleep(time.Millisecond * 300)
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

func Test_bucket_Scan(t *testing.T) {
	bkt := NewBucket[testData](nil, "TST")
	bkt.Update("foo", &testData{"foo"})
	bkt.Update("bar", &testData{"bar"})
	bkt.Update("foobar", &testData{"foobar"})
	bkt.Update("foo:bar", &testData{"foo:bar"})
	bkt.Update("foo@bar", &testData{"foo@bar"})
	bkt.Update("foo$bar", &testData{"foo$bar"})
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
		result := bkt.Scan(item.expression)
		assert.Equal(t, item.expect, len(result))
	}
}

func BenchmarkUpdateData1000(b *testing.B) {
	bkt := NewBucket[testInt](nil, "TST")
	defer bkt.Clear()
	defer b.StopTimer()
	b.ResetTimer()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc := bkt.Update(key, &testInt{i})
		doc.AddLabels([]string{key})
	}
}

func BenchmarkUpdateDataWithTs1000(b *testing.B) {
	bkt := NewBucket[testInt](nil, "TST")
	defer bkt.Clear()
	defer b.StopTimer()
	b.ResetTimer()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _ := bkt.UpdateWithTs(key, &testInt{i}, time.Now())
		doc.AddLabels([]string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
}

func BenchmarkFilter1000Label1(b *testing.B) {
	bkt := NewBucket[testInt](nil, "TST")
	defer bkt.Clear()
	defer b.StopTimer()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _ := bkt.UpdateWithTs(key, &testInt{i}, time.Now())
		doc.AddLabels([]string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.ResetTimer()
	results := bkt.Filter([]string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	if len(results) != 1000 {
		b.Errorf("expect 1000 got %v", len(results))
	}
}

func BenchmarkFilter1000Label8(b *testing.B) {
	bkt := NewBucket[testInt](nil, "TST")
	defer bkt.Clear()
	defer b.StopTimer()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%v", i)
		doc, _ := bkt.UpdateWithTs(key, &testInt{i}, time.Now())
		doc.AddLabels([]string{"label1", "label2", "label3", "label4", "label5", "label6", "label7", "label8"})
	}
	b.ResetTimer()
	results := bkt.Filter([]string{"label1", "label2"}, []string{"label3", "label4"}, []string{"label5", "label6"}, []string{"label7", "label8"})
	if len(results) != 1000 {
		b.Errorf("expect 1000 got %v", len(results))
	}
}
