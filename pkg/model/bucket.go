package model

import (
	"context"
	"time"
)

type CacheBucket interface {
	Name() string
	Docs(ctx context.Context, keys []string) ([]CacheDoc, error)
	Values(ctx context.Context, keys []string) ([]any, error)
	Update(ctx context.Context, key string, data any) (CacheDoc, error)
	// UpdateWithTs return doc, updated flag, and error
	UpdateWithTs(ctx context.Context, key string, data any, ts time.Time) (CacheDoc, bool, error)
	//Filter return all keys that match the given label filters
	Filter(ctx context.Context, labelFilters ...[]string) ([]string, error)
	// Scan return all keys that match the given pattern
	Scan(ctx context.Context, match string) ([]string, error)
	Remove(ctx context.Context, keys []string) error
	Clear(ctx context.Context) error
	Delete(ctx context.Context) error
}
