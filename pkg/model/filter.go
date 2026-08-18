package model

import "time"

// FilterOptions provides filtering options for key queries.
// Designed to be extensible with additional filter types in the future.
type FilterOptions struct {
	// LabelFilter supports label-based filtering with OR within each slice and AND between slices.
	// Example:
	//   [][]string{{"a", "b"}, {"c"}} returns keys with (a OR b) AND c
	//   [][]string{{"a"}} returns keys with label "a"
	//   nil or empty means no label filtering
	LabelFilter [][]string

	// AfterTs filters keys to only include those with updates after the specified time.
	// Uses exclusive boundary: only keys with updates strictly after this timestamp are returned.
	// nil means no time filtering.
	AfterTs *time.Time
}
