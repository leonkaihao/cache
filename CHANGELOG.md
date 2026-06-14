# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.1.0]

### Added
- **Timeline**: New `CacheTimeline` interface for time-indexed state storage
  - Support for sparse field updates (only changed fields stored per timestamp)
  - Out-of-order event insertion with `Insert()` method
  - Time-based queries: `GetAt()`, `GetExact()`, `GetRange()`, `GetLatest()`, `Timeline()`
  - Recomputation support with `GetAffectedRange()`
  - Configurable retention policies (count + duration with min/max strategies)
  - Per-key retention policy overrides
  - Microsecond-precision timestamps with automatic normalization
  - Atomic field merging for concurrent writes to same timestamp
  - Memory and Redis backend implementations
- New timeline constants in `pkg/consts/consts.go`:
  - `TIMELINE_PREFIX = "T@"`
  - `TS_PREFIX = "TS/"`
  - `RETENTION_PREFIX = "R/"`
- Timeline methods on `CacheClient` interface:
  - `Timeline(name string) CacheTimeline`
  - `Timelines() []CacheTimeline`
  - `RemoveTimeline(name string) error`
- Comprehensive test suites for timeline functionality
- Timeline examples in `cmd/sample-mem/main.go` and `cmd/sample-redis/main.go`
- Timeline documentation in `examples/timeline/README.md`

### Changed
- Updated README.md with Timeline section and examples
- Extended project structure to include timeline components

## [2.0.0] - Previous Release

### Changed
- All operations now accept `context.Context` as first parameter
- All operations now return errors instead of panicking
- Production-safe error handling throughout
- Redis client accepts `WithTimeout()` option

### Breaking Changes
- Context parameter added to all methods
- Error returns added to all operations
- Several method signatures changed for consistency

See previous CHANGELOG for detailed v2.0.0 changes.
