package mem

import (
	"context"
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
)

type bucket[T any] struct {
	sync.RWMutex
	name   string
	cli    *client
	docs   map[string]model.CacheDoc  // keys
	labels map[string]map[string]bool // label: docKeys
}

func NewBucket[T any](cli model.CacheClient, name string) (model.CacheBucket, error) {
	scli, ok := cli.(*client)
	if !ok {
		return nil, fmt.Errorf("expected *mem.client, got %T", cli)
	}
	return &bucket[T]{
		name:   name,
		cli:    scli,
		docs:   make(map[string]model.CacheDoc),
		labels: make(map[string]map[string]bool),
	}, nil
}

func (bkt *bucket[T]) Name() string {
	return bkt.name
}

func (bkt *bucket[T]) Docs(ctx context.Context, keys []string) ([]model.CacheDoc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bkt.RLock()
	defer bkt.RUnlock()

	docs := make([]model.CacheDoc, len(keys))
	for i, key := range keys {
		docs[i] = bkt.docs[key] // nil if not exists
	}
	return docs, nil
}

func (bkt *bucket[T]) Values(ctx context.Context, keys []string) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bkt.RLock()
	defer bkt.RUnlock()

	values := make([]any, len(keys))
	for i, key := range keys {
		if doc := bkt.docs[key]; doc != nil {
			val, _ := doc.Val(ctx) // Ignore inner error in batch operation
			values[i] = val
		}
	}
	return values, nil
}

// Update directly update the value with incoming data
func (bkt *bucket[T]) Update(ctx context.Context, key string, data any) (model.CacheDoc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	val, ok := data.(*T)
	if !ok {
		return nil, fmt.Errorf("invalid data type: expected *%T, got %T", new(T), data)
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	bkt.Lock()
	defer bkt.Unlock()

	doc, ok := bkt.docs[key]
	if !ok {
		doc = NewCacheDoc(bkt, key, val)
		bkt.docs[key] = doc
	} else {
		if err := doc.SetValue(ctx, val); err != nil {
			return nil, fmt.Errorf("failed to set value: %w", err)
		}
	}

	return doc, nil
}

// UpdateWithTs update the data with the latest time, otherwise use existing one.
// if original data don't have time, directly replace it
func (bkt *bucket[T]) UpdateWithTs(ctx context.Context, key string, data any, ts time.Time) (model.CacheDoc, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	val, ok := data.(*T)
	if !ok {
		return nil, false, fmt.Errorf("invalid data type: expected *%T, got %T", new(T), data)
	}

	if key == "" {
		return nil, false, fmt.Errorf("key cannot be empty")
	}

	bkt.Lock()
	defer bkt.Unlock()

	doc, exists := bkt.docs[key]
	if !exists {
		doc = NewCacheDoc(bkt, key, val)
		if err := doc.WithTime(ctx, ts); err != nil {
			return nil, false, fmt.Errorf("failed to set time: %w", err)
		}
		bkt.docs[key] = doc
		return doc, true, nil
	}

	updated, err := doc.SetValueWithTs(ctx, val, ts)
	if err != nil {
		return nil, false, fmt.Errorf("failed to update with timestamp: %w", err)
	}

	return doc, updated, nil
}

// Keys returns all keys in the bucket, optionally filtered by labels.
// Supports label-based filtering with OR within each slice and AND between slices.
// Example:
//   Keys(ctx, FilterOptions{}) - returns all keys
//   Keys(ctx, FilterOptions{LabelFilter: {{"foo", "bar"}}}) - keys with label foo OR bar
//   Keys(ctx, FilterOptions{LabelFilter: {{"foo"}, {"bar"}}}) - keys with label foo AND bar
func (bkt *bucket[T]) Keys(ctx context.Context, opt model.FilterOptions) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bkt.RLock()
	defer bkt.RUnlock()

	filterSteps := opt.LabelFilter

	// Empty filter = return all keys
	if len(filterSteps) == 0 {
		keys := make([]string, 0, len(bkt.docs))
		for key := range bkt.docs {
			keys = append(keys, key)
		}
		return keys, nil
	}

	result := map[string]bool{}

	for i, labels := range filterSteps {
		if i == 0 {
			// First step: OR logic within labels
			if len(labels) == 0 {
				// Empty first step = all keys
				for key := range bkt.docs {
					result[key] = true
				}
				continue
			}

			for _, label := range labels {
				if keys, ok := bkt.labels[label]; ok {
					for k := range keys {
						result[k] = true
					}
				}
			}
		} else {
			// Subsequent steps: AND with previous result
			if len(labels) == 0 {
				// Empty step = no filtering
				continue
			}

			for key := range result {
				doc := bkt.docs[key]
				docLabels, _ := doc.Labels(ctx) // Ignore error in filter
				if !docLabels.CheckOr(labels) {
					delete(result, key)
				}
			}
		}
	}

	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}

	return keys, nil
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
func (bkt *bucket[T]) Scan(ctx context.Context, match string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bkt.RLock()
	defer bkt.RUnlock()

	result := []string{}
	for k := range bkt.docs {
		matched, err := path.Match(match, k)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", match, err)
		}
		if matched {
			result = append(result, k)
		}
	}

	return result, nil
}

func (bkt *bucket[T]) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	bkt.Lock()
	oldDocs := bkt.docs
	bkt.docs = make(map[string]model.CacheDoc)
	bkt.labels = make(map[string]map[string]bool)
	bkt.Unlock()

	// Stop all expiration timers outside the lock
	for _, doc := range oldDocs {
		if cdoc, ok := doc.(*cacheDoc[T]); ok {
			_ = cdoc.CancelExpire() // Stop timer
			cdoc.Lock()
			cdoc.bucket = nil // Mark as deleted
			cdoc.Unlock()
		}
	}

	return nil
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

func (bkt *bucket[T]) Remove(ctx context.Context, keys []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	bkt.Lock()
	defer bkt.Unlock()

	for _, key := range keys {
		doc, ok := bkt.docs[key]
		if !ok {
			continue
		}

		// Clean up labels
		docLabels, _ := doc.Labels(ctx) // Ignore error during cleanup
		for label := range docLabels {
			if labelMap, exists := bkt.labels[label]; exists {
				delete(labelMap, key)
			}
		}

		delete(bkt.docs, key)
	}

	return nil
}

func (bkt *bucket[T]) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := bkt.Clear(ctx); err != nil {
		return fmt.Errorf("failed to clear bucket: %w", err)
	}

	if bkt.cli != nil {
		bkt.cli.RemoveBucket(bkt.name)
		bkt.cli = nil
	}

	return nil
}
