package model

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
	Keys() []string
	MembersMap(key string) MembersMap
	MembersMaps(keys []string) []MembersMap
	Add(key string, members []string)
	Remove(key string, members []string)
	Clear(key string)
	ClearAll()
	Delete()
	GetLastErrors() []error
}
