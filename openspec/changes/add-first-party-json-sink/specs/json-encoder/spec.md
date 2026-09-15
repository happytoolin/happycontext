# json-encoder Specification (delta)

## ADDED Requirements

### Requirement: Escaping correctness
The encoder SHALL produce JSON byte-identical to the vendored zerolog
reference implementation for every input, verified by property test.

#### Scenario: Random byte strings
- GIVEN 200,000 randomly generated strings including control bytes,
  quotes, backslashes, DEL, invalid UTF-8, and multi-byte runes
- WHEN each is encoded by the SWAR fast path and by the zerolog-table
  reference
- THEN the output bytes are identical

#### Scenario: Broken zero-detector regression
- GIVEN a zero-byte detector missing the `&^x` term (the class of bug that
  once made the encoder silently skip escaping)
- WHEN the property test runs
- THEN it fails

### Requirement: Fast path gating
The SWAR chunk scan SHALL engage only for strings of length ≥ 16 and SHALL
fall back to the table path on any suspicious chunk.

#### Scenario: Short strings
- GIVEN a 7-character field key
- WHEN encoded
- THEN the table path is used and output matches the reference

### Requirement: First-party sink
`NewJSONSink(w io.Writer)` SHALL emit one JSON line per event with a single
`Write` call, adding no third-party dependency to the root module.

#### Scenario: Output shape
- GIVEN a kept event with level error and completion time set
- WHEN the sink writes
- THEN the line matches the v0.5 zerolog adapter's field set: lowercase
  level, `message` key, RFC3339 time

#### Scenario: Performance gate
- GIVEN the benches 12-field medium case
- WHEN benchmarked
- THEN ns/op ≤ 400 and allocs/op ≤ 2
