package mem

import (
	"context"
	"fmt"
	"sync"

	"github.com/leonkaihao/cache/v2/pkg/model"
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

func (clt *collection) Keys(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	clt.RLock()
	defer clt.RUnlock()

	result := make([]string, 0, len(clt.docs))
	for key := range clt.docs {
		result = append(result, key)
	}

	return result, nil
}

func (clt *collection) MembersMap(ctx context.Context, key string) (model.MembersMap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	clt.RLock()
	defer clt.RUnlock()

	set, ok := clt.docs[key]
	if !ok {
		return nil, nil // Not an error, just doesn't exist
	}

	// Copy to prevent external mutation
	result := make(model.MembersMap, len(set))
	for k, v := range set {
		result[k] = v
	}

	return result, nil
}

func (clt *collection) MembersMaps(ctx context.Context, keys []string) ([]model.MembersMap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	clt.RLock()
	defer clt.RUnlock()

	result := make([]model.MembersMap, len(keys))
	for i, key := range keys {
		if set, ok := clt.docs[key]; ok {
			mm := make(model.MembersMap, len(set))
			for k, v := range set {
				mm[k] = v
			}
			result[i] = mm
		} else {
			result[i] = nil
		}
	}

	return result, nil
}

func (clt *collection) Add(ctx context.Context, key string, members []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	if len(members) == 0 {
		return fmt.Errorf("members cannot be empty")
	}

	clt.Lock()
	defer clt.Unlock()

	set, ok := clt.docs[key]
	if !ok {
		set = make(map[string]struct{})
		clt.docs[key] = set
	}

	for _, member := range members {
		set[member] = struct{}{}
	}

	return nil
}

func (clt *collection) Remove(ctx context.Context, key string, members []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	if len(members) == 0 {
		return fmt.Errorf("members cannot be empty")
	}

	clt.Lock()
	defer clt.Unlock()

	set, ok := clt.docs[key]
	if !ok {
		return nil // Not an error, nothing to remove
	}

	for _, member := range members {
		delete(set, member)
	}

	return nil
}

func (clt *collection) Clear(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	clt.Lock()
	defer clt.Unlock()

	delete(clt.docs, key)
	return nil
}

func (clt *collection) ClearAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	clt.Lock()
	defer clt.Unlock()

	clt.docs = make(map[string]map[string]struct{})
	return nil
}

func (clt *collection) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := clt.ClearAll(ctx); err != nil {
		return fmt.Errorf("failed to clear collection: %w", err)
	}

	if clt.cli != nil {
		clt.cli.RemoveCollection(clt.name)
		clt.cli = nil
	}

	return nil
}
