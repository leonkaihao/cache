package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchError_ErrorMessage(t *testing.T) {
	be := &BatchError{
		Total:     10,
		Failed:    2,
		KeyErrors: map[string]error{"k1": fmt.Errorf("e1"), "k2": fmt.Errorf("e2")},
	}
	assert.Contains(t, be.Error(), "2/10")
	assert.Contains(t, be.Error(), "keys failed")
}

func TestBatchError_SatisfiesErrorInterface(t *testing.T) {
	var err error = &BatchError{Total: 5, Failed: 1, KeyErrors: map[string]error{"k": fmt.Errorf("oops")}}
	require.NotNil(t, err)

	var be *BatchError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 5, be.Total)
	assert.Equal(t, 1, be.Failed)
}

func TestBatchError_OrNil_NoFailures(t *testing.T) {
	be := NewBatchError(3)
	assert.Nil(t, be.OrNil())
}

func TestBatchError_OrNil_WithFailures(t *testing.T) {
	be := NewBatchError(3)
	be.Add("key1", fmt.Errorf("boom"))
	assert.NotNil(t, be.OrNil())
	assert.Equal(t, 1, be.Failed)
}

func TestBatchError_Add(t *testing.T) {
	be := NewBatchError(5)
	be.Add("k1", fmt.Errorf("err1"))
	be.Add("k2", fmt.Errorf("err2"))
	assert.Equal(t, 2, be.Failed)
	assert.Contains(t, be.KeyErrors, "k1")
	assert.Contains(t, be.KeyErrors, "k2")
}
