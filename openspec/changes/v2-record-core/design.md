# Design: v2-record-core

The complete technical design is `V2_DESIGN.md` at the repo root — this
file only indexes the load-bearing decisions an implementer needs at hand.

## Storage
- Event = per-request in-memory WAL: `[]Field` typed records (~64 B each),
  pooled, request-confined, one writer. Never a map, never a pre-encoded
  buffer (§7 ledger).
- Sealing (amendment 20): after `End`, mutations are no-ops — pooled
  buffers must never be written post-recycle.
- Arming (amendment 1): every append does one atomic flag load; armed
  events append under a per-event mutex; the watchdog snapshots under the
  same mutex. "Fast path never touches a mutex" is only true *unarmed*.
- Duplicates: pure append; last-write-wins resolved at encode with an
  on-demand seen-set (amendment 3). No Add-time scan.

## Read side
- `Record`/`Field` follow `slog.Record`/`slog.Value` conventions
  (amendment 18). `Encoded()` is lazy-once via atomic publish
  (amendment 6). Sinks are concurrency-safe (amendment 2).

## Construction
- `Compile`/`MustCompile` → immutable `*Runtime` (amendment 13); error
  contract with sentinels (amendment 17); nil sink = no-op runtime.

## Output
- First-party JSON = zerolog shape (lowercase level, `message`, RFC3339
  completion time — amendment 5). Golden gate is parsed field-set
  equivalence, never byte equality (v0 map order is random).

## Performance gates
- `V2_DESIGN.md` §4. Standing rule: no benchmark counts until the property
  test passes; parallel results require interleaved reruns to be believed
  (see `V2_PLAN.md` §05's cautionary tale for why).

## Mechanics on the branch
- Nested modules use `replace … => ../` during development; the lockstep
  scripts restore published-requirement form at cutover.
- The `happycontext.test` stray binary seen in the worktree is a local
  artifact (gitignored) — never commit it.
