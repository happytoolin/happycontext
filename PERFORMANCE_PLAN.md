# Performance and Simplification Plan

## Purpose

This plan separates API-compatible performance work from changes reserved for
the v1 release. The focus is to avoid work that has no effect on the emitted
event. It does not propose a cache, a new configuration layer, or a new runtime
dependency.

The compatible work is stacked on PR #18, `perf/reduce-hot-path-allocations`.
The stack branch is `feat/nonbreaking-performance-fast-paths`.

Benchmark results in this document came from an Apple M4. The final stacked
branch comparison used Go 1.27.0 and five 300 ms samples against commit
`6fda79e`. Earlier exploratory runs used Go 1.25.14. Several changes affect the
same work, so their percentages must not be added together.

## API-compatible work

### 1. Create exactly one zerolog event

Current code creates an Info event before it selects Debug, Warn, or Error.
The unused Info event is not finalized. It can retain zerolog's event buffer and
it consumes an extra sampler decision.

Implementation:

- Select the event in one level switch.
- Return before field conversion when the selected event is disabled.
- Build and send only the selected event.
- Keep the existing deterministic field order.

Measured result:

- Warn writes improve from about 267 ns to 165 ns, or about 38%.
- Disabled Debug writes improve from about 265 ns to 3.35 ns, or about 79
  times.
- Non-Info writes remove about 624 B and two allocations.
- Sampling observes one decision for one real event.

Compatibility:

- Public APIs and output fields do not change.
- The change fixes incorrect sampler advancement.
- Hooks and enabled-level behavior need explicit tests.

### 2. Stop scanning every policy during finalization

`NormalizeConfig` already creates an isolated normalized configuration for
middleware. Direct callers can still pass a raw `Config`. The finish path does
not need to normalize unrelated policy entries. It only needs the selected
values.

Existing selection logic already:

- clamps the selected default sampling rate;
- clamps the selected policy sampling rate;
- clamps the selected level sampling rate;
- validates the selected success, failure, panic, and outcome levels;
- ignores policy and level keys that are not selected.

Implementation:

- Remove `normalizeConfigShared` from `finishOperation`.
- Keep public `NormalizeConfig` unchanged.
- In `policyForDomain`, use the empty-string policy as the default-domain alias
  only when the canonical `operation` policy is absent.
- Keep nil sink, context, and event guards before all policy work.

Measured result:

| Policy count | Before | After | Improvement |
| ---: | ---: | ---: | ---: |
| 1 | about 450 ns | about 370 ns | about 18% |
| 16 | about 675 ns | about 382 ns | about 44% |
| 128 | about 2,009 ns | about 379 ns | about 81% |

The existing representative policy benchmark improves from about 576 ns to
406 ns, or about 30%. It also contains level rates and outcome overrides.

Compatibility proof:

- Compare raw and normalized policy selection.
- Include empty aliases and canonical policy precedence.
- Include invalid keys and invalid levels.
- Include NaN, positive and negative infinity, and signed zero.
- Run the equivalence test repeatedly and under the race detector.

A prepared-config type is not needed. Removing the scan is smaller and keeps
the existing API.

### 3. Reserve capacity for completion fields

Every completed operation adds `duration_ms`, `op.code`, and `op.outcome`.
The start-field capacity hint currently counts only start fields. An operation
with all optional start fields grows the map during completion.

Implementation:

- Count the three fixed completion fields in the capacity hint.
- Keep the existing minimum capacity of eight.
- Allocate nine slots only when all four optional start fields are present.

Measured result:

- Full lifecycle improves from about 754 ns to 533 ns, or about 29%, when all
  compatible core changes are measured together.
- Allocation size falls from about 1,936 B to 1,648 B.
- Allocations fall from 15 to 14.
- Common smaller operations keep the same map capacity.

### 4. Clone fields only when a built-in sampler keeps the event

The finish path currently clones all fields before it knows whether built-in
sampling will drop the event. A dropped event does not need a sink snapshot.
The completed Event must still contain its final fields.

Implementation:

- Finalize the Event before sampling.
- Read the small scalar sampling input without cloning the field map.
- For built-in sampling, clone only after the event is kept.
- Preserve the current snapshot-before-callback behavior for custom samplers.
- Never call a sampler or sink while the Event lock is held.

Measured avoidable work on a dropped event:

| Field count | Before | After | Bytes before/after | Allocations before/after |
| ---: | ---: | ---: | ---: | ---: |
| 8 | about 731 ns | about 548 ns | 1,904 / 1,240 B | 13 / 9 |
| 32 | about 1,954 ns | about 1,639 ns | 7,152 / 4,760 B | 17 / 13 |

Compatibility proof:

- A dropped Event still exposes completion fields after `End`.
- Custom samplers still see a complete Event.
- Custom sampler mutation behavior remains unchanged.
- Concurrent Event access stays race-free.
- Sampling rates 0, 0.05, 0.5, and 1 get benchmark coverage.

### 5. Replace the contended sampler state with `math/rand/v2`

The current fractional sampler increments one global atomic value. Parallel
sampling makes that cache line a contention point. Go's standard runtime random
source avoids this shared atomic increment.

Implementation:

- Use `math/rand/v2` for fractional sampling.
- Return false before random generation when the rate is zero or NaN.
- Return true before random generation when the rate is at least one.
- Keep the public sampler signatures unchanged.

Measured result:

- The current parallel sampler costs about 41.6 ns per decision on the final
  comparison.
- `math/rand/v2` costs about 1.07 ns per parallel decision.
- The sampler is about 39 times faster under measured contention.
- Serial sampling changes from about 3.32 ns to 6.34 ns.
- Full lifecycle improvement is small when allocations dominate.

