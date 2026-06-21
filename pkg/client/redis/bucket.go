package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/leonkaihao/cache/pkg/consts"
	"github.com/leonkaihao/cache/pkg/model"
)

type bucket[T any] struct {
	name    string
	cli     *client
	coder   model.Coder
	timeout time.Duration
}

func NewBucket[T any](cli model.CacheClient, name string, coder model.Coder) (model.CacheBucket, error) {
	c, ok := cli.(*client)
	if !ok {
		return nil, fmt.Errorf("expected *redis.client, got %T", cli)
	}
	if coder == nil {
		return nil, fmt.Errorf("coder cannot be nil")
	}
	return &bucket[T]{
		name:    name,
		cli:     c,
		coder:   coder,
		timeout: c.defaultTimeout,
	}, nil
}

func (bkt *bucket[T]) Name() string {
	return bkt.name
}

func (bkt *bucket[T]) Docs(ctx context.Context, keys []string) ([]model.CacheDoc, error) {
	ctx, cancel := context.WithTimeout(ctx, bkt.timeout)
	defer cancel()

	docs := make([]model.CacheDoc, len(keys))
	redisCli := bkt.cli.getRedisCli()
	pipe := redisCli.Pipeline()
	cmds := []*goredis.IntCmd{}
	newKeys := []string{}

	for _, key := range keys {
		newKey := formatDocKey(bkt, key)
		newKeys = append(newKeys, newKey)
		cmd := pipe.Exists(ctx, newKey)
		cmds = append(cmds, cmd)
	}

	if _, err := pipe.Exec(ctx); err != nil && err != goredis.Nil {
		return nil, fmt.Errorf("fail to check docs existence: %w", err)
	}

	be := model.NewBatchError(len(keys))
	for i, cmd := range cmds {
		exists, err := cmd.Result()
		if err != nil {
			be.Add(keys[i], fmt.Errorf("fail to check cache doc %v: %w", newKeys[i], err))
			continue
		}
		if exists == 0 {
			docs[i] = nil
			continue
		}
		docs[i] = &cacheDoc[T]{
			bucket: bkt,
			key:    keys[i],
		}
	}

	return docs, be.OrNil()
}

func (bkt *bucket[T]) Values(ctx context.Context, keys []string) ([]any, error) {
	ctx, cancel := context.WithTimeout(ctx, bkt.timeout)
	defer cancel()

	values := make([]any, len(keys))
	redisCli := bkt.cli.getRedisCli()
	pipe := redisCli.Pipeline()
	cmds := []*goredis.StringCmd{}
	newKeys := []string{}

	for _, key := range keys {
		newKey := formatDocKey(bkt, key)
		newKeys = append(newKeys, newKey)
		cmd := pipe.HGet(ctx, newKey, CACHEDOC_VAL)
		cmds = append(cmds, cmd)
	}

	if _, err := pipe.Exec(ctx); err != nil && err != goredis.Nil {
		return nil, fmt.Errorf("fail to get values: %w", err)
	}

	be := model.NewBatchError(len(keys))
	for i, cmd := range cmds {
		jsonData, err := cmd.Result()
		if err != nil {
			if err == goredis.Nil {
				values[i] = nil
				continue
			}
			be.Add(keys[i], fmt.Errorf("fail to fetch cache doc %v: %w", newKeys[i], err))
			continue
		}
		data := new(T)
		if err := bkt.coder.Decode(jsonData, data); err != nil {
			be.Add(keys[i], fmt.Errorf("fail to decode value of cache doc %v: %w", newKeys[i], err))
			continue
		}
		values[i] = data
	}

	return values, be.OrNil()
}

func (bkt *bucket[T]) Update(ctx context.Context, key string, data any) (model.CacheDoc, error) {
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	doc := NewCacheDoc(bkt, key)
	if err := doc.SetValue(ctx, data); err != nil {
		return nil, err
	}

	return doc, nil
}

