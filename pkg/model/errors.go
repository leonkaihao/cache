package model

import "fmt"

// BatchError reports partial failures in batch operations.
// When a batch method returns a non-nil error that is a *BatchError,
// the result slice still contains valid data for keys that succeeded.
// Callers SHOULD type-assert to *BatchError to access per-key detail
// and SHOULD use partial results for the successful positions.
//
// nil results in the batch result slice mean "key has no data" — that
// is a normal condition and is NOT counted as a failure.
type BatchError struct {
	// Total is the number of keys in the batch request.
	Total int
	// Failed is the number of keys that encountered a real retrieval error.
	Failed int
	// KeyErrors maps each failed key to its specific error.
	KeyErrors map[string]error
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("batch query: %d/%d keys failed", e.Failed, e.Total)
}

// NewBatchError creates an empty BatchError for a batch of size total.
func NewBatchError(total int) *BatchError {
	return &BatchError{
		Total:     total,
		Failed:    0,
		KeyErrors: make(map[string]error),
	}
}

// Add records a failure for key with the given error.
func (e *BatchError) Add(key string, err error) {
	e.KeyErrors[key] = err
	e.Failed++
}

// OrNil returns nil if no failures were recorded, otherwise returns e.
func (e *BatchError) OrNil() error {
	if e.Failed == 0 {
		return nil
	}
	return e
}
