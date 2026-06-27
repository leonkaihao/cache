# Timeline Examples

This directory contains examples demonstrating Timeline usage.

## Device State Tracking

See `cmd/sample-mem/main.go` and `cmd/sample-redis/main.go` for complete examples of:

- Creating timelines
- Recording state updates with sparse field updates
- Querying historical state
- Handling out-of-order events
- Managing retention policies
- Recomputation after historical insertions
- Batch queries with multiple keys
- Label-based key filtering

## Basic Usage

```go
// Create timeline
timeline := cli.Timeline("device_states")

// Set retention policy
timeline.SetRetention(model.RetentionPolicy{
    MaxCount:    100,
    MaxDuration: 2 * time.Hour,
    Strategy:    model.RetentionMax,
})

// Record state
timeline.Append(ctx, "device_A", time.Now(), map[string]string{
    "zones": "Z1,Z3",
    "battery": "85",
}, false)

// Query state (batch API - takes []string, returns []map[string]string)
states, err := timeline.GetLatest(ctx, []string{"device_A"})
if err != nil {
    log.Fatal(err)
}
state := states[0] // nil means no data (not an error)
```

## Batch Queries

Query multiple keys at once for better performance:

```go
// Query multiple devices
states, err := timeline.GetLatest(ctx, []string{"device_A", "device_B", "device_C"})
if err != nil {
    log.Fatal(err)
}

// Results are parallel to input keys
for i, key := range []string{"device_A", "device_B", "device_C"} {
    if states[i] == nil {
        log.Printf("%s: no data", key)
    } else {
        log.Printf("%s: battery=%s", key, states[i]["battery"])
    }
}
```

## Label-Based Filtering

Organize and filter keys by labels:

```go
// Add labels to keys
timeline.AddKeyLabels(ctx, "device_A", []string{"sensor", "outdoor", "region-west"})
timeline.AddKeyLabels(ctx, "device_B", []string{"sensor", "indoor", "region-west"})
timeline.AddKeyLabels(ctx, "device_C", []string{"actuator", "outdoor", "region-east"})

// Filter by labels (OR within array, AND between arrays)
outdoorKeys, err := timeline.Keys(ctx, []string{"outdoor"})
// Returns: ["device_A", "device_C"]

westSensors, err := timeline.Keys(ctx, 
    []string{"outdoor", "indoor"},  // OR
    []string{"region-west"},         // AND
    []string{"sensor"},              // AND
)
// Returns: ["device_A", "device_B"]

// Query filtered devices
states, err := timeline.GetLatest(ctx, westSensors)
```

## Partial Failure Handling

Batch operations can return partial results on failure:

```go
import "errors"

states, err := timeline.GetLatest(ctx, []string{"device_A", "device_B", "device_C"})
if err != nil {
    var batchErr *model.BatchError
    if errors.As(err, &batchErr) {
        // Some succeeded - use partial results
        log.Printf("Success: %d/%d", batchErr.Total-batchErr.Failed, batchErr.Total)
        for i, state := range states {
            if state != nil {
                // Process successful result
            }
        }
    } else {
        // Total failure
        log.Fatal(err)
    }
}
```

## See Also

- Main documentation in `README.md`
- API specification in `pkg/model/timeline.go`
- Design document in `openspec/changes/add-timeline-cache/design.md`
