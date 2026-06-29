package redis

import (
	"sync"
	"time"

	"github.com/leonkaihao/cache/v2/pkg/model"
	redis "github.com/redis/go-redis/v9"
)

type ClientOption func(*client)

func WithTimeout(d time.Duration) ClientOption {
	return func(c *client) {
		c.defaultTimeout = d
	}
}

type client struct {
	rc             *redis.Client
	bkts           map[string]model.CacheBucket
	collections    map[string]model.CacheCollection
	timelines      map[string]model.CacheTimeline
	defaultTimeout time.Duration
	mu             sync.Mutex
}

func NewClient(url, pass string, dbIndex int, opts ...ClientOption) model.CacheClient {
	cli := &client{
		rc:             redis.NewClient(&redis.Options{Addr: url, Password: pass, DB: dbIndex}),
		bkts:           make(map[string]model.CacheBucket),
		collections:    make(map[string]model.CacheCollection),
		timelines:      make(map[string]model.CacheTimeline),
		defaultTimeout: time.Second, // Default timeout
	}
	for _, opt := range opts {
		opt(cli)
	}
	Logger.Info("redis cache client started", "url", url, "timeout", cli.defaultTimeout)
	return cli
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

func (cli *client) Timeline(name string) model.CacheTimeline {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	tl, ok := cli.timelines[name]
	if !ok {
		tl = &redisTimeline{
			name:      name,
			cli:       cli,
			retention: model.RetentionPolicy{Strategy: model.RetentionMax},
		}
		cli.timelines[name] = tl
	}
	return tl
}

func (cli *client) Timelines() []model.CacheTimeline {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	result := make([]model.CacheTimeline, 0, len(cli.timelines))
	for _, tl := range cli.timelines {
		result = append(result, tl)
	}
	return result
}

func (cli *client) RemoveTimeline(name string) error {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	delete(cli.timelines, name)
	return nil
}
