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

// Query state
state, err := timeline.GetLatest(ctx, "device_A")
```

## See Also

- Main documentation in `README.md`
- API specification in `pkg/model/timeline.go`
- Design document in `openspec/changes/add-timeline-cache/design.md`
