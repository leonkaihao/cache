package mem

import (
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/leonkaihao/cache/pkg/model"
)

type bucket[T any] struct {
	sync.RWMutex
	name   string
	cli    *client
	docs   map[string]model.CacheDoc  // keys
	labels map[string]map[string]bool // label: docKeys
	errs   []error
}

func NewBucket[T any](cli model.CacheClient, name string) model.CacheBucket {
	scli, _ := cli.(*client)
	return &bucket[T]{
		name:   name,
		cli:    scli,
		docs:   make(map[string]model.CacheDoc),
		labels: make(map[string]map[string]bool),
		errs:   []error{},
	}
}
func (bkt *bucket[T]) Name() string {
	return bkt.name
}

func (bkt *bucket[T]) Docs(keys []string) []model.CacheDoc {
	defer bkt.cleanErrors()
	bkt.RLock()
	defer bkt.RUnlock()
	docs := make([]model.CacheDoc, len(keys))
	for i, key := range keys {
		docs[i] = bkt.docs[key]
	}
	return docs
}

func (bkt *bucket[T]) Values(keys []string) []any {
	defer bkt.cleanErrors()
	bkt.RLock()
	defer bkt.RUnlock()
	values := make([]any, len(keys))
	for i, key := range keys {
		doc := bkt.docs[key]
		if doc != nil {
			values[i] = doc.Val()
		}
	}
	return values
}

// Update directly update the value with incoming data
func (bkt *bucket[T]) Update(key string, data any) model.CacheDoc {
	defer bkt.cleanErrors()
	val, ok := data.(*T)
	if !ok {
		bkt.appendError(fmt.Errorf("Update: update doc key '%v' with an empty value", key))
		return nil
	}
	if key == "" {
		bkt.appendError(fmt.Errorf("Update: update doc %v with an empty key", val))
		return nil
	}
	bkt.Lock()
	defer bkt.Unlock()
	var (
		doc model.CacheDoc
	)
	doc, ok = bkt.docs[key]
	if !ok {
		doc = NewCacheDoc(bkt, key, val)
	} else {
		doc.SetValue(val)
	}
	bkt.docs[key] = doc
	return doc
}

// UpdateWithTs update the data with the latest time, otherwise use existing one.
// if original data don't have time, directly replace it
func (bkt *bucket[T]) UpdateWithTs(key string, data any, ts time.Time) (model.CacheDoc, bool) {
	defer bkt.cleanErrors()
	val, ok := data.(*T)
	if !ok {
		bkt.appendError(fmt.Errorf("UpdateWithTs: update doc key '%v' with an empty value", key))
		return nil, false
	}
	if key == "" {
		bkt.appendError(fmt.Errorf("UpdateWithTs: updated doc %v with an empty key", val))
		return nil, false
	}
	bkt.Lock()
	defer bkt.Unlock()
	var (
		doc     model.CacheDoc
		updated bool
	)
	doc, ok = bkt.docs[key]
	if !ok {
		doc = NewCacheDoc(bkt, key, val).WithTime(ts)
		updated = true
	} else {
		doc, updated = doc.SetValueWithTs(val, ts)
	}
	bkt.docs[key] = doc
	return doc, updated
}

// Filter is a way of filtering data with labels
// it can have multiple label filters
// each filter is a string array, label is the item. all the labels inside a filter has OR logic
// between filters are AND logic
// i.e. Filter([]string{"foo", "bar"}, []string{"new", "bee"}) means data with label foo OR bar, AND new OR bee.
func (bkt *bucket[T]) Filter(filterSteps ...[]string) []string { // result keys
	defer bkt.cleanErrors()
	bkt.RLock()
	defer bkt.RUnlock()
	ret := []string{}
	if len(filterSteps) == 0 {
		for key := range bkt.docs {
			ret = append(ret, key)
		}
		return ret
	}

	result := map[string]bool{} // key:true

	for i, labels := range filterSteps {
		if i == 0 {
			if len(labels) == 0 {
				for key := range bkt.docs {
					result[key] = true
				}
				continue
			}
			for _, label := range labels {
				keys, ok := bkt.labels[label]
				if ok {
					for k := range keys {
						result[k] = true
					}
				}
			}
		} else {
			if len(labels) == 0 {
				continue
			}
			for key := range result {
				doc := bkt.docs[key]
				match := false
				for _, label := range labels {
					_, ok := doc.Labels()[label]
					match = match || ok
				}
				if !match {
					delete(result, key)
				}
			}
		}
	}
	for key := range result {
		ret = append(ret, key)
	}
	return ret
}

// Scan match a key pattern and return all matched keys
// the match logic follows the rule of redis key scan
// The pattern syntax is:
//
//	pattern:
//		{ term }
//	term:
//		'*'         matches any sequence of non-/ characters
//		'?'         matches any single non-/ character
//		'[' [ '^' ] { character-range } ']'
//		            character class (must be non-empty)
//		c           matches character c (c != '*', '?', '\\', '[')
//		'\\' c      matches character c
//
//	character-range:
//		c           matches character c (c != '\\', '-', ']')
//		'\\' c      matches character c
//		lo '-' hi   matches character c for lo <= c <= hi
func (bkt *bucket[T]) Scan(match string) []string {
	result := []string{}
	for k := range bkt.docs {
		if matched, err := path.Match(match, k); err == nil && matched {
			result = append(result, k)
		}
	}
	return result
}

func (bkt *bucket[T]) Clear() {
	defer bkt.cleanErrors()
	bkt.Lock()
	defer bkt.Unlock()
	bkt.docs = make(map[string]model.CacheDoc)
	bkt.labels = make(map[string]map[string]bool)
}

func (bkt *bucket[T]) addLabels(key string, labels []string) {
	bkt.Lock()
	defer bkt.Unlock()
	for _, label := range labels {
		lmap, ok := bkt.labels[label]
		if !ok {
			lmap = map[string]bool{}
			bkt.labels[label] = lmap
		}
		lmap[key] = true
	}
}

func (bkt *bucket[T]) removeLabels(key string, labels []string) {
	bkt.Lock()
	defer bkt.Unlock()
	for _, label := range labels {
		lmap, ok := bkt.labels[label]
		if ok {
			delete(lmap, key)
		}
	}
}

func (bkt *bucket[T]) Remove(keys []string) []model.CacheDoc {
	bkt.Lock()
	defer bkt.Unlock()
	docs := []model.CacheDoc{}
	for _, key := range keys {
		doc, ok := bkt.docs[key]
		docs = append(docs, doc)
		if !ok {
			continue
		}
		for label := range doc.Labels() {
			delete(bkt.labels[label], doc.Key())
		}
		delete(bkt.docs, key)
	}
	return docs
}

func (bkt *bucket[T]) Delete() {
	defer bkt.cleanErrors()
	if bkt.cli != nil {
		bkt.cli.RemoveBucket(bkt.name)
		bkt.cli = nil
	}
}

func (bkt *bucket[T]) GetLastErrors() []error {
	return bkt.errs
}

func (bkt *bucket[T]) appendError(err error) {
	if err == nil {
		return
	}
	bkt.errs = append(bkt.errs, err)
}

func (bkt *bucket[T]) cleanErrors() {
	bkt.errs = []error{}
}
