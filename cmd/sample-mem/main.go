package main

import (
	"context"
	"log"
	"time"

	cache "github.com/leonkaihao/cache/v2/pkg/client/mem"
	"github.com/leonkaihao/cache/v2/pkg/model"
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
	ctx := context.Background()

	// Create client
	cli := cache.NewClient()

	// Create buckets with error handling
	fooBkt1, err := cache.NewBucket[Foo](cli, "foo1")
	if err != nil {
		log.Fatalf("Failed to create foo1 bucket: %v", err)
	}
	cli.WithBucket(fooBkt1)

	barBkt1, err := cache.NewBucket[Bar](cli, "bar1")
	if err != nil {
		log.Fatalf("Failed to create bar1 bucket: %v", err)
	}
	cli.WithBucket(barBkt1)

	fooBkt2, err := cache.NewBucket[Foo](cli, "foo2")
	if err != nil {
		log.Fatalf("Failed to create foo2 bucket: %v", err)
	}
	cli.WithBucket(fooBkt2)

	barBkt2, err := cache.NewBucket[Bar](cli, "bar2")
	if err != nil {
		log.Fatalf("Failed to create bar2 bucket: %v", err)
	}
	cli.WithBucket(barBkt2)

	if err := bucketOperations(ctx, cli); err != nil {
		log.Fatalf("Bucket operations failed: %v", err)
	}

	if err := collectionOperations(ctx, cli); err != nil {
		log.Fatalf("Collection operations failed: %v", err)
	}

	if err := timelineOperations(ctx, cli); err != nil {
		log.Fatalf("Timeline operations failed: %v", err)
	}

	log.Println("All operations completed successfully!")
}

func bucketOperations(ctx context.Context, cli model.CacheClient) error {
	barBkt2 := cli.Bucket("bar2")

	// Fetch existing bucket
	fooBkt1 := cli.Bucket("foo1")
	_ = cli.Bucket("bar1") // return barBkt1

	// Update/insert doc with context
	doc1, err := fooBkt1.Update(ctx, "key1", &Foo{A: true, B: 3, C: "str1"})
	if err != nil {
		return err
	}

	doc2, updated, err := fooBkt1.UpdateWithTs(ctx, "key2", &Foo{A: true, B: 3, C: "str1"}, time.Now())
	if err != nil {
		return err
	}
	if updated {
		// Set expire for 1 sec then delete
		if err := doc2.Expire(time.Second, func(doc model.CacheDoc) {
			_ = doc.Delete(context.Background())
		}); err != nil {
			return err
		}
	}

	doc3, _, err := fooBkt1.UpdateWithTs(ctx, "key3", &Foo{A: true, B: 3, C: "str1"}, time.Now())
	if err != nil {
		return err
	}

	_, err = barBkt2.Update(ctx, "key3", &Foo{A: true, B: 3, C: "str1"})
	if err != nil {
		return err
	}

	// Add labels with error handling
	if err := doc1.AddLabels(ctx, []string{"label1", "label2"}); err != nil {
		return err
	}
	ls1, err := doc1.Labels(ctx)
	if err != nil {
		return err
	}

	if err := doc3.AddLabels(ctx, []string{"label2", "label3"}); err != nil {
		return err
	}

	// Check labels
	_ = ls1.CheckAnd([]string{"label1", "label2"}) // true
	_ = ls1.CheckAnd([]string{"label1", "label3"}) // false
	_ = ls1.CheckOr([]string{"label1", "label3"})  // true
	_ = ls1.CheckOr([]string{"label3", "label4"})  // false

	// Search with label
	_, err = fooBkt1.Keys(ctx, model.FilterOptions{LabelFilter: [][]string{{"label1"}}}) // doc1
	if err != nil {
		return err
	}
	_, err = fooBkt1.Keys(ctx, model.FilterOptions{LabelFilter: [][]string{{"label2"}}}) // doc1, doc3
	if err != nil {
		return err
	}
	_, err = fooBkt1.Keys(ctx, model.FilterOptions{LabelFilter: [][]string{{"label3"}}}) // doc3
	if err != nil {
		return err
	}
	_, err = fooBkt1.Keys(ctx, model.FilterOptions{LabelFilter: [][]string{{"label1", "label3"}}}) // doc1, doc3
	if err != nil {
		return err
	}
	keys1, err := fooBkt1.Keys(ctx, model.FilterOptions{}) // all: doc1, doc3
	if err != nil {
		return err
	}

	// Fetch docs from keys
	docs1, err := fooBkt1.Docs(ctx, keys1)
	if err != nil {
		return err
	}
	if len(docs1) > 0 && docs1[0] != nil {
		_, err = docs1[0].Labels(ctx)
		if err != nil {
			return err
		}
	}

	// Fetch values from keys
	_, err = fooBkt1.Values(ctx, keys1)
	if err != nil {
		return err
	}

	_ = cli.Buckets() // return all available buckets

	// These 2 operations below are equivalent
	cli.RemoveBucket("foo1")
	if err := fooBkt1.Delete(ctx); err != nil {
		return err
	}

	return nil
}

