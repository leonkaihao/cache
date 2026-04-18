package model

import "time"

type CacheBucket interface {
	Name() string
	Docs(keys []string) []CacheDoc
	Values(keys []string) []any
	Update(key string, data any) CacheDoc
	// UpdateWithTs return doc and flag to indicate if data is updated or not
	UpdateWithTs(key string, data any, ts time.Time) (CacheDoc, bool)
	//Filter return all keys that match the given label filters
	Filter(labelFilters ...[]string) []string
	// Scan return all keys that match the given pattern
	Scan(match string) []string
	Remove(keys []string) []CacheDoc
	Clear()
	Delete()
	GetLastErrors() []error
}
