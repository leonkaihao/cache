package model

// FilterOptions provides filtering options for key queries.
// Designed to be extensible with additional filter types in the future.
type FilterOptions struct {
	// LabelFilter supports label-based filtering with OR within each slice and AND between slices.
	// Example:
	//   [][]string{{"a", "b"}, {"c"}} returns keys with (a OR b) AND c
	//   [][]string{{"a"}} returns keys with label "a"
	//   nil or empty means no label filtering
	LabelFilter [][]string
	// Add other filters here in the future (e.g., TimeRange, KeyPattern, etc.)
}