func collectionOperations(ctx context.Context, cli model.CacheClient) error {
	clt1 := cli.Collection("clt1")

	if err := clt1.Add(ctx, "key1", []string{"mem1", "mem2"}); err != nil {
		return err
	}
	if err := clt1.Add(ctx, "key1", []string{"mem2", "mem3"}); err != nil {
		return err
	}
	if err := clt1.Add(ctx, "key2", []string{"mem4", "mem5"}); err != nil {
		return err
	}

	_ = clt1.Name() // return clt1

	keys, err := clt1.Keys(ctx)
	if err != nil {
		return err
	}
	log.Printf("Collection keys: %v\n", keys) // [key1, key2]

	mm, err := clt1.MembersMap(ctx, "key1")
	if err != nil {
		return err
	}
	if mm != nil {
		_ = mm.List()         // ["mem1", "mem2", "mem3"]
		_ = mm.Exists("mem2") // true
	}

	if err := clt1.Remove(ctx, "key1", []string{"mem2"}); err != nil {
		return err
	}

	mm, err = clt1.MembersMap(ctx, "key1")
	if err != nil {
		return err
	}
	if mm != nil {
		_ = mm.Exists("mem2") // false
	}

	if err := clt1.Clear(ctx, "key2"); err != nil {
		return err
	}
	if err := clt1.ClearAll(ctx); err != nil {
		return err
	}

	return nil
}

func timelineOperations(ctx context.Context, cli model.CacheClient) error {
	// Create timeline
	timeline := cli.Timeline("device_states")

	// Set retention policy: keep last 100 updates or 2 hours (config-driven, in-memory)
	timeline.WithOptions(model.TimelineOptions{Retention: model.RetentionPolicy{
		MaxCount:    100,
		MaxDuration: 2 * time.Hour,
		Strategy:    model.RetentionMax,
	}})

	// Record device state at different timestamps
	now := time.Now()

	// Initial state
	if err := timeline.Append(ctx, "device_A", now, map[string]string{
		"zones":   "Z1,Z3",
		"beacons": "B5",
		"battery": "85",
	}, false); err != nil {
		return err
	}

	// Update zones only (sparse update)
	if err := timeline.Append(ctx, "device_A", now.Add(5*time.Minute), map[string]string{
		"zones": "Z1,Z3,Z5",
	}, false); err != nil {
		return err
	}

	// Update battery only
	if err := timeline.Append(ctx, "device_A", now.Add(10*time.Minute), map[string]string{
		"battery": "82",
	}, false); err != nil {
		return err
	}

	// Query current state (merged from all updates)
	latestStates, err := timeline.GetLatest(ctx, []string{"device_A"})
	if err != nil {
		return err
	}
	state := latestStates[0]
	log.Printf("Latest state: zones=%s, beacons=%s, battery=%s\n",
		state["zones"], state["beacons"], state["battery"])
	// Output: zones=Z1,Z3,Z5, beacons=B5, battery=82

	// Query historical state
	historicalStates, err := timeline.GetAt(ctx, []string{"device_A"}, now.Add(7*time.Minute))
	if err != nil {
		return err
	}
	historicalState := historicalStates[0]
	log.Printf("State at +7min: zones=%s, beacons=%s, battery=%s\n",
		historicalState["zones"], historicalState["beacons"], historicalState["battery"])
	// Output: zones=Z1,Z3,Z5, beacons=B5, battery=85

	// Insert out-of-order event
	lateEvent := now.Add(2 * time.Minute)
	if err := timeline.Insert(ctx, "device_A", lateEvent, map[string]string{
		"zones": "Z1,Z2",
	}, false); err != nil {
		return err
	}

	// Find affected states after historical insertion
	affected, err := timeline.GetAffectedRange(ctx, "device_A", lateEvent)
	if err != nil {
		return err
	}
	log.Printf("Affected states after late event: %d states need recomputation\n", len(affected))

	// Get timeline of all states
	allStates, err := timeline.Timeline(ctx, "device_A")
	if err != nil {
		return err
	}
	log.Printf("Total timeline points: %d\n", len(allStates))

	// List all keys in timeline
	keys, err := timeline.Keys(ctx, model.FilterOptions{})
	if err != nil {
		return err
	}
	log.Printf("Timeline keys: %v\n", keys)

	return nil
}
