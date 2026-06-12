package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/leonkaihao/cache/pkg/consts"
	"github.com/leonkaihao/cache/pkg/model"
)

type collection struct {
	name    string
	cli     *client
	timeout time.Duration
}

func newCacheCollection(cli *client, name string) model.CacheCollection {
	return &collection{
		cli:     cli,
		name:    name,
		timeout: cli.defaultTimeout,
	}
}

func (clt *collection) Name() string {
	return clt.name
}

func (clt *collection) Keys(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, clt.timeout)
	defer cancel()

	cli := clt.cli.getRedisCli()
	result, err := cli.SMembers(ctx, formatCollectionKeys(clt)).Result()
	if err != nil && err != goredis.Nil {
		return nil, fmt.Errorf("fail to get members of Set keys %v: %w", formatCollectionKeys(clt), err)
	}

	return result, nil
}

// MembersMap returns nil if key is not in the collection
func (clt *collection) MembersMap(ctx context.Context, key string) (model.MembersMap, error) {
	ctx, cancel := context.WithTimeout(ctx, clt.timeout)
	defer cancel()

	cli := clt.cli.getRedisCli()

	isMember, err := cli.SIsMember(ctx, formatCollectionKeys(clt), key).Result()
	if err != nil {
		return nil, fmt.Errorf("fail to check if key exists in collection: %w", err)
	}
	if !isMember {
		return nil, nil
	}

	result, err := cli.SMembersMap(ctx, formatCollectionKey(clt, key)).Result()
	if err != nil && err != goredis.Nil {
		return nil, fmt.Errorf("fail to get members map of Set key %v: %w", formatCollectionKey(clt, key), err)
	}

	return result, nil
}

// MembersMaps return array items, if an item is nil, means the key does not exist
func (clt *collection) MembersMaps(ctx context.Context, keys []string) ([]model.MembersMap, error) {
	ctx, cancel := context.WithTimeout(ctx, clt.timeout)
	defer cancel()

	newKeys := make([]string, len(keys))
	result := make([]model.MembersMap, len(keys))

	cli := clt.cli.getRedisCli()
	pipe := cli.Pipeline()
	boolCmds := make([]*goredis.BoolCmd, len(keys))

	// Check key exists in collection
	for i, key := range keys {
		cmd := pipe.SIsMember(ctx, formatCollectionKeys(clt), key)
		boolCmds[i] = cmd
	}

	if _, err := pipe.Exec(ctx); err != nil && err != goredis.Nil {
		return nil, fmt.Errorf("fail to check collection keys: %w", err)
	}

	// Get members
	pipe = cli.Pipeline()
	mapCmds := make([]*goredis.StringStructMapCmd, len(newKeys))
	for i, key := range keys {
		newKeys[i] = formatCollectionKey(clt, key)
		cmd := pipe.SMembersMap(ctx, newKeys[i])
		mapCmds[i] = cmd
	}

	if _, err := pipe.Exec(ctx); err != nil && err != goredis.Nil {
		return nil, fmt.Errorf("fail to get values of collection %v members maps: %w", len(keys), err)
	}

	for i := range keys {
		exists, err := boolCmds[i].Result()
		if err != nil {
			return nil, fmt.Errorf("fail to check key %v: %w", keys[i], err)
		}
		if !exists {
			result[i] = nil
			continue
		}

		mm, err := mapCmds[i].Result()
		if err != nil && err != goredis.Nil {
			return nil, fmt.Errorf("fail to get member map %v: %w", newKeys[i], err)
		}
		result[i] = mm
	}

	return result, nil
}

func (clt *collection) Add(ctx context.Context, key string, members []string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}
	if len(members) == 0 {
		return fmt.Errorf("members cannot be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, clt.timeout)
	defer cancel()

	cli := clt.cli.getRedisCli()

	// Convert members to []interface{} for SAdd
	memberIfaces := make([]interface{}, len(members))
	for i, m := range members {
		memberIfaces[i] = m
	}

	if _, err := cli.SAdd(ctx, formatCollectionKey(clt, key), memberIfaces...).Result(); err != nil {
		return fmt.Errorf("fail to add members to Set %v: %w", formatCollectionKey(clt, key), err)
	}

	if err := cli.SAdd(ctx, formatCollectionKeys(clt), key).Err(); err != nil {
		return fmt.Errorf("fail to add key to collection keys: %w", err)
	}

	return nil
}

func (clt *collection) Remove(ctx context.Context, key string, members []string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}
	if len(members) == 0 {
		return nil // Nothing to remove
	}

	ctx, cancel := context.WithTimeout(ctx, clt.timeout)
	defer cancel()

	cli := clt.cli.getRedisCli()

	// Convert members to []interface{} for SRem
	memberIfaces := make([]interface{}, len(members))
	for i, m := range members {
		memberIfaces[i] = m
	}

	if _, err := cli.SRem(ctx, formatCollectionKey(clt, key), memberIfaces...).Result(); err != nil {
		return fmt.Errorf("fail to remove members from Set %v: %w", formatCollectionKey(clt, key), err)
	}

	return nil
}

func (clt *collection) Clear(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, clt.timeout)
	defer cancel()

	return clt.clear(ctx, key)
}

func (clt *collection) ClearAll(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, clt.timeout)
	defer cancel()

	return clt.clearAll(ctx)
}

func (clt *collection) clear(ctx context.Context, key string) error {
	cli := clt.cli.getRedisCli()

	if _, err := cli.Del(ctx, formatCollectionKey(clt, key)).Result(); err != nil {
		return fmt.Errorf("fail to delete collection %v: %w", formatCollectionKey(clt, key), err)
	}

	if key != "" { // Remove from keys collection
		if err := cli.SRem(ctx, formatCollectionKeys(clt), key).Err(); err != nil {
			return fmt.Errorf("fail to remove key from collection keys: %w", err)
		}
	}

	return nil
}

func (clt *collection) clearAll(ctx context.Context) error {
	cli := clt.cli.getRedisCli()

	members, err := cli.SMembers(ctx, formatCollectionKeys(clt)).Result()
	if err != nil && err != goredis.Nil {
		return fmt.Errorf("fail to get members of Set keys %v: %w", formatCollectionKeys(clt), err)
	}

	if len(members) > 0 {
		memberKeys := make([]string, len(members))
		for i, key := range members {
			memberKeys[i] = formatCollectionKey(clt, key)
		}

		if err := cli.Del(ctx, memberKeys...).Err(); err != nil {
			return fmt.Errorf("fail to delete collection members: %w", err)
		}
	}

	return nil
}

func (clt *collection) Delete(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, clt.timeout)
	defer cancel()

	if err := clt.clearAll(ctx); err != nil {
		return fmt.Errorf("fail to clear all collection members: %w", err)
	}

	if err := clt.clear(ctx, ""); err != nil {
		return fmt.Errorf("fail to clear collection keys: %w", err)
	}

	clt.cli.RemoveCollection(clt.Name())
	return nil
}

func formatCollectionKeys(clt model.CacheCollection) string {
	return fmt.Sprintf("%v%v/%v", consts.CLT_PREFIX, clt.Name(), consts.KEYS_PREFIX)
}

func formatCollectionKey(clt model.CacheCollection, key string) string {
	return fmt.Sprintf("%v%v/%v%v", consts.CLT_PREFIX, clt.Name(), consts.KEYS_PREFIX, key)
}
