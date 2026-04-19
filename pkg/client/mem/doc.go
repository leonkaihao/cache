package mem

import (
	"sync"
	"time"

	"github.com/leonkaihao/cache/pkg/model"
)

type cacheDoc[T any] struct {
	sync.RWMutex
	bucket  *bucket[T]
	labels  map[string]bool
	key     string
	val     any
	ts      time.Time
	expirer *time.Timer
}

func NewCacheDoc[T any](bucket *bucket[T], key string, val *T) *cacheDoc[T] {
	return &cacheDoc[T]{
		bucket: bucket,
		key:    key,
		val:    val,
		labels: make(map[string]bool),
	}
}

func (doc *cacheDoc[T]) Key() string {
	return doc.key
}

func (doc *cacheDoc[T]) Val() any {
	return doc.val
}

func (doc *cacheDoc[T]) SetValue(val any) model.CacheDoc {
	doc.val = val
	return doc
}

func (doc *cacheDoc[T]) SetValueWithTs(val any, ts time.Time) (model.CacheDoc, bool) {
	if !ts.After(doc.ts) {
		Logger.Debug("SetValueWithTs: not update because incoming time is before current time", "key", doc.key, "incoming_time", ts, "current_time", doc.ts)
		return doc, false
	}
	doc.ts = ts
	doc.val = val
	return doc, true
}

func (doc *cacheDoc[T]) WithTime(tm time.Time) model.CacheDoc {
	doc.ts = tm
	return doc
}

func (doc *cacheDoc[T]) Time() time.Time {
	return doc.ts
}

func (doc *cacheDoc[T]) Labels() model.LabelSet {
	return model.LabelSet(doc.labels).Copy()
}

func (doc *cacheDoc[T]) AddLabels(labelsOrig []string) model.LabelSet {
	var result model.LabelSet
	doc.Lock()
	labels := []string{}
	for _, label := range labelsOrig {
		if label == "" {
			continue
		}
		doc.labels[label] = true
		labels = append(labels, label)
	}
	result = model.LabelSet(doc.labels).Copy()
	doc.Unlock()
	if doc.bucket != nil {
		doc.bucket.addLabels(doc.key, labels)
	}
	return result
}

func (doc *cacheDoc[T]) RemoveLabels(labelsOrig []string) model.LabelSet {
	var result model.LabelSet
	doc.Lock()
	labels := []string{}
	for _, label := range labelsOrig {
		if label == "" {
			continue
		}
		delete(doc.labels, label)
		labels = append(labels, label)
	}
	result = model.LabelSet(doc.labels).Copy()
	doc.Unlock()
	if doc.bucket != nil {
		doc.bucket.removeLabels(doc.key, labels)
	}
	return result
}

func (doc *cacheDoc[T]) Delete() {
	if doc.expirer != nil {
		doc.expirer.Stop()
		doc.expirer = nil
	}
	if doc.bucket == nil {
		return
	}
	doc.RLock()
	labels := []string{}
	for k := range doc.labels {
		labels = append(labels, k)
	}
	doc.RUnlock()
	doc.bucket.removeLabels(doc.key, labels)
	doc.bucket.Remove([]string{doc.key})
	doc.bucket = nil
}

func (doc *cacheDoc[T]) Expire(d time.Duration, onExpire func(model.CacheDoc)) {
	if doc.expirer != nil {
		doc.expirer.Stop()
		doc.expirer = nil
	}
	doc.expirer = time.AfterFunc(d, func() {
		if onExpire != nil {
			onExpire(doc)
		}
		doc.expirer = nil
	})
}
