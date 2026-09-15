# request-wal Specification (delta)

## ADDED Requirements

### Requirement: Append-only request log
The event SHALL be a per-request, in-memory, append-only log of typed
field records with one writer (the request goroutine) and no lock on the
unarmed fast path.

#### Scenario: Field append
- GIVEN a request with an attached event
- WHEN `hc.Add(ctx, "user_id", "u_8472")` is called
- THEN one typed record is appended in ~10 ns with zero allocations for
  constant scalar values

#### Scenario: Deterministic order
- GIVEN three fields added in order A, B, C
- WHEN the event is committed
- THEN the emitted JSON contains them in insertion order

### Requirement: Sealing
The WAL SHALL ignore any mutation whose atomic load observes `End`
having already sealed the event. Writes that begin after seal SHALL be
no-ops and SHALL NOT corrupt another request's buffer.

A residual TOCTOU on the unarmed single-load fast path is accepted
(amendments 1/20): a straggler that loaded `walLive` and is preempted
across `End` → seal → recycle MAY still complete that in-flight append
into the recycled buffer. The generation check closes every write that
starts after recycle.

#### Scenario: Straggler write after commit
- GIVEN an event whose `End` has committed and recycled its buffer
- WHEN an async goroutine calls `hc.Add` on the stale context
- THEN the write is a no-op and no other request's buffer is corrupted

#### Scenario: In-flight append versus recycle
- GIVEN a straggler that has already loaded the live generation
- WHEN `End` seals and recycles the event before that append stores
- THEN the append MAY land in the next request's buffer (accepted
  residual of the single-load protocol)

### Requirement: Duplicate keys
Duplicate keys SHALL resolve last-write-wins at encode time, with dedupe
state allocated only when duplicates exist.

#### Scenario: Overwrite
- GIVEN `Add(ctx, "k", 1)` followed by `Add(ctx, "k", 2)`
- WHEN committed
- THEN the emitted event contains `"k": 2` exactly once

### Requirement: Arming protocol
Every append SHALL perform one atomic load of the armed flag; armed
events SHALL serialize appends and watchdog snapshots under a per-event
mutex.

#### Scenario: Watchdog snapshot mid-flight
- GIVEN an armed (stalled) event appending records
- WHEN the watchdog reads the WAL tail
- THEN the snapshot is race-detector clean
