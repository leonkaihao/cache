package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
	goredis "github.com/redis/go-redis/v9"
)

const (
	CACHEDOC_KEY    = "key"
	CACHEDOC_VAL    = "val"
	CACHEDOC_LABELS = "labels"
	CACHEDOC_TS     = "ts"
)

type cacheDoc[T any] struct {
	sync.RWMutex
	bucket  *bucket[T]
	key     string
	expirer *time.Timer
}

func NewCacheDoc[T any](bucket *bucket[T], key string) *cacheDoc[T] {
	return &cacheDoc[T]{
		bucket: bucket,
		key:    key,
	}
}

func (doc *cacheDoc[T]) Key() string {
	return doc.key
}

func (doc *cacheDoc[T]) Val(ctx context.Context) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)

	valStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_VAL).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil, fmt.Errorf("document not found")
		}
		return nil, fmt.Errorf("fail to get value of cache doc %v: %w", newKey, err)
	}

	data := new(T)
	if err := doc.bucket.coder.Decode(valStr, data); err != nil {
		return nil, fmt.Errorf("fail to decode value of cache doc %v: %w", newKey, err)
	}

	return data, nil
}

func (doc *cacheDoc[T]) saveInBucket(ctx context.Context) error {
	redisCli := doc.bucket.cli.getRedisCli()
	if err := redisCli.SAdd(ctx, formatBucketKeys(doc.bucket), doc.key).Err(); err != nil {
		return fmt.Errorf("fail to save doc in bucket: %w", err)
	}
	return nil
}

func (doc *cacheDoc[T]) SetValue(ctx context.Context, val any) error {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)

	data, err := doc.bucket.coder.Encode(val)
	if err != nil {
		return fmt.Errorf("fail to encode value: %w", err)
	}

	if err := redisCli.HSet(ctx, newKey, CACHEDOC_VAL, string(data)).Err(); err != nil {
		return fmt.Errorf("fail to set value of cache doc %v: %w", newKey, err)
	}

	if err := doc.saveInBucket(ctx); err != nil {
		return err
	}

	return nil
}

func (doc *cacheDoc[T]) SetValueWithTs(ctx context.Context, val any, ts time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)

	data, err := doc.bucket.coder.Encode(val)
	if err != nil {
		return false, fmt.Errorf("fail to encode value: %w", err)
	}

	// Check existing timestamp
	tsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_TS).Result()
	if err != nil && err != goredis.Nil {
		return false, fmt.Errorf("fail to get timestamp: %w", err)
	}

	if err != goredis.Nil { // Has existing timestamp
		preTs, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			return false, fmt.Errorf("fail to parse timestamp: %w", err)
		}
		if !ts.After(preTs) {
			Logger.Debug("SetValueWithTs: not updated, incoming time not after current time",
				"key", newKey, "incoming", ts, "current", preTs)
			return false, nil
		}
	}

	// Update both fields atomically
	pipe := redisCli.Pipeline()
	pipe.HSet(ctx, newKey, CACHEDOC_TS, ts.UTC().Format(time.RFC3339Nano))
	pipe.HSet(ctx, newKey, CACHEDOC_VAL, string(data))
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("fail to update doc: %w", err)
	}

	if err := doc.saveInBucket(ctx); err != nil {
		return false, err
	}

	return true, nil
}

func (doc *cacheDoc[T]) WithTime(ctx context.Context, ts time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)

	if err := redisCli.HSet(ctx, newKey, CACHEDOC_TS, ts.UTC().Format(time.RFC3339Nano)).Err(); err != nil {
		return fmt.Errorf("fail to set timestamp of cache doc %v: %w", newKey, err)
	}

	return nil
}

func (doc *cacheDoc[T]) Time(ctx context.Context) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)

	tsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_TS).Result()
	if err != nil {
		if err == goredis.Nil {
			return time.Time{}, fmt.Errorf("timestamp not found for doc %v", newKey)
		}
		return time.Time{}, fmt.Errorf("fail to get timestamp of cache doc %v: %w", newKey, err)
	}

	preTs, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("fail to parse timestamp of cache doc %v: %w", newKey, err)
	}

	return preTs, nil
}

