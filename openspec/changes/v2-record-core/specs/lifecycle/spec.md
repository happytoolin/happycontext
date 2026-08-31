# lifecycle Specification (delta)

## ADDED Requirements

### Requirement: Single lifecycle
`hc.Start(ctx, rt, start)` SHALL create the operation and `op.End(&err)`
SHALL be the only finalization path, capturing the deferred error and
panic state, committing once, and re-panicking after a recovered panic.

#### Scenario: Deferred completion
- GIVEN a function with `op := hc.Start(...)` and `defer op.End(&err)`
- WHEN the function returns an error
- THEN one event is emitted with `op.outcome` derived by
  panic > error > explicit > 5xx > success

#### Scenario: One-shot End
- GIVEN an `End` that already committed
- WHEN `End` is called again
- THEN it is a no-op returning the first result and the pool state is intact

### Requirement: Sampler input
`SampleInput` SHALL expose finalized scalars plus `Lookup` and `Fields()`;
error and panic events SHALL bypass rate sampling before any custom
sampler runs.

#### Scenario: Errors never sampled away
- GIVEN `Sampler: NeverSampler()` and a failing request
- WHEN the event finalizes
- THEN the event is still emitted

#### Scenario: Field lookup in sampler
- GIVEN a custom sampler reading `in.Lookup("user_tier")`
- WHEN the tier is "enterprise"
- THEN the sampler's keep decision applies
