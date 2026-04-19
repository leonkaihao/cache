package redis

import (
	"context"
	"time"

	"github.com/leonkaihao/cache/pkg/model"
)

const (
	CACHEDOC_KEY    = "key"
	CACHEDOC_VAL    = "val"
	CACHEDOC_LABELS = "labels"
	CACHEDOC_TS     = "ts"
)

type cacheDoc[T any] struct {
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

func (doc *cacheDoc[T]) Val() any {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)
	valStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_VAL).Result()
	if err != nil {
		Logger.Fatal("fail to get value of cache doc", "key", newKey, "error", err)
	}
	data := new(T)
	err = doc.bucket.coder.Decode(valStr, data)
	if err != nil {
		Logger.Fatal("fail to unmarshal value of cache doc", "key", newKey, "error", err)
	}
	return data
}

func (doc *cacheDoc[T]) saveInBucket(ctx context.Context) {
	redisCli := doc.bucket.cli.getRedisCli()
	redisCli.SAdd(ctx, formatBucketKeys(doc.bucket), doc.key)
}

func (doc *cacheDoc[T]) SetValue(val any) model.CacheDoc {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)
	data, err := doc.bucket.coder.Encode(val)
	if err != nil {
		Logger.Fatal("fail to marshal value of cache doc", "key", newKey, "error", err)
	}
	err = redisCli.HSet(ctx, newKey, CACHEDOC_VAL, string(data)).Err()
	if err != nil {
		Logger.Fatal("fail to set value of cache doc", "key", newKey, "error", err)
	}
	doc.saveInBucket(ctx)
	return doc
}

func (doc *cacheDoc[T]) SetValueWithTs(val any, ts time.Time) (model.CacheDoc, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)
	data, err := doc.bucket.coder.Encode(val)
	if err != nil {
		Logger.Fatal("fail to marshal value of cache doc", "key", newKey, "error", err)
	}
	var preTs time.Time
	tsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_TS).Result()
	if err != nil {
		// no ts in the doc, directly assign
		goto assign_value
	}
	preTs, err = time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		Logger.Fatal("fail to parse ts of cache doc", "key", newKey, "error", err)
	}

	if !ts.After(preTs) {
		Logger.Debug("SetValueWithTs: not update because incoming time is before current time", "key", newKey, "incoming_time", ts, "current_time", preTs)
		return nil, false
	}

assign_value:
	err = redisCli.HSet(ctx, newKey, CACHEDOC_TS, ts.Format(time.RFC3339Nano)).Err()
	if err != nil {
		Logger.Fatal("fail to set ts of cache doc", "key", newKey, "error", err)
	}
	redisCli.HSet(ctx, newKey, CACHEDOC_VAL, string(data))
	doc.saveInBucket(ctx)
	return doc, true
}

func (doc *cacheDoc[T]) WithTime(ts time.Time) model.CacheDoc {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)
	err := redisCli.HSet(ctx, newKey, CACHEDOC_TS, ts.Format(time.RFC3339Nano)).Err()
	if err != nil {
		Logger.Fatal("fail to set ts of cache doc", "key", newKey, "error", err)
	}
	return doc
}

func (doc *cacheDoc[T]) Time() time.Time {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := doc.bucket.cli.getRedisCli()
	newKey := formatDocKey(doc.bucket, doc.key)
	tsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_TS).Result()
	if err != nil {
		Logger.Fatal("fail to parse ts of cache doc", "key", newKey, "error", err)
	}

	preTs, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		Logger.Fatal("fail to parse ts of cache doc", "key", newKey, "error", err)
	}
	return preTs
}

func (doc *cacheDoc[T]) Labels() model.LabelSet {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := doc.bucket.cli.getRedisCli()
	ret := model.LabelSet{}
	newKey := formatDocKey(doc.bucket, doc.key)
	labelsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_LABELS).Result()
	if err != nil {
		Logger.Info("fail to get labels of cache doc, default empty", "key", newKey, "error", err)
		return model.LabelSet{}
	}
	return ret.FromStr(labelsStr)
}

func (doc *cacheDoc[T]) AddLabels(labels []string) model.LabelSet {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := doc.bucket.cli.getRedisCli()
	labelSet := model.LabelSet{}
	newKey := formatDocKey(doc.bucket, doc.key)
	labelsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_LABELS).Result()
	if err == nil {
		labelSet = labelSet.FromStr(labelsStr)
	}
	for _, label := range labels {
		if label == "" {
			continue
		}
		// record the doc key to redis set
		newLabel := formatLabel(doc.bucket, label)
		redisCli.SAdd(ctx, newLabel, doc.key)
		labelSet[label] = true
		redisCli.SAdd(ctx, formatBucketLabels(doc.bucket), label)
	}
	err = redisCli.HSet(ctx, newKey, CACHEDOC_LABELS, labelSet.Format()).Err()
	if err != nil {
		Logger.Fatal("fail to set labels of cache doc", "key", newKey, "error", err)
	}
	return labelSet
}

func (doc *cacheDoc[T]) RemoveLabels(labels []string) model.LabelSet {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := doc.bucket.cli.getRedisCli()
	labelSet := model.LabelSet{}
	newKey := formatDocKey(doc.bucket, doc.key)
	labelsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_LABELS).Result()
	if err != nil {
		Logger.Info("fail to get labels of cache doc for labels removal, ignored", "key", newKey, "error", err)
		return nil
	}
	labelSet = labelSet.FromStr(labelsStr)
	for _, label := range labels {
		if label == "" {
			continue
		}
		// record the doc key to redis set
		newLabel := formatLabel(doc.bucket, label)
		redisCli.SRem(ctx, newLabel, doc.key)
		delete(labelSet, label)
	}
	err = redisCli.HSet(ctx, newKey, CACHEDOC_LABELS, labelSet.Format()).Err()
	if err != nil {
		Logger.Fatal("fail to set labels of cache doc", "key", newKey, "error", err)
	}
	return labelSet
}

func (doc *cacheDoc[T]) Delete() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if doc.expirer != nil {
		doc.expirer.Stop()
		doc.expirer = nil
	}
	redisCli := doc.bucket.cli.getRedisCli()
	labelSet := model.LabelSet{}
	newKey := formatDocKey(doc.bucket, doc.key)
	labelsStr, err := redisCli.HGet(ctx, newKey, CACHEDOC_LABELS).Result()
	if err != nil {
		Logger.Info("fail to get labels of cache doc for doc deletion, ignored", "key", newKey, "error", err)
	}
	labelSet = labelSet.FromStr(labelsStr)
	for label := range labelSet {
		redisCli.SRem(ctx, formatLabel(doc.bucket, label), doc.key)
	}
	redisCli.SRem(ctx, formatBucketKeys(doc.bucket), doc.key)
	redisCli.Del(ctx, newKey)
}

func (doc *cacheDoc[T]) Expire(d time.Duration, onExpire func(model.CacheDoc)) {
	if doc.expirer != nil {
		doc.expirer.Stop()
		doc.expirer = nil
	}
	doc.expirer = time.AfterFunc(d, func() {
		if onExpire != nil {
			onExpire(doc)
		}
		doc.expirer = nil
	})
}