func (doc *cacheDoc[T]) Labels(ctx context.Context) (model.LabelSet, error) {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	redisCli := doc.bucket.cli.getRedisCli()
	ret := model.LabelSet{}
	newKey := formatDocKey(doc.bucket, doc.key)

	labelsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_LABELS).Result()
	if err != nil {
		if err == goredis.Nil {
			return model.LabelSet{}, nil // No labels is not an error
		}
		return nil, fmt.Errorf("fail to get labels of cache doc %v: %w", newKey, err)
	}

	return ret.FromStr(labelsStr), nil
}

func (doc *cacheDoc[T]) AddLabels(ctx context.Context, labels []string) error {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	redisCli := doc.bucket.cli.getRedisCli()
	labelSet := model.LabelSet{}
	newKey := formatDocKey(doc.bucket, doc.key)

	// Get existing labels
	labelsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_LABELS).Result()
	if err == nil {
		labelSet = labelSet.FromStr(labelsStr)
	} else if err != goredis.Nil {
		return fmt.Errorf("fail to get existing labels: %w", err)
	}

	// Add new labels
	for _, label := range labels {
		if label == "" {
			continue
		}
		newLabel := formatLabel(doc.bucket, label)
		if err := redisCli.SAdd(ctx, newLabel, doc.key).Err(); err != nil {
			return fmt.Errorf("fail to add label %v: %w", label, err)
		}
		labelSet[label] = true
		if err := redisCli.SAdd(ctx, formatBucketLabels(doc.bucket), label).Err(); err != nil {
			return fmt.Errorf("fail to track label %v: %w", label, err)
		}
	}

	if err := redisCli.HSet(ctx, newKey, CACHEDOC_LABELS, labelSet.Format()).Err(); err != nil {
		return fmt.Errorf("fail to set labels of cache doc %v: %w", newKey, err)
	}

	return nil
}

func (doc *cacheDoc[T]) RemoveLabels(ctx context.Context, labels []string) error {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	redisCli := doc.bucket.cli.getRedisCli()
	labelSet := model.LabelSet{}
	newKey := formatDocKey(doc.bucket, doc.key)

	labelsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_LABELS).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil // No labels to remove, not an error
		}
		return fmt.Errorf("fail to get labels for removal: %w", err)
	}

	labelSet = labelSet.FromStr(labelsStr)
	for _, label := range labels {
		if label == "" {
			continue
		}
		newLabel := formatLabel(doc.bucket, label)
		if err := redisCli.SRem(ctx, newLabel, doc.key).Err(); err != nil {
			return fmt.Errorf("fail to remove label %v: %w", label, err)
		}
		delete(labelSet, label)
	}

	if err := redisCli.HSet(ctx, newKey, CACHEDOC_LABELS, labelSet.Format()).Err(); err != nil {
		return fmt.Errorf("fail to update labels of cache doc %v: %w", newKey, err)
	}

	return nil
}

func (doc *cacheDoc[T]) Delete(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, doc.bucket.timeout)
	defer cancel()

	doc.Lock()
	if doc.expirer != nil {
		doc.expirer.Stop()
		doc.expirer = nil
	}
	doc.Unlock()

	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)

	// Get labels for cleanup
	labelSet := model.LabelSet{}
	labelsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_LABELS).Result()
	if err == nil {
		labelSet = labelSet.FromStr(labelsStr)
	} else if err != goredis.Nil {
		return fmt.Errorf("fail to get labels for deletion: %w", err)
	}

	// Clean up label indices
	for label := range labelSet {
		if err := redisCli.SRem(ctx, formatLabel(doc.bucket, label), doc.key).Err(); err != nil {
			return fmt.Errorf("fail to remove from label %v: %w", label, err)
		}
	}

	// Remove from bucket keys
	if err := redisCli.SRem(ctx, formatBucketKeys(doc.bucket), doc.key).Err(); err != nil {
		return fmt.Errorf("fail to remove from bucket keys: %w", err)
	}

	// Delete the document
	if err := redisCli.Del(ctx, newKey).Err(); err != nil {
		return fmt.Errorf("fail to delete doc %v: %w", newKey, err)
	}

	return nil
}

func (doc *cacheDoc[T]) Expire(d time.Duration, onExpire func(model.CacheDoc)) error {
	doc.Lock()
	defer doc.Unlock()

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
