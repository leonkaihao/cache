package model

import (
	"context"
	"time"
)

type CacheDoc interface {
	CacheTime
	CacheExpire
	Key() string
	Val(ctx context.Context) (any, error)
	SetValue(ctx context.Context, val any) error
	Labels(ctx context.Context) (LabelSet, error)
	AddLabels(ctx context.Context, labels []string) error
	RemoveLabels(ctx context.Context, labels []string) error
	Delete(ctx context.Context) error
}

type CacheTime interface {
	WithTime(ctx context.Context, ts time.Time) error
	// SetValueWithTs returns updated flag and error
	SetValueWithTs(ctx context.Context, val any, ts time.Time) (bool, error)
	Time(ctx context.Context) (time.Time, error)
}

type CacheExpire interface {
	Expire(d time.Duration, onExpire func(CacheDoc)) error
	CancelExpire() error
}
