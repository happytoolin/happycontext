package hc

import (
	"context"
	"errors"
	"fmt"
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
	ctx     context.Context
	rt      *Runtime
	start   OperationStart
	ref     walRef // embedded-by-value: the ctx points here, no extra alloc
	ev      *event
	done    bool
	emitted bool
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
	applyOperationStartFields(ev, op.ref.gen, op.start)
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
// emitted (kept by the sampler and written). It is one-shot: a second
// call is a no-op returning the first result. If the surrounding
// function is panicking, End records the panic, commits, and re-panics.
//
// End MUST be deferred directly — recover() only observes a panic when
// called by the directly-deferred function:
//
//	defer op.End(&err)
//
// The closure form (defer func() { op.End(&err) }()) compiles but
// silently disables panic capture. Note also that a sink which panics
// during Write replaces an in-flight original panic.
func (op *Operation) End(errp *error) (emitted bool) {
	if op == nil || op.done {
		if op == nil {
			return false
		}
		return op.emitted
	}
	op.done = true
	defer func() { op.ev.release() }()

	var err error
	if errp != nil {
		err = *errp
	}
	recovered := recover()

	ev := op.ev
	start := op.start
	rt := op.rt

	// The owner's final writes, then SEAL before any WAL read: from here
	// on the event is immutable, so stragglers cannot race the scan, the
	// record handed to sinks, or the encode (amendment 20's threat model
	// is the sink-read window, which this closes; release() re-seals
	// idempotently before pooling).
	code := 0
	outcome := OutcomeSuccess
	now := time.Now() // one clock read: completion stamp + duration base
	duration := now.Sub(ev.startedAt)
	annotateOperationFailures(ev, &op.ref, err, recovered)
	ev.seal()

	scan := scanWAL(ev)
	code = scan.code
	outcome = resolveOutcomeV2(err, recovered, code, scan.outcome)
	annotatePostSeal(ev, &op.ref, duration, code, outcome)

	emitted = op.commit(ev, rt, start, outcome, code, duration, now, err, recovered != nil, scan)
	op.emitted = emitted

	if recovered != nil {
		panic(recovered)
	}
	return emitted
}

// walScan is the single backward walk over the sealed WAL collecting
// everything End needs: explicit outcome, HTTP status, and the sampler
// scalars (first — i.e. last-written — values win).
type walScan struct {
	outcome    Outcome
	hasOutcome bool
	code       int
	hasCode    bool
	method     string
	path       string
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
		case "http.method":
			if s.method == "" && f.kind == KindString {
				s.method = f.str
			}
		case "http.path":
			if s.path == "" && f.kind == KindString {
				s.path = f.str
			}
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

	// Amendment 4: error and panic bypass is structural — decided before
	// any custom sampler runs, so failures are never sampled away.
	if !in.HasError {
		if rt.sampler != nil {
			if !rt.sampler(in) {
				return false
			}
		} else if !shouldWriteHealthy(rt, policy, in) {
			return false
		}
	}

	msg := resolveEventMessage(rt.message, start.Domain, ev.msg)
	rec := &Record{
		level:       level,
		msg:         msg,
		fields:      ev.fields,
		completedAt: now,
	}
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

func applyOperationStartFields(ev *event, gen uint64, start OperationStart) {
	ev.appendStr(gen, "op.domain", string(normalizeDomain(start.Domain)))
	ev.appendStr(gen, "op.name", start.Name)
	if start.ID != "" {
		ev.appendStr(gen, "op.id", start.ID)
	}
	if start.Source != "" {
		ev.appendStr(gen, "op.source", start.Source)
	}
	if start.Attempt > 0 {
		ev.appendInt64(gen, "op.attempt", int64(start.Attempt))
	}
	if start.MaxAttempts > 0 {
		ev.appendInt64(gen, "op.max_attempts", int64(start.MaxAttempts))
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

// annotatePostSeal appends the completion fields after sealing. The
// writes belong to the owner (the request goroutine) and cannot race
// stragglers: everything else is already sealed off.
func annotatePostSeal(ev *event, ref *walRef, duration time.Duration, code int, outcome Outcome) {
	gen := ref.gen
	ev.appendSealed(gen, fieldInt64("duration_ms", duration.Milliseconds()))
	if code != 0 {
		ev.appendSealed(gen, fieldInt64("op.code", int64(code)))
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
	in := SampleInput{
		Domain:     normalizeDomain(start.Domain),
		Operation:  start.Name,
		Outcome:    outcome,
		Code:       code,
		StatusCode: code,
		Method:     scan.method,
		Path:       scan.path,
		Duration:   duration,
		Level:      level,
		HasError:   hasError,
		ev:         ev,
	}
	if scan.hasCode {
		in.StatusCode = scan.code
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
