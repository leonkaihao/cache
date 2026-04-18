package mem

import (
	"sync"

	"github.com/leonkaihao/cache/pkg/model"
)

type collection struct {
	sync.RWMutex
	cli  *client
	name string
	docs map[string]map[string]struct{}
}

func newCacheCollection(cli *client, name string) model.CacheCollection {
	return &collection{
		cli:  cli,
		name: name,
		docs: map[string]map[string]struct{}{},
	}
}

func (clt *collection) Name() string {
	return clt.name
}

func (clt *collection) Keys() []string {
	clt.RLock()
	defer clt.RUnlock()
	result := []string{}
	for key := range clt.docs {
		result = append(result, key)
	}
	return result
}

func (clt *collection) MembersMap(key string) model.MembersMap {
	clt.RLock()
	defer clt.RUnlock()
	result := map[string]struct{}{}
	if set, ok := clt.docs[key]; ok {
		for k, v := range set {
			result[k] = v
		}
	} else {
		return nil
	}
	return result
}

func (clt *collection) MembersMaps(keys []string) []model.MembersMap {
	clt.RLock()
	defer clt.RUnlock()
	result := make([]model.MembersMap, len(keys))
	for i, key := range keys {
		each := model.MembersMap{}
		if set, ok := clt.docs[key]; ok {
			for k, v := range set {
				each[k] = v
			}
		} else {
			each = nil
		}
		result[i] = each
	}
	return result
}

func (clt *collection) Add(key string, members []string) {
	if key == "" {
		return
	}
	clt.Lock()
	defer clt.Unlock()
	set, ok := clt.docs[key]
	if !ok {
		set = map[string]struct{}{}
		clt.docs[key] = set
	}
	for _, member := range members {
		set[member] = struct{}{}
	}
}

func (clt *collection) Remove(key string, members []string) {
	if key == "" {
		return
	}
	clt.Lock()
	defer clt.Unlock()
	set, ok := clt.docs[key]
	if !ok {
		return
	}
	for _, member := range members {
		delete(set, member)
	}
}

func (clt *collection) Clear(key string) {
	if key == "" {
		return
	}
	clt.Lock()
	defer clt.Unlock()
	_, ok := clt.docs[key]
	if !ok {
		return
	}
	delete(clt.docs, key)
}

func (clt *collection) ClearAll() {
	clt.Lock()
	defer clt.Unlock()
	clt.docs = map[string]map[string]struct{}{}
}

func (clt *collection) Delete() {
	if clt.cli != nil {
		clt.cli.RemoveCollection(clt.name)
		clt.cli = nil
	}
}

func (clt *collection) GetLastErrors() []error {
	return []error{}
}
