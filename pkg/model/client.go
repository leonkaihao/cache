package model

type CacheClient interface {
	WithBucket(CacheBucket) CacheBucket
	Bucket(name string) CacheBucket
	Buckets() []CacheBucket
	RemoveBucket(bktName string)

	Collection(name string) CacheCollection
	Collections() []CacheCollection
	RemoveCollection(name string)

	Timeline(name string) CacheTimeline
	Timelines() []CacheTimeline
	RemoveTimeline(name string) error
}
