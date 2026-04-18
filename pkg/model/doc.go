package model

import (
	"time"
)

type CacheDoc interface {
	CacheTime
	CacheExpire
	Key() string
	Val() any
	SetValue(val any) CacheDoc
	Labels() LabelSet
	AddLabels(labels []string) LabelSet
	RemoveLabels(label []string) LabelSet
	Delete()
}

type CacheTime interface {
	WithTime(ts time.Time) CacheDoc
	// SetValueWithTs returns doc and flag(value is updated or not)
	SetValueWithTs(val any, ts time.Time) (CacheDoc, bool)
	Time() time.Time
}

type CacheExpire interface {
	Expire(d time.Duration, onExpire func(CacheDoc))
}