func (bkt *bucket[T]) UpdateWithTs(ctx context.Context, key string, data any, ts time.Time) (model.CacheDoc, bool, error) {
	if key == "" {
		return nil, false, fmt.Errorf("key cannot be empty")
	}

	doc := NewCacheDoc(bkt, key)
	updated, err := doc.SetValueWithTs(ctx, data, ts)
	if err != nil {
		return nil, false, err
	}

	return doc, updated, nil
}

// Filter is a way of filtering data with labels
// it can have multiple label filters
// each filter is a string array, label is the item. all the labels inside a filter has OR logic
// between filters are AND logic
// i.e. Filter([]string{"foo", "bar"}, []string{"new", "bee"}) means data with label foo OR bar, AND new OR bee.
func (bkt *bucket[T]) Filter(ctx context.Context, filterSteps ...[]string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, bkt.timeout)
	defer cancel()

	redisCli := bkt.cli.getRedisCli()

	if len(filterSteps) == 0 {
		result, err := redisCli.SMembers(ctx, formatBucketKeys(bkt)).Result()
		if err != nil {
			return nil, fmt.Errorf("fail to get bucket keys: %w", err)
		}
		return result, nil
	}

	pipe := redisCli.Pipeline()
	cmds := map[string]*goredis.StringSliceCmd{}

	for _, labels := range filterSteps {
		for _, label := range labels {
			newLabel := formatLabel(bkt, label)
			cmd := pipe.SMembers(ctx, newLabel)
			cmds[newLabel] = cmd
		}
	}

	if _, err := pipe.Exec(ctx); err != nil && err != goredis.Nil {
		return nil, fmt.Errorf("fail to get label members: %w", err)
	}

	filterStepsNew := [][]map[string]bool{}
	for _, labels := range filterSteps {
		newMaps := []map[string]bool{}
		for _, label := range labels {
			newLabel := formatLabel(bkt, label)
			result, err := cmds[newLabel].Result()
			if err != nil && err != goredis.Nil {
				return nil, fmt.Errorf("fail to get members from label %v: %w", newLabel, err)
			}
			newMaps = append(newMaps, arrToMap(result))
		}
		filterStepsNew = append(filterStepsNew, newMaps)
	}

	base := map[string]bool{}
	for i, keysets := range filterStepsNew {
		collection := map[string]bool{}

		for j, keyset := range keysets {
			if j == 0 {
				collection = keyset
				continue
			}
			collection = union(collection, keyset)
		}

		if len(keysets) == 0 {
			result, err := redisCli.SMembers(ctx, formatBucketKeys(bkt)).Result()
			if err != nil {
				return nil, fmt.Errorf("fail to get bucket keys: %w", err)
			}
			collection = arrToMap(result)
		}

		if i == 0 {
			base = collection
			continue
		}

		base = intersect(base, collection)
	}

	ret := make([]string, 0, len(base))
	for key := range base {
		ret = append(ret, key)
	}

	return ret, nil
}

// Scan find all the matched keys with redis scan key pattern within the bucket
func (bkt *bucket[T]) Scan(ctx context.Context, match string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, bkt.timeout)
	defer cancel()

	redisCli := bkt.cli.getRedisCli()
	var (
		cursor uint64 = 0
		result        = []string{}
	)
	pattern := formatBucketKeyMatch(bkt, match)

	for {
		keys, nextCursor, err := redisCli.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("fail to scan bucket %v with pattern %v: %w", bkt.name, pattern, err)
		}

		for _, key := range keys {
			trimmedKey := key[len(formatBucketKeys(bkt)):]
			result = append(result, trimmedKey)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return result, nil
}

func (bkt *bucket[T]) Clear(ctx context.Context) error {
	return bkt.clear(ctx)
}

func (bkt *bucket[T]) Remove(ctx context.Context, keys []string) error {
	for _, key := range keys {
		doc := NewCacheDoc(bkt, key)
		if err := doc.Delete(ctx); err != nil {
			return fmt.Errorf("failed to remove key %s: %w", key, err)
		}
	}
	return nil
}

