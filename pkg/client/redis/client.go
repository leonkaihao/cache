package redis

import (
	redis "github.com/redis/go-redis/v9"
	"github.com/leonkaihao/cache/pkg/model"
)

type client struct {
	rc          *redis.Client
	bkts        map[string]model.CacheBucket
	collections map[string]model.CacheCollection
}

func NewClient(url, pass string, dbIndex int) model.CacheClient {
	Logger.Info("redis cache client started", "url", url)
	return &client{
		rc:          redis.NewClient(&redis.Options{Addr: url, Password: pass, DB: dbIndex}),
		bkts:        make(map[string]model.CacheBucket),
		collections: make(map[string]model.CacheCollection),
	}
}

func (cli *client) WithBucket(bkt model.CacheBucket) model.CacheBucket {
	if bkt == nil {
		return nil
	}
	cli.bkts[bkt.Name()] = bkt
	return bkt
}

func (cli *client) Bucket(name string) model.CacheBucket {
	return cli.bkts[name]
}

func (cli *client) Buckets() []model.CacheBucket {
	bkts := make([]model.CacheBucket, len(cli.bkts))
	var i int
	for _, bkt := range cli.bkts {
		bkts[i] = bkt
		i++
	}
	return bkts
}

func (cli *client) RemoveBucket(bktName string) {
	delete(cli.bkts, bktName)
}

func (cli *client) getRedisCli() *redis.Client {
	return cli.rc
}

func (cli *client) Collection(name string) model.CacheCollection {
	clt, ok := cli.collections[name]
	if !ok {
		clt = newCacheCollection(cli, name)
		cli.collections[name] = clt
	}
	return clt

}

func (cli *client) Collections() []model.CacheCollection {
	result := []model.CacheCollection{}
	for _, clt := range cli.collections {
		result = append(result, clt)
	}
	return result
}

func (cli *client) RemoveCollection(name string) {
	delete(cli.collections, name)
}
