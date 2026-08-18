# Cache Library

A flexible caching library for Go with support for in-memory and Redis backends.

## Language

**Timeline**:
A time-indexed state storage where multiple versions of state coexist at different timestamps. Each field maintains its own independent time series with per-field retention. Supports sparse field updates, out-of-order insertion, and time-based queries. Each timeline maintains a union schema of all fields that have appeared across any key.
_Avoid_: time series, versioned cache, temporal store

**TimelineData**:
The data operations interface of a timeline, responsible for writing (Append, Insert), querying (GetAt, GetLatest, GetRange), and lifecycle management (Remove, Clear, Delete).
_Avoid_: timeline storage, data layer

**TimelineLabels**:
The label management interface of a timeline, responsible for associating keys with labels and querying keys by label filters.
_Avoid_: tagging, metadata

**TimelineOptions**:
Configuration options for a timeline, including retention policies and future extensibility points.
_Avoid_: timeline config, settings

**Retention Policy**:
Automatic data lifecycle management rules defining how long timeline data is kept. Applied per-field independently: each field retains its MaxCount most recent updates and/or MaxDuration time window.
_Avoid_: TTL, expiration policy, cleanup policy

**Bucket**:
A typed key-value store with support for labels, timestamps, and expiration callbacks.
_Avoid_: namespace, partition

**Collection**:
A set-based data structure mapping keys to members, where each member is a string.
_Avoid_: set store, multi-map
