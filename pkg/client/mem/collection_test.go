package mem

import (
	"testing"
)

func Test_newCacheCollection(t *testing.T) {
	clt := newCacheCollection(nil, "xxx")
	clt.Add("key1", []string{"mem1", "mem2", "mem3"})
	clt.Add("key2", []string{"mem4", "mem5", "mem6"})

	clt.Remove("key1", []string{"mem1", "mem3"})
	clt.Clear("key2")
	if len(clt.Keys()) != 1 || clt.Keys()[0] != "key1" {
		t.Errorf("expect key mem2")
	}
	mem := clt.MembersMap("key1")
	if len(mem) != 1 {
		t.Errorf("expect 1 mem for key1")
	}
	if _, ok := mem["mem2"]; !ok {
		t.Errorf("expect only mem2 in key1")
	}

	mem = clt.MembersMap("key2")
	if mem != nil {
		t.Errorf("expect nil mem for key2")
	}

	clt.Remove("key1", []string{"mem2", "mem3"})
	if len(clt.Keys()) != 1 {
		t.Errorf("expect key1")
	}
	mm := clt.MembersMaps([]string{"key1", "key2"})
	if len(mm) != 2 || mm[0] == nil || len(mm[0].List()) != 0 || mm[1] != nil {
		t.Errorf("membermaps checking failed")
	}
}
