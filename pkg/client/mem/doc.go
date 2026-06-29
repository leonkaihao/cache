package mem

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
)

type cacheDoc[T any] struct {
	sync.RWMutex
	bucket  *bucket[T]
	labels  map[string]bool
	key     string
	val     any
	ts      time.Time
	expirer *time.Timer
}

func NewCacheDoc[T any](bucket *bucket[T], key string, val *T) *cacheDoc[T] {
	return &cacheDoc[T]{
		bucket: bucket,
		key:    key,
		val:    val,
		labels: make(map[string]bool),
	}
}

func (doc *cacheDoc[T]) Key() string {
	return doc.key
}

func (doc *cacheDoc[T]) Val(ctx context.Context) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	doc.RLock()
	defer doc.RUnlock()

	if doc.bucket == nil {
		return nil, fmt.Errorf("document has been deleted")
	}

	return doc.val, nil
}

func (doc *cacheDoc[T]) SetValue(ctx context.Context, val any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	doc.Lock()
	defer doc.Unlock()

	if doc.bucket == nil {
		return fmt.Errorf("document has been deleted")
	}

	doc.val = val
	return nil
}

func (doc *cacheDoc[T]) SetValueWithTs(ctx context.Context, val any, ts time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	doc.Lock()
	defer doc.Unlock()

	if doc.bucket == nil {
		return false, fmt.Errorf("document has been deleted")
	}

	if !ts.After(doc.ts) {
		Logger.Debug("SetValueWithTs: not updated, incoming time not after current time",
			"key", doc.key, "incoming", ts, "current", doc.ts)
		return false, nil
	}

	doc.ts = ts.UTC()
	doc.val = val
	return true, nil
}

func (doc *cacheDoc[T]) WithTime(ctx context.Context, tm time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	doc.Lock()
	defer doc.Unlock()

	if doc.bucket == nil {
		return fmt.Errorf("document has been deleted")
	}

	doc.ts = tm.UTC()
	return nil
}

func (doc *cacheDoc[T]) Time(ctx context.Context) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}

	doc.RLock()
	defer doc.RUnlock()

	if doc.bucket == nil {
		return time.Time{}, fmt.Errorf("document has been deleted")
	}

	return doc.ts, nil
}

func (doc *cacheDoc[T]) Labels(ctx context.Context) (model.LabelSet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	doc.RLock()
	defer doc.RUnlock()

	if doc.bucket == nil {
		return nil, fmt.Errorf("document has been deleted")
	}

	return model.LabelSet(doc.labels).Copy(), nil
}

func (doc *cacheDoc[T]) AddLabels(ctx context.Context, labelsOrig []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	doc.Lock()
	if doc.bucket == nil {
		doc.Unlock()
		return fmt.Errorf("cannot add labels to deleted document")
	}

	validLabels := make([]string, 0, len(labelsOrig))
	for _, label := range labelsOrig {
		if label == "" {
			continue
		}
		doc.labels[label] = true
		validLabels = append(validLabels, label)
	}
	// Copy bucket pointer under lock before releasing to prevent TOCTOU race with Delete().
	// The bucket has its own synchronization, so using the copy after unlock is safe.
	bucket := doc.bucket
	doc.Unlock()

	if bucket != nil && len(validLabels) > 0 {
		bucket.addLabels(doc.key, validLabels)
	}

	return nil
}

func (doc *cacheDoc[T]) RemoveLabels(ctx context.Context, labelsOrig []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	doc.Lock()
	if doc.bucket == nil {
		doc.Unlock()
		return fmt.Errorf("cannot remove labels from deleted document")
	}

	validLabels := make([]string, 0, len(labelsOrig))
	for _, label := range labelsOrig {
		if label == "" {
			continue
		}
		delete(doc.labels, label)
		validLabels = append(validLabels, label)
	}
	// Copy bucket pointer under lock before releasing to prevent TOCTOU race with Delete().
	// The bucket has its own synchronization, so using the copy after unlock is safe.
	bucket := doc.bucket
	doc.Unlock()

	if bucket != nil && len(validLabels) > 0 {
		bucket.removeLabels(doc.key, validLabels)
	}

	return nil
}

func (doc *cacheDoc[T]) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	doc.Lock()

	// Stop expiration timer
	if doc.expirer != nil {
		doc.expirer.Stop()
		doc.expirer = nil
	}

	if doc.bucket == nil {
		doc.Unlock()
		return fmt.Errorf("document already deleted")
	}

	// Collect labels while holding lock
	labels := make([]string, 0, len(doc.labels))
	for k := range doc.labels {
		labels = append(labels, k)
	}

	bucket := doc.bucket
	doc.bucket = nil // Mark as deleted BEFORE unlock
	doc.Unlock()

	// Clean up in bucket (has its own locks)
	if len(labels) > 0 {
		bucket.removeLabels(doc.key, labels)
	}
	return bucket.Remove(ctx, []string{doc.key})
}

func (doc *cacheDoc[T]) Expire(d time.Duration, onExpire func(model.CacheDoc)) error {
	doc.Lock()
	defer doc.Unlock()

	if doc.bucket == nil {
		return fmt.Errorf("cannot set expiration on deleted document")
	}

	// Stop existing timer if any
	if doc.expirer != nil {
		doc.expirer.Stop()
	}

	doc.expirer = time.AfterFunc(d, func() {
		if onExpire != nil {
			onExpire(doc)
		}
		doc.Lock()
		doc.expirer = nil
		doc.Unlock()
	})

	return nil
}

func (doc *cacheDoc[T]) CancelExpire() error {
	doc.Lock()
	defer doc.Unlock()

	if doc.expirer != nil {
		doc.expirer.Stop()
		doc.expirer = nil
	}

	return nil
}
