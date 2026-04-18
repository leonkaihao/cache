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
	name string
	cli  *client
	errs []error
}

func newCacheCollection(cli *client, name string) model.CacheCollection {
	return &collection{
		cli:  cli,
		name: name,
		errs: []error{},
	}
}

func (clt *collection) Name() string {
	return clt.name
}

func (clt *collection) Keys() []string {
	clt.cleanErrors()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cli := clt.cli.getRedisCli()
	result, err := cli.SMembers(ctx, formatCollectionKeys(clt)).Result()
	if err != nil {
		clt.errs = append(clt.errs, fmt.Errorf("fail to get members of Set keys %v, %v", formatCollectionKeys(clt), err))
		return nil
	}
	return result
}

// return nil if key is not in the collection
func (clt *collection) MembersMap(key string) model.MembersMap {
	clt.cleanErrors()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cli := clt.cli.getRedisCli()

	if isMm, err := cli.SIsMember(ctx, formatCollectionKeys(clt), key).Result(); err != nil || !isMm {
		return nil
	}

	result, err := cli.SMembersMap(ctx, formatCollectionKey(clt, key)).Result()
	if err != nil {
		clt.errs = append(clt.errs, fmt.Errorf("fail to get members map of Set key %v, %v", formatCollectionKey(clt, key), err))
		return nil
	}
	return result
}

// MembersMaps return array items, if an item is nil, means the key is not exist
func (clt *collection) MembersMaps(keys []string) []model.MembersMap {
	clt.cleanErrors()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	newKeys := make([]string, len(keys))
	result := make([]model.MembersMap, len(keys))
	// check key exists in collection
	cli := clt.cli.getRedisCli()
	pipe := cli.Pipeline()
	boolCmds := make([]*goredis.BoolCmd, len(keys))
	for i, key := range keys {
		cmd := pipe.SIsMember(ctx, formatCollectionKeys(clt), key)
		boolCmds[i] = cmd
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		clt.errs = append(clt.errs, fmt.Errorf("fail to check collection keys, %v", err))
	}
	// check members
	pipe = cli.Pipeline()
	mapCmds := make([]*goredis.StringStructMapCmd, len(newKeys))
	for i, key := range keys {
		newKeys[i] = formatCollectionKey(clt, key)
		cmd := pipe.SMembersMap(ctx, newKeys[i])
		mapCmds[i] = cmd
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		clt.errs = append(clt.errs, fmt.Errorf("fail to get values of collection %v members maps, %v", len(keys), err))
	}

	for i := range keys {
		exists, err := boolCmds[i].Result()
		if err != nil {
			clt.errs = append(clt.errs, fmt.Errorf("fail to check key %v, %v", keys[i], err))
			result[i] = nil
			continue
		} else if !exists {
			result[i] = nil
			continue
		}
		mm, err := mapCmds[i].Result()
		if err != nil {
			clt.errs = append(clt.errs, fmt.Errorf("fail to get member map %v, %v", newKeys[i], err))
			result[i] = model.MembersMap{}
			continue
		}
		result[i] = mm

	}
	return result
}

func (clt *collection) Add(key string, members []string) {
	clt.cleanErrors()
	if key == "" {
		clt.errs = append(clt.errs, fmt.Errorf("Add collection members %v with an empty key", members))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cli := clt.cli.getRedisCli()
	var err error
	if len(members) == 0 {
		_, err = cli.SAdd(ctx, formatCollectionKey(clt, key)).Result()
	} else {
		_, err = cli.SAdd(ctx, formatCollectionKey(clt, key), members).Result()
	}
	if err != nil {
		clt.errs = append(clt.errs, fmt.Errorf("fail to add members to Set %v, %v", formatCollectionKey(clt, key), err))
		return
	}
	cli.SAdd(ctx, formatCollectionKeys(clt), key)
}

func (clt *collection) Remove(key string, members []string) {
	clt.cleanErrors()

	if key == "" {
		clt.errs = append(clt.errs, fmt.Errorf("Remove collection members %v with an empty key", members))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cli := clt.cli.getRedisCli()
	var (
		err error
	)

	if len(members) != 0 {
		_, err = cli.SRem(ctx, formatCollectionKey(clt, key), members).Result()
	}
	if err != nil {
		clt.errs = append(clt.errs, fmt.Errorf("fail to remove members from Set %v, %v", formatCollectionKey(clt, key), err))
	}
}

func (clt *collection) Clear(key string) {
	clt.cleanErrors()
	if key == "" {
		clt.errs = append(clt.errs, fmt.Errorf("Clear collection with an empty key"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	clt.clear(ctx, key)
}

func (clt *collection) ClearAll() {
	clt.cleanErrors()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	clt.clearAll(ctx)
}

func (clt *collection) clear(ctx context.Context, key string) {
	cli := clt.cli.getRedisCli()
	_, err := cli.Del(ctx, formatCollectionKey(clt, key)).Result()
	if err != nil {
		clt.errs = append(clt.errs, fmt.Errorf("fail to delete collection %v, %v", formatCollectionKey(clt, key), err))
	}
	if key != "" { // keys collection
		cli.SRem(ctx, formatCollectionKeys(clt), key)
	}
}

func (clt *collection) clearAll(ctx context.Context) {
	cli := clt.cli.getRedisCli()
	members, err := cli.SMembers(ctx, formatCollectionKeys(clt)).Result()
	if err != nil {
		clt.errs = append(clt.errs, fmt.Errorf("fail to get members of Set keys %v, %v", formatCollectionKeys(clt), err))
		return
	}
	for i, key := range members {
		members[i] = formatCollectionKey(clt, key)
	}
	cli.Del(ctx, members...)
}

func (clt *collection) Delete() {
	clt.cleanErrors()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	clt.clearAll(ctx)
	clt.clear(ctx, "")
	clt.cli.RemoveCollection(clt.Name())
}

func (clt *collection) GetLastErrors() []error {
	return clt.errs
}

func (clt *collection) cleanErrors() {
	clt.errs = []error{}
}

func formatCollectionKeys(clt model.CacheCollection) string {
	return fmt.Sprintf("%v%v/%v", consts.CLT_PREFIX, clt.Name(), consts.KEYS_PREFIX)
}

func formatCollectionKey(clt model.CacheCollection, key string) string {
	return fmt.Sprintf("%v%v/%v%v", consts.CLT_PREFIX, clt.Name(), consts.KEYS_PREFIX, key)
}
