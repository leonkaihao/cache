package mem

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_newCacheCollection(t *testing.T) {
	ctx := context.Background()
	clt := newCacheCollection(nil, "xxx")

	require.NoError(t, clt.Add(ctx, "key1", []string{"mem1", "mem2", "mem3"}))
	require.NoError(t, clt.Add(ctx, "key2", []string{"mem4", "mem5", "mem6"}))

	require.NoError(t, clt.Remove(ctx, "key1", []string{"mem1", "mem3"}))
	require.NoError(t, clt.Clear(ctx, "key2"))

	keys, err := clt.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Equal(t, "key1", keys[0])

	mem, err := clt.MembersMap(ctx, "key1")
	require.NoError(t, err)
	assert.Len(t, mem, 1)
	_, ok := mem["mem2"]
	assert.True(t, ok, "expect only mem2 in key1")

	mem, err = clt.MembersMap(ctx, "key2")
	require.NoError(t, err)
	assert.Nil(t, mem, "expect nil mem for key2")

	require.NoError(t, clt.Remove(ctx, "key1", []string{"mem2", "mem3"}))

	keys, err = clt.Keys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 1, "expect key1")

	mm, err := clt.MembersMaps(ctx, []string{"key1", "key2"})
	require.NoError(t, err)
	assert.Len(t, mm, 2)
	assert.NotNil(t, mm[0])
	assert.Len(t, mm[0].List(), 0)
	assert.Nil(t, mm[1])
}

// Test that Add() rejects empty members
func TestCollectionAddEmptyMembers(t *testing.T) {
	ctx := context.Background()
	clt := newCacheCollection(nil, "test")

	err := clt.Add(ctx, "key1", []string{})
	assert.Error(t, err, "Add should reject empty members")
	assert.Contains(t, err.Error(), "empty")
}

// Test that Add() rejects empty key
func TestCollectionAddEmptyKey(t *testing.T) {
	ctx := context.Background()
	clt := newCacheCollection(nil, "test")

	err := clt.Add(ctx, "", []string{"mem1"})
	assert.Error(t, err, "Add should reject empty key")
	assert.Contains(t, err.Error(), "key")
}
