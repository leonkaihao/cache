package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/leonkaihao/cache/pkg/consts"
	"github.com/leonkaihao/cache/pkg/model"
	log "github.com/leonkaihao/log"
)

type bucket[T any] struct {
	name  string
	cli   *client
	coder model.Coder
	errs  []error
}

func NewBucket[T any](cli model.CacheClient, name string, coder model.Coder) *bucket[T] {
	bkt := &bucket[T]{
		name:  name,
		cli:   cli.(*client),
		coder: coder,
		errs:  []error{},
	}
	return bkt
}

func (bkt *bucket[T]) Name() string {
	return bkt.name
}

func (bkt *bucket[T]) Docs(keys []string) []model.CacheDoc {
	bkt.cleanErrors()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
	_, err := pipe.Exec(ctx)
	if err != nil {
		bkt.errs = append(bkt.errs, fmt.Errorf("fail to get values of bucket %v docs, %v", len(keys), err))
	}
	for i, cmd := range cmds {
		exists, err := cmd.Result()
		if err != nil {
			bkt.errs = append(bkt.errs, fmt.Errorf("fail to check cache doc %v, %v", newKeys[i], err))
			docs[i] = nil
			continue
		}
		if exists == 0 {
			docs[i] = nil
			continue
		}
		doc := &cacheDoc[T]{
			bucket: bkt,
			key:    keys[i],
		}
		docs[i] = doc

	}
	return docs
}

func (bkt *bucket[T]) Values(keys []string) []any {
	bkt.cleanErrors()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
	_, err := pipe.Exec(ctx)
	if err != nil {
		bkt.errs = append(bkt.errs, fmt.Errorf("fail to get values of bucket keys %v, %v", newKeys, err))
	}
	for i, cmd := range cmds {
		jsonData, err := cmd.Result()
		if err != nil {
			bkt.errs = append(bkt.errs, fmt.Errorf("fail to fetch cache doc %v, %v", newKeys[i], err))
			values[i] = nil
			continue
		}
		data := new(T)
		err = bkt.coder.Decode(jsonData, data)
		if err != nil {
			bkt.errs = append(bkt.errs, fmt.Errorf("fail to unmarshal value of cache doc %v, %v", newKeys[i], err))
			values[i] = nil
			continue
		}
		values[i] = data
	}
	return values
}

// Update directly update the value with incoming data
func (bkt *bucket[T]) Update(key string, data any) model.CacheDoc {
	bkt.cleanErrors()
	if key == "" {
		bkt.errs = append(bkt.errs, fmt.Errorf("Update: update doc %v with an empty key", data))
		return nil
	}
	doc := NewCacheDoc(bkt, key)
	return doc.SetValue(data)
}

// UpdateWithTs update the data with the latest time, otherwise use existing one.
// if original data don't have time, directly replace it
func (bkt *bucket[T]) UpdateWithTs(key string, data any, ts time.Time) (model.CacheDoc, bool) {
	bkt.cleanErrors()
	if key == "" {
		bkt.errs = append(bkt.errs, fmt.Errorf("UpdateWithTs: updated doc %v with an empty key", data))
		return nil, false
	}
	doc := NewCacheDoc(bkt, key)
	return doc.SetValueWithTs(data, ts)
}

// Filter is a way of filtering data with labels
// it can have multiple label filters
// each filter is a string array, label is the item. all the labels inside a filter has OR logic
// between filters are AND logic
// i.e. Filter([]string{"foo", "bar"}, []string{"new", "bee"}) means data with label foo OR bar, AND new OR bee.
func (bkt *bucket[T]) Filter(filterSteps ...[]string) []string { // result keys
	bkt.cleanErrors()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	redisCli := bkt.cli.getRedisCli()
	if len(filterSteps) == 0 {
		result, err := redisCli.SMembers(ctx, formatBucketKeys(bkt)).Result()
		if err != nil {
			bkt.errs = append(bkt.errs, fmt.Errorf("fail to get members of bucket doc %v, %v", formatBucketKeys(bkt), err))
			return nil
		}
		return result
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
	_, err := pipe.Exec(ctx)
	if err != nil {
		bkt.errs = append(bkt.errs, fmt.Errorf("fail to get values of bucket %v labels, %v", len(cmds), err))
	}

	filterStepsNew := [][]map[string]bool{}
	for _, labels := range filterSteps {
		newMaps := []map[string]bool{}
		for _, label := range labels {
			newLabel := formatLabel(bkt, label)
			result, err := cmds[newLabel].Result()
			if err != nil {
				bkt.errs = append(bkt.errs, fmt.Errorf("fail to get members from label %v, %v", newLabel, err))
				continue
			}
			newMaps = append(newMaps, arrToMap(result))
		}
		filterStepsNew = append(filterStepsNew, newMaps)
	}
	base := map[string]bool{}
	for i, keysets := range filterStepsNew {
		// union all each
		var (
			collection = map[string]bool{}
		)
		for j, keyset := range keysets {
			if j == 0 {
				collection = keyset
				continue
			}
			collection = union(collection, keyset)
		}
		if len(keysets) == 0 {
			newLabel := formatBucketKeys(bkt)
			result, _ := redisCli.SMembers(ctx, newLabel).Result()
			collection = arrToMap(result)
		}
		if i == 0 {
			base = collection
			continue
		}

		base = intersect(base, collection)

	}

	ret := []string{}
	for key := range base {
		ret = append(ret, key)
	}
	return ret
}

// Scan find all the matched keys with redis scan key pattern within the bucket
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
	bkt.cleanErrors()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
			bkt.errs = append(bkt.errs, fmt.Errorf("fail to scan bucket %v with pattern %v, %v", bkt.name, pattern, err))
			return result
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
	return result
}

func (bkt *bucket[T]) Clear() {
	err := bkt.clear()
	if err != nil {
		log.Error(err.Error())
	}
}

func (bkt *bucket[T]) Remove(keys []string) []model.CacheDoc {
	docs := []model.CacheDoc{}
	for _, key := range keys {
		NewCacheDoc(bkt, key).Delete()
	}
	return docs
}

func (bkt *bucket[T]) Delete() {
	bkt.cleanErrors()
	if bkt.cli != nil {
		err := bkt.clear()
		if err != nil {
			bkt.errs = append(bkt.errs, err)
		}
		bkt.cli.RemoveBucket(bkt.name)
		bkt.cli = nil
	}
}

func (bkt *bucket[T]) clear() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisCli := bkt.cli.getRedisCli()

	keys, err := redisCli.SMembers(ctx, formatBucketKeys(bkt)).Result()
	if err != nil {
		return fmt.Errorf("fail to get members of bucket keyset %v, %v", formatBucketKeys(bkt), err)
	}
	for i, key := range keys {
		keys[i] = formatDocKey(bkt, key)
	}
	redisCli.Del(ctx, keys...)
	redisCli.Del(ctx, formatBucketKeys(bkt))

	labels, err := redisCli.SMembers(ctx, formatBucketLabels(bkt)).Result()
	if err != nil {
		return fmt.Errorf("fail to get members of bucket labelset %v, %v", formatBucketLabels(bkt), err)
	}
	for i, label := range labels {
		labels[i] = formatLabel(bkt, label)
	}
	redisCli.Del(ctx, labels...)
	redisCli.Del(ctx, formatBucketLabels(bkt))
	return nil
}

func (bkt *bucket[T]) GetLastErrors() []error {
	return bkt.errs
}

func (bkt *bucket[T]) cleanErrors() {
	bkt.errs = []error{}
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