Decision:

Use the standard library implementation because request handling is normally
concurrent. Keep serial and parallel benchmarks to show the trade-off.

### 6. Gate adapters before field conversion

Disabled records do not need key sorting, pooled slices, reflection, or logger
field conversion.

Implementation:

- Zerolog: select one event and return when `Event.Enabled` is false.
- Zap: use `Logger.Check`, then write through the returned `CheckedEntry`.
- Slog: check the handler before conversion. Test the enabled path because
  `Logger.LogAttrs` performs its own enabled check.
- Skip field pools for zero-field events.
- Keep deterministic ordering for enabled records.

Measured result:

| Adapter | Disabled medium before | After | Improvement |
| --- | ---: | ---: | ---: |
| slog | about 168 ns | about 3.39 ns | about 50 times |
| zap | about 326 ns | about 4.78 ns | about 68 times |
| zerolog | about 265 ns | about 3.35 ns | about 79 times |

Enabled writes remained within benchmark noise or improved slightly.

Compatibility proof:

- Test every level.
- Test disabled Debug records.
- Test zap hooks, sampling, `AddCaller`, and `AddCallerSkip`.
- Test stateful slog handlers and dynamic level changes.
- Test deterministic order and nil fields.

## Small compatible cuts

These cuts are useful only when their tests remain simple:

- Add route and status with one `hc.Add` call.
- Read the Commit domain from the existing snapshot.
- Use the Event returned by `newEvent` directly in `BeginOperation`.
- Guard nil sinks before start-field hydration.
- Call `fmt.Stringer.String` once.
- Allocate error unwrap cycle tracking only after the first unwrap.

The HTTP status switch fast path is rejected. It saved about 0.33 ns and added
another branch. Cross-module adapter generation is also rejected. The modules
use different dependency versions, and generation would add more maintenance
than it removes.

## Outcome conflict reserved for v1

`resolveOutcome` currently accepts a valid explicit outcome before it examines
an error or recovered panic. A caller can therefore supply `OutcomeSuccess`
with an error. The emitted event has error metadata but can use the success
level.

The proposed v1 precedence is:

1. Recovered panic.
2. Error, including cancellation and timeout.
3. Explicit valid outcome.
4. Failure status code.
5. Success.

This must not be included in the compatible performance PR. Direct callers can
currently depend on explicit outcome precedence.

## v1 breaking-change plan

### A. Freeze events and borrow fields during sink writes

Change the sink contract from an owned `map[string]any` snapshot to a read-only
view that is valid for the duration of `Write`.

Expected result:

- Remove 2-4 allocations per kept event.
- Remove 336-2,392 B for the measured 8-32 field cases.
- Improve lifecycle time by an estimated 10-35%.

Migration requirements:

- Custom sinks must not retain the view.
- Post-finish Event mutation must be rejected or documented as invalid.
- Provide a migration adapter that clones for old sinks during the v1 preview.

### B. Sample before completion map writes

Remove `Event` from `SampleInput`. Pass final scalar metadata and selected fixed
fields instead. A dropped event can then skip completion boxing and snapshot
creation.

Expected result:

- Improve dropped-event time by an estimated 20-50%.
- Reduce dropped-event bytes by an estimated 40-60%.

Migration requirements:

- Custom samplers that inspect arbitrary Event fields must migrate.
- Document which final scalar values are available to samplers.
- Keep failure and panic retention semantics explicit.

### C. Use one lifecycle API

Keep `Operation` as the lifecycle owner. Remove or deprecate the parallel
`BeginOperation`, `FinishOperation`, and `OperationFinish` path after a preview
period.

Expected result:

- Remove about 100 lines of hydration and reconciliation logic.
- Remove repeated field validation and one or two locks per finish.
- Improve lifecycle time by an estimated 5-15%.

Migration requirements:

- Direct lifecycle users must retain `*Operation`.
- Integrations must use `Operation.End`.
- Provide a mechanical migration example in the v1 guide.

### D. Make Event ownership explicit

If v1 makes Events request-confined, remove internal mutexes. Callers that share
an Event must provide synchronization or clone it first.

Expected result:

- Improve normal lifecycle time by an estimated 5-15%.
- Remove lock contention from accidental shared use.

This option needs a separate design decision. Concurrent Event use is currently
tested and supported.

### E. Reduce duplicate output fields

HTTP and worker integrations emit some values in two namespaces. Examples are
`http.status` with `op.code`, `http.route` with `op.name`, and worker `op.*`
fields with matching `job.*` fields.

Expected result:

- Reduce adapter and encoding work by an estimated 5-15%.
- Reduce normal map sizes and serialized event size.

Migration requirements:

- Publish a field mapping table.
- Provide a compatibility window or dual-write option.
- Update dashboards, alerts, and query examples before removal.

### F. Consider typed fields only after the simpler work

A typed field representation can reduce boxing and adapter reflection. It also
changes `Add`, `EventFields`, `Sink`, adapters, and custom values. The migration
cost is high.

Expected result is an estimated 10-30% in adapter and core allocation work. This
estimate must be re-measured after sections A-E. Do not start this redesign if
the earlier changes make it unnecessary.

## v1 acceptance gates

Before a breaking design is selected:

- Benchmark 0, 8, 32, and 128 fields.
- Benchmark sampling rates 0, 0.05, 0.5, and 1.
- Benchmark 0, 1, 16, and 128 policies.
- Benchmark enabled and disabled adapter levels.
- Run all module tests and race tests.
- Verify failure, panic, cancellation, and timeout retention.
- Publish migration examples before removing old APIs or output fields.
