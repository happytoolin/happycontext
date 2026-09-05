package hc

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
)

// OperationStart describes operation metadata initialized at start.
type OperationStart struct {
	Domain               Domain // http | job | msg | cli | custom
	Name, ID, Source     string
	Attempt, MaxAttempts int
}

// Operation is the request lifecycle handle. Exactly one goroutine — the
// request's — drives it; End is one-shot.
type Operation struct {
	ctx   context.Context
	rt    *Runtime
	start OperationStart
	ref   walRef // embedded-by-value: the ctx points here, no extra alloc
	ev    *event

	// record is the pooled Record view for this operation's single
	// commit: embedding it replaces the per-commit &Record{...}
	// allocation with one that dies with the operation.
	record Record

	// endState is the one-shot claim word: 0 open, 1 claimed by the
	// winning End caller, 2 published (emitted is valid). Exactly one
	// caller CASes 0→1 and commits; the others wait for publication
	// (characterized by TestConcurrentEndCharacterization). emitted is
	// read only after observing 2, so the read is race-free.
	endState atomic.Uint32
	emitted  bool
}

// Start attaches a new request WAL to ctx and returns the operation
// handle. A nil *Runtime is valid: requests run, nothing emits.
func Start(ctx context.Context, rt *Runtime, start OperationStart) *Operation {
	if ctx == nil {
		ctx = context.Background()
	}
	ev := newEvent()
	op := &Operation{
		rt:    rt,
		start: normalizedOperationStart(start),
		ref:   walRef{ev: ev, gen: ev.state.Load() >> walStateBits},
		ev:    ev,
	}
	op.ctx = context.WithValue(ctx, contextKey{}, &op.ref)
	// Start metadata is appended lazily at End (annotatePostSeal), so
	// the live WAL carries no start fields during the request.
	return op
}

// Context returns the operation context — the ctx to pass down so the
// hc.Add helpers reach this operation's WAL.
func (op *Operation) Context() context.Context {
	if op == nil {
		return nil
	}
	return op.ctx
}

// End finalizes the operation from the deferred error pointer and panic
// state, committing exactly one event, and returns whether an event was
// emitted. One-shot: a second call is a no-op returning the first
// result (false, by definition, if the first call crashed mid-commit —
// a panic in the sampler or a sink never publishes an emission). If the surrounding function is panicking, End records the
// panic, commits, and re-panics.
//
// End MUST be deferred directly (defer op.End(&err)) — the closure form
// silently disables panic capture. Reentrant use is not supported: a
// second End from inside a sink's Write deadlocks on the one-shot
// claim. Concurrent first calls are safe (characterized by
// TestConcurrentEndCharacterization): exactly one wins, the rest
// return its published result.
func (op *Operation) End(errp *error) (emitted bool) {
	if op == nil {
		return false
	}
	// Claim the one-shot: published returns the cached result, claimed
	// means another caller is committing — wait for it.
	for {
		switch op.endState.Load() {
		case 2:
			return op.emitted // published: the read is race-free (see above)
		case 0:
			if op.endState.CompareAndSwap(0, 1) {
				goto claimed
			}
		}
		runtime.Gosched()
	}
claimed:
	// Publish on every exit path, including panics, so waiting callers
	// can never spin on a winner that died mid-commit.
	defer func() { op.endState.Store(2) }()
	defer func() { op.ev.release() }()

	var err error
	if errp != nil {
		err = *errp
	}
	recovered := recover()

	ev := op.ev
	start := op.start
	rt := op.rt

	// The owner's final writes, then SEAL before any WAL read: from
	// here on the event is immutable, so stragglers cannot race the
	// scan, the record handed to sinks, or the encode (amendment 20's
	// threat model is the sink-read window, which this closes).
	now := time.Now() // one clock read: completion stamp + duration base
	duration := now.Sub(ev.startedAt)
	annotateOperationFailures(ev, &op.ref, err, recovered)
	ev.seal()

	scan := scanWAL(ev)
	code := scan.code
	outcome := resolveOutcomeV2(err, recovered, code, scan.outcome)
	annotatePostSeal(ev, &op.ref, start, duration, normalizeDomain(start.Domain) == DomainHTTP, scan, outcome)

	emitted = op.commit(ev, rt, start, outcome, code, duration, now, err, recovered != nil, scan)
	op.emitted = emitted

	if recovered != nil {
		panic(recovered)
	}
	return emitted
}

