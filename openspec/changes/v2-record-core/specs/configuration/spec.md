# configuration Specification (delta)

## ADDED Requirements

### Requirement: Compile once
Configuration SHALL be compiled once via `Compile(cfg) (*Runtime, error)`
or `MustCompile(cfg) *Runtime`; integrations take `*Runtime`, never
`Config`. The Runtime SHALL be immutable and shared by all requests.

#### Scenario: Invalid rate
- GIVEN `SamplingRate: 1.5`
- WHEN `Compile` runs
- THEN it returns an error wrapping `ErrInvalidRate` with an `"hc: "` prefix

#### Scenario: Nil sink
- GIVEN `Config` with no sink
- WHEN compiled
- THEN the runtime is valid and emits nothing

### Requirement: Level type
`Level` SHALL be an int-backed type whose `String()` renders the classic
names; wire output SHALL be unchanged.

#### Scenario: Constant compatibility
- GIVEN v0 code using `hc.LevelWarn`
- WHEN recompiled against v2
- THEN the constant reference compiles and behaves identically
