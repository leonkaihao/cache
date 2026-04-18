package main

import (
	"time"

	cache "github.com/leonkaihao/cache/pkg/client/mem"
	"github.com/leonkaihao/cache/pkg/model"
)

type Foo struct {
	A bool   `json:"a"`
	B int    `json:"b"`
	C string `json:"c"`
}

type Bar struct {
	D bool   `json:"d"`
	E int    `json:"e"`
	F string `json:"f"`
}

func main() {
	// create client
	cli := cache.NewClient()
	// create bucket
	cli.WithBucket(cache.NewBucket[Foo](cli, "foo1")) // return fooBkt1
	cli.WithBucket(cache.NewBucket[Bar](cli, "bar1")) // return barBkt1
	cli.WithBucket(cache.NewBucket[Foo](cli, "foo2")) // return fooBkt2
	cli.WithBucket(cache.NewBucket[Bar](cli, "bar2")) // return barBkt2
	bucketOperations(cli)
	collectionOperations(cli)
}

func bucketOperations(cli model.CacheClient) {

	barBkt2 := cli.Bucket("bar2")

	// fetch existing bucket
	fooBkt1 := cli.Bucket("foo1")
	cli.Bucket("bar1") // return barBkt1

	// update/insert doc
	doc1 := fooBkt1.Update("key1", &Foo{A: true, B: 3, C: "str1"}) // update with the object only
	doc2, updated := fooBkt1.UpdateWithTs("key2", &Foo{A: true, B: 3, C: "str1"}, time.Now())
	if updated {
		// set expire for 1 sec then delete
		doc2.Expire(time.Second, func(doc model.CacheDoc) {
			doc2.Delete()
		})
	}
	doc3, _ := fooBkt1.UpdateWithTs("key3", &Foo{A: true, B: 3, C: "str1"}, time.Now()) // update based on timeline
	barBkt2.Update("key3", &Foo{A: true, B: 3, C: "str1"})                              // return doc4

	// add labels
	ls1 := doc1.AddLabels([]string{"label1", "label2"})
	doc3.AddLabels([]string{"label2", "label3"})

	// check labels
	ls1.CheckAnd([]string{"label1", "label2"}) // true
	ls1.CheckAnd([]string{"label1", "label3"}) // false
	ls1.CheckOr([]string{"label1", "label3"})  // true
	ls1.CheckOr([]string{"label3", "label4"})  // false

	// search with label
	fooBkt1.Filter([]string{"label1"})           // doc1
	fooBkt1.Filter([]string{"label2"})           // doc1, doc3
	fooBkt1.Filter([]string{"label3"})           // doc3
	fooBkt1.Filter([]string{"label1", "label3"}) // doc1, doc3
	keys1 := fooBkt1.Filter([]string{})          // all: doc1, doc3

	// fetch docs from keys
	// docs have the same size and indexes with keys
	// any doc that is not found will be null in the same index with the key
	docs1 := fooBkt1.Docs(keys1) // return CacheDoc type
	docs1[0].Labels()            // map[string]bool{label1, label2} from doc1

	// fetch values from keys
	// values have the same size and indexes with keys
	// any value that is not found will be null in the same index with the key
	fooBkt1.Values(keys1) // return actual values of *Foo type

	cli.Buckets() // return all available buckets

	// these 2 operations below are the same
	cli.RemoveBucket("foo1")
	fooBkt1.Delete()

}

func collectionOperations(cli model.CacheClient) {
	clt1 := cli.Collection("clt1")
	clt1.Add("key1", []string{"mem1", "mem2"})
	clt1.Add("key1", []string{"mem2", "mem3"})
	clt1.Add("key2", []string{"mem4", "mem5"})
	clt1.Name()                            // return clt1
	clt1.Keys()                            // return [key1, key2]
	clt1.MembersMap("key1").List()         // return ["mem1", "mem2", "mem3"]
	clt1.MembersMap("key1").Exists("mem2") // return true
	clt1.Remove("key1", []string{"mem2"})
	clt1.MembersMap("key1").Exists("mem2") // return false

	clt1.Clear("key2") // key2 and its members will be removed from collection
	clt1.ClearAll()    // collection is empty, without any key
}