// walScan is the single backward walk over the sealed WAL collecting
// everything End needs: explicit outcome, HTTP status, the sampler
// scalars (first — i.e. last-written — values win), and whether the
// request wrote any start-metadata key itself (lazy start fields must
// not clobber a user override — suppressing the canonical append
// reproduces the old LWW fold exactly).
type walScan struct {
	outcome    Outcome
	hasOutcome bool
	code       int // resolved http.status (outcome + sampling input)
	hasCode    bool
	opCode     int // explicit op.code field (non-HTTP operations)
	hasOpCode  bool
	method     string
	path       string

	hasDomain      bool // user wrote op.domain/op.name/... (any kind)
	hasName        bool
	name           string // last-write op.name (string), if any
	hasID          bool
	hasSource      bool
	hasAttempt     bool
	hasMaxAttempts bool
}

func scanWAL(ev *event) walScan {
	var s walScan
	for i := len(ev.fields) - 1; i >= 0; i-- {
		f := ev.fields[i]
		switch f.key {
		case "op.outcome":
			if !s.hasOutcome && f.kind == KindString {
				if o := Outcome(f.str); IsValidOutcome(o) {
					s.outcome = o
					s.hasOutcome = true
				}
			}
		case "http.status":
			if !s.hasCode && f.kind == KindInt {
				s.code = int(f.num)
				s.hasCode = true
			}
		case "op.code":
			if !s.hasOpCode && f.kind == KindInt {
				s.opCode = int(f.num)
				s.hasOpCode = true
			}
		case "http.method":
			if s.method == "" && f.kind == KindString {
				s.method = f.str
			}
		case "http.path":
			if s.path == "" && f.kind == KindString {
				s.path = f.str
			}
		case "op.domain":
			s.hasDomain = true
		case "op.name":
			s.hasName = true
			if s.name == "" && f.kind == KindString {
				s.name = f.str
			}
		case "op.id":
			s.hasID = true
		case "op.source":
			s.hasSource = true
		case "op.attempt":
			s.hasAttempt = true
		case "op.max_attempts":
			s.hasMaxAttempts = true
		}
	}
	return s
}

// commit resolves level, message, and sampling, then writes the record.
func (op *Operation) commit(ev *event, rt *Runtime, start OperationStart, outcome Outcome, code int, duration time.Duration, now time.Time, err error, panicked bool, scan walScan) bool {
	if rt.noop() {
		return false
	}

	policy := rt.policyFor(start.Domain)
	level := levelFloor(levelFromPolicy(policy, outcome), ev.requestedLevel, ev.hasRequestedLvl)

	in := buildSampleInput(ev, start, outcome, code, duration, level, err, panicked, scan)

	// The keep-everything fast path (rate == 1.0, no sampler, no level
	// rates, no policies): healthy events can never be dropped, so the
	// gate is skipped entirely. Error/panic events bypass the gate
	// structurally (amendment 4), so the flag only short-circuits the
	// healthy branch.
	if !rt.alwaysKeep {
		if !in.HasError {
			if rt.sampler != nil {
				if !rt.sampler(in) {
					return false
				}
			} else if !shouldWriteHealthy(rt, policy, in) {
				return false
			}
		}
	}

	msg := resolveEventMessage(rt.message, start.Domain, ev.msg)
	// Reset the lazy-encode cache: the record is embedded, so a stale
	// atomic pointer from a previous generation would otherwise serve
	// old bytes if the operation were ever committed again.
	rec := &op.record
	rec.level = level
	rec.msg = msg
	rec.fields = ev.fields
	rec.completedAt = now
	rec.encoded.Store(nil)
	rt.emit(op.ctx, rec)
	return true
}

// shouldWriteHealthy applies the compiled rate configuration for
// non-error events.
func shouldWriteHealthy(rt *Runtime, policy OperationPolicy, in SampleInput) bool {
	rate := rt.rate
	if policy.SamplingRate != nil {
		rate = *policy.SamplingRate
	} else if levelRate, ok := rt.levelRates[in.Level]; ok {
		rate = levelRate
	}
	return shouldSample(rate)
}

