package model

import "context"

type MembersMap map[string]struct{}

func (smm MembersMap) List() []string {
	result := []string{}
	for k := range smm {
		result = append(result, k)
	}
	return result
}

func (smm MembersMap) Exists(mem string) bool {
	_, ok := smm[mem]
	return ok
}

type CacheCollection interface {
	Name() string
	Keys(ctx context.Context) ([]string, error)
	MembersMap(ctx context.Context, key string) (MembersMap, error)
	MembersMaps(ctx context.Context, keys []string) ([]MembersMap, error)
	Add(ctx context.Context, key string, members []string) error
	Remove(ctx context.Context, key string, members []string) error
	Clear(ctx context.Context, key string) error
	ClearAll(ctx context.Context) error
	Delete(ctx context.Context) error
}