func (bkt *bucket[T]) Delete(ctx context.Context) error {
	if err := bkt.clear(ctx); err != nil {
		return fmt.Errorf("failed to clear bucket: %w", err)
	}

	if bkt.cli != nil {
		bkt.cli.RemoveBucket(bkt.name)
		bkt.cli = nil
	}

	return nil
}

func (bkt *bucket[T]) clear(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, bkt.timeout)
	defer cancel()

	redisCli := bkt.cli.getRedisCli()

	keys, err := redisCli.SMembers(ctx, formatBucketKeys(bkt)).Result()
	if err != nil && err != goredis.Nil {
		return fmt.Errorf("fail to get members of bucket keyset %v: %w", formatBucketKeys(bkt), err)
	}

	// Delete all documents
	if len(keys) > 0 {
		docKeys := make([]string, len(keys))
		for i, key := range keys {
			docKeys[i] = formatDocKey(bkt, key)
		}
		if err := redisCli.Del(ctx, docKeys...).Err(); err != nil {
			return fmt.Errorf("fail to delete doc keys: %w", err)
		}
	}

	// Delete bucket keys set
	if err := redisCli.Del(ctx, formatBucketKeys(bkt)).Err(); err != nil {
		return fmt.Errorf("fail to delete bucket keys set: %w", err)
	}

	// Delete labels
	labels, err := redisCli.SMembers(ctx, formatBucketLabels(bkt)).Result()
	if err != nil && err != goredis.Nil {
		return fmt.Errorf("fail to get members of bucket labelset %v: %w", formatBucketLabels(bkt), err)
	}

	if len(labels) > 0 {
		labelKeys := make([]string, len(labels))
		for i, label := range labels {
			labelKeys[i] = formatLabel(bkt, label)
		}
		if err := redisCli.Del(ctx, labelKeys...).Err(); err != nil {
			return fmt.Errorf("fail to delete label keys: %w", err)
		}
	}

	// Delete bucket labels set
	if err := redisCli.Del(ctx, formatBucketLabels(bkt)).Err(); err != nil {
		return fmt.Errorf("fail to delete bucket labels set: %w", err)
	}

	return nil
}

func arrToMap(src []string) map[string]bool {
	result := map[string]bool{}
	for _, key := range src {
		result[key] = true
	}
	return result
}

func intersect(set1, set2 map[string]bool) map[string]bool {
	for key := range set1 {
		if _, ok := set2[key]; !ok {
			delete(set1, key)
		}
	}
	return set1
}

func union(set1, set2 map[string]bool) map[string]bool {
	for key := range set2 {
		if _, ok := set1[key]; !ok {
			set1[key] = true
		}
	}
	return set1
}

func formatDocKey(bkt model.CacheBucket, key string) string {
	return fmt.Sprintf("%v%v/%v%v", consts.BUCKET_PREFIX, bkt.Name(), consts.KEYS_PREFIX, key)
}

func formatLabel(bkt model.CacheBucket, label string) string {
	return fmt.Sprintf("%v%v/%v%v", consts.BUCKET_PREFIX, bkt.Name(), consts.LABELS_PREFIX, label)
}

func formatBucketKeys(bkt model.CacheBucket) string {
	return fmt.Sprintf("%v%v/%v", consts.BUCKET_PREFIX, bkt.Name(), consts.KEYS_PREFIX)
}

func formatBucketLabels(bkt model.CacheBucket) string {
	return fmt.Sprintf("%v%v/%v", consts.BUCKET_PREFIX, bkt.Name(), consts.LABELS_PREFIX)
}

func formatBucketKeyMatch(bkt model.CacheBucket, scan string) string {
	return fmt.Sprintf("%v%v/%v%v", consts.BUCKET_PREFIX, bkt.Name(), consts.KEYS_PREFIX, scan)
}