// applyOperationStartFields appends the normalized start metadata,
// lazily: End writes it post-seal so the record keeps the canonical
// start-metadata → completion order without the request paying for it
// while it runs. A key the request already wrote is skipped — appending
// later would flip the LWW fold and clobber the user's override (the
// route-name contract in the HTTP integrations depends on this).
func applyOperationStartFields(ev *event, ref *walRef, start OperationStart, scan walScan) {
	gen := ref.gen
	if !scan.hasDomain {
		ev.appendSealed(gen, fieldStr("op.domain", string(normalizeDomain(start.Domain))))
	}
	if !scan.hasName {
		ev.appendSealed(gen, fieldStr("op.name", start.Name))
	}
	if !scan.hasID && start.ID != "" {
		ev.appendSealed(gen, fieldStr("op.id", start.ID))
	}
	if !scan.hasSource && start.Source != "" {
		ev.appendSealed(gen, fieldStr("op.source", start.Source))
	}
	if !scan.hasAttempt && start.Attempt > 0 {
		ev.appendSealed(gen, fieldInt64("op.attempt", int64(start.Attempt)))
	}
	if !scan.hasMaxAttempts && start.MaxAttempts > 0 {
		ev.appendSealed(gen, fieldInt64("op.max_attempts", int64(start.MaxAttempts)))
	}
}

func annotateOperationFailures(ev *event, ref *walRef, err error, recovered any) {
	if recovered != nil {
		ev.appendAny(ref.gen, "panic", structuredPanicField(recovered))
	}
	if err != nil {
		ev.setError(ref, err)
	} else if recovered != nil {
		ev.setError(ref, fmt.Errorf("panic: %v", recovered))
	}
}

// annotatePostSeal appends the completion fields after sealing; the
// owner's post-seal writes cannot race stragglers. Start metadata
// joins first, keeping the wire's start-metadata → completion order
// with only the WAL position of the start fields moved.
//
// Canonical fields: HTTP operations carry http.status; non-HTTP
// operations surface their explicit op.code here (ledger: canonical
// fields).
func annotatePostSeal(ev *event, ref *walRef, start OperationStart, duration time.Duration, isHTTP bool, scan walScan, outcome Outcome) {
	gen := ref.gen
	applyOperationStartFields(ev, ref, start, scan)
	ev.appendSealed(gen, fieldInt64("duration_ms", duration.Milliseconds()))
	if !isHTTP && scan.hasOpCode {
		ev.appendSealed(gen, fieldInt64("op.code", int64(scan.opCode)))
	}
	ev.appendSealed(gen, fieldStr("op.outcome", string(outcome)))
}

// resolveOutcomeV2 applies the v2 precedence: panic > error > explicit
// (a valid op.outcome the caller wrote) > 5xx > success.
func resolveOutcomeV2(err error, recovered any, code int, explicit Outcome) Outcome {
	if recovered != nil {
		return OutcomePanic
	}
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return OutcomeCanceled
		case errors.Is(err, context.DeadlineExceeded):
			return OutcomeTimeout
		default:
			return OutcomeFailure
		}
	}
	if explicit != "" {
		return explicit
	}
	if code >= 500 {
		return OutcomeFailure
	}
	return OutcomeSuccess
}

func buildSampleInput(ev *event, start OperationStart, outcome Outcome, code int, duration time.Duration, level Level, err error, panicked bool, scan walScan) SampleInput {
	hasError := err != nil || panicked || ev.hasErr || outcome != OutcomeSuccess
	opName := start.Name
	if scan.name != "" {
		opName = scan.name
	}
	// HTTP samplers see http.status; non-HTTP samplers see their
	// canonical op.code (the README's non-HTTP contract for Code).
	// StatusCode stays the HTTP-compat view of http.status in both.
	samplerCode := code
	if normalizeDomain(start.Domain) != DomainHTTP && scan.hasOpCode {
		samplerCode = scan.opCode
	}
	in := SampleInput{
		Domain:     normalizeDomain(start.Domain),
		Operation:  opName,
		Outcome:    outcome,
		Code:       samplerCode,
		StatusCode: code,
		Method:     scan.method,
		Path:       scan.path,
		Duration:   duration,
		Level:      level,
		HasError:   hasError,
		ev:         ev,
	}
	return in
}

func resolveEventMessage(configured string, domain Domain, eventMessage string) string {
	if eventMessage != "" {
		return eventMessage
	}
	if configured != "" {
		return configured
	}
	if normalizeDomain(domain) == DomainHTTP {
		return DefaultMessage
	}
	return DefaultOperationMessage
}

const defaultDomainValue Domain = "operation"

const defaultOpName = "operation"

func normalizedOperationStart(start OperationStart) OperationStart {
	start.Domain = normalizeDomain(start.Domain)
	if start.Name == "" {
		start.Name = defaultOpName
	}
	return start
}
