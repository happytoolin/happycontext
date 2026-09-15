# record-sink Specification (delta)

## ADDED Requirements

### Requirement: Record view
Sinks SHALL receive a read-only `*Record` — level, message, insertion-
ordered `Fields()`, `Lookup(key)`, and a lazily built `Encoded()` cache —
valid only for the duration of `Write`.

#### Scenario: Zero-copy read
- GIVEN a kept event with 12 fields
- WHEN a sink calls `rec.Fields()`
- THEN no map is built and no clone occurs

#### Scenario: Encoded is encode-once
- GIVEN a record written by a fan-out of two sinks
- WHEN both call `rec.Encoded()`
- THEN encoding happened at most once

### Requirement: Sink contract
`Sink.Write(ctx context.Context, rec *Record)` SHALL follow the
`slog.Handler.Handle` shape; implementations SHALL be safe for concurrent
use and SHALL NOT retain the record or its bytes past the call.

#### Scenario: Request context reaches sinks
- GIVEN a commit on a request with an active context
- WHEN the sink writes
- THEN it receives that context, not a fabricated background one

#### Scenario: Retention violation
- GIVEN a sink that stores `rec.Encoded()` beyond `Write`
- WHEN the buffer is recycled
- THEN documented contract is violated — examples must copy
