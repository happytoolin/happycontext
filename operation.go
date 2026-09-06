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
	// Domain is the operation category, one of the Domain constants;
	// the zero value defaults to the "operation" domain.
	Domain               Domain
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
// a panic in the sampler or a sink never publishes an emission). If the
// surrounding function is panicking, End records the panic, commits,
// and re-panics.
//
// End MUST be deferred directly (defer op.End(&err)) — the closure form
// silently disables panic capture. Reentrant use is not supported: a
// second End from inside a sink's Write deadlocks on the one-shot
// claim. Concurrent first calls are safe: exactly one wins the claim
// and commits; the others wait and return the published result.
func (op *Operation) End(errp *error) (emitted bool) {
	// A nil *Operation and the zero Operation are both no-ops: the zero
	// value carries no event, so there is nothing to commit. This keeps
	// the zero value from dereferencing a nil event in release, matching
	// the nil-guards on Context.
	if op == nil || op.ev == nil {
		return false
	}
	if !op.claim() {
		return op.emitted // published by the winning caller (race-free, see endState)
	}
	// Publish on every exit path, including panics, so waiting callers
	// can never spin on a winner that died mid-commit. LIFO order: the
	// event is released (sealed + pooled) first, then the claim word
	// publishes — waiters only read Operation fields, never the pooled
	// event, so recycling before publication is safe.
	defer func() { op.endState.Store(2) }()
	defer func() { op.ev.release() }()

	var err error
	if errp != nil {
		err = *errp
	}
	recovered := recover()

	ev := op.ev
	start := op.start

	// The owner's final writes, then SEAL before any WAL read: from
	// here on the event is immutable, so stragglers cannot race the
	// scan, the record handed to sinks, or the encode.
	now := time.Now() // one clock read: completion stamp + duration base
	duration := now.Sub(ev.startedAt)
	annotateOperationFailures(ev, &op.ref, err, recovered)
	ev.seal()

	scan := scanWAL(ev)
	code := scan.code
	isHTTP := start.Domain == DomainHTTP
	// The canonical code drives the 5xx outcome rule: http.status for
	// HTTP, op.code for everything else (a non-HTTP op's http.status is
	// user data, not canonical) — so a job surfacing failure via
	// op.code >= 500 resolves failure (and bypasses sampling) exactly
	// like its HTTP twin, instead of logging a self-contradictory
	// op.code=503 + op.outcome=success line.
	outcomeCode := 0
	if isHTTP {
		outcomeCode = code
	} else if scan.hasOpCode {
		outcomeCode = scan.opCode
	}
	outcome := resolveOutcome(err, recovered, outcomeCode, scan.outcome)

	in := commitInput{
		outcome:  outcome,
		code:     code,
		duration: duration,
		now:      now,
		err:      err,
		panicked: recovered != nil,
		scan:     scan,
	}
	op.annotatePostSeal(in)

	emitted = op.commit(in)
	op.emitted = emitted

	if recovered != nil {
		panic(recovered)
	}
	return emitted
}

// claim acquires the one-shot commit right (endState 0 → 1 by CAS).
// A caller that observes the state claimed (1) or published (2) waits
// for the winner's publication — which every exit path of the
// winner's End performs, including panics — and then reports false;
// End reads the published result in that case.
func (op *Operation) claim() bool {
	for {
		switch op.endState.Load() {
		case 2:
			return false
		case 0:
			if op.endState.CompareAndSwap(0, 1) {
				return true
			}
		}
		runtime.Gosched()
	}
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
		case KeyOpOutcome:
			if !s.hasOutcome && f.kind == KindString {
				if o := Outcome(f.str); IsValidOutcome(o) {
					s.outcome = o
					s.hasOutcome = true
				}
			}
		case KeyHTTPStatus:
			if !s.hasCode && f.kind == KindInt {
				s.code = int(f.num)
				s.hasCode = true
			}
		case KeyOpCode:
			if !s.hasOpCode && f.kind == KindInt {
				s.opCode = int(f.num)
				s.hasOpCode = true
			}
		case KeyHTTPMethod:
			if s.method == "" && f.kind == KindString {
				s.method = f.str
			}
		case KeyHTTPPath:
			if s.path == "" && f.kind == KindString {
				s.path = f.str
			}
		case KeyOpDomain:
			s.hasDomain = true
		case KeyOpName:
			s.hasName = true
			if s.name == "" && f.kind == KindString {
				s.name = f.str
			}
		case KeyOpID:
			s.hasID = true
		case KeyOpSource:
			s.hasSource = true
		case KeyOpAttempt:
			s.hasAttempt = true
		case KeyOpMaxAttempts:
			s.hasMaxAttempts = true
		}
	}
	return s
}

// commitInput bundles what End resolved about the completed operation
// for the commit stage — everything that is not already reachable from
// the Operation itself (ev, rt, start, ctx, record). One struct instead
// of a ten-parameter call, and the natural home for these semantics:
// all of it describes the sealed event between seal and commit.
type commitInput struct {
	outcome  Outcome // resolved outcome (panic > error > explicit > 5xx > success)
	code     int     // resolved http.status (the canonical code for HTTP operations)
	duration time.Duration
	now      time.Time // completion stamp (single clock read from End)
	err      error
	panicked bool
	scan     walScan
}

// commit resolves level, message, and sampling, then writes the record.
func (op *Operation) commit(in commitInput) bool {
	rt := op.rt
	if rt.noop() {
		return false
	}

	ev := op.ev
	start := op.start
	policy := rt.policyFor(start.Domain)
	level := levelFloor(levelFromPolicy(policy, in.outcome), ev.requestedLevel, ev.hasRequestedLvl)

	sampleIn := buildSampleInput(ev, start, in, level)

	// The keep-everything fast path (rate == 1.0, no sampler, no level
	// rates, no policies): healthy events can never be dropped, so the
	// gate is skipped entirely. Error/panic events bypass the gate
	// structurally, so the flag only short-circuits the healthy branch.
	if !rt.alwaysKeep {
		if !sampleIn.HasError {
			if rt.sampler != nil {
				if !rt.sampler(sampleIn) {
					return false
				}
			} else if !shouldWriteHealthy(rt, policy, sampleIn) {
				return false
			}
		}
	}

	rec := &op.record
	rec.level = level
	rec.msg = resolveEventMessage(rt.message, start.Domain, ev.msg)
	rec.fields = ev.fields
	rec.completedAt = in.now
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
func appendStartFields(start OperationStart, scan walScan, add func(Field)) {
	if !scan.hasDomain {
		add(fieldStr(KeyOpDomain, string(start.Domain)))
	}
	if !scan.hasName {
		add(fieldStr(KeyOpName, start.Name))
	}
	if !scan.hasID && start.ID != "" {
		add(fieldStr(KeyOpID, start.ID))
	}
	if !scan.hasSource && start.Source != "" {
		add(fieldStr(KeyOpSource, start.Source))
	}
	if !scan.hasAttempt && start.Attempt > 0 {
		add(fieldInt64(KeyOpAttempt, int64(start.Attempt)))
	}
	if !scan.hasMaxAttempts && start.MaxAttempts > 0 {
		add(fieldInt64(KeyOpMaxAttempts, int64(start.MaxAttempts)))
	}
}

func annotateOperationFailures(ev *event, ref *walRef, err error, recovered any) {
	if recovered != nil {
		ev.appendAny(ref.gen, KeyPanic, structuredPanicField(recovered))
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
func (op *Operation) annotatePostSeal(in commitInput) {
	ev := op.ev
	// The owner is the only writer past the seal, so the state and the
	// generation are stable across this entire block (release is
	// deferred to post-commit, reset requires a pool round-trip): one
	// armed check and one lock/unlock bracket replace the per-append
	// atomic-load-and-maybe-mutex round trip of a per-field sealed
	// append — ~9 atomic loads and N indirect calls saved per request.
	armed := walState(ev.state.Load()&walStateMask) == walSealedArmed
	if armed {
		ev.mu.Lock()
		defer ev.mu.Unlock()
	}

	fields := ev.fields
	add := func(f Field) { fields = append(fields, f) }
	appendStartFields(op.start, in.scan, add)
	add(fieldInt64(KeyDurationMS, in.duration.Milliseconds()))
	if op.start.Domain != DomainHTTP && in.scan.hasOpCode {
		add(fieldInt64(KeyOpCode, int64(in.scan.opCode)))
	}
	add(fieldStr(KeyOpOutcome, string(in.outcome)))
	ev.fields = fields
}

// resolveOutcome applies the precedence: panic > error > explicit
// (a valid op.outcome the caller wrote) > 5xx > success.
func resolveOutcome(err error, recovered any, code int, explicit Outcome) Outcome {
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

func buildSampleInput(ev *event, start OperationStart, in commitInput, level Level) SampleInput {
	hasError := in.err != nil || in.panicked || ev.hasErr || in.outcome != OutcomeSuccess
	opName := start.Name
	if in.scan.name != "" {
		opName = in.scan.name
	}
	// HTTP samplers see http.status; non-HTTP samplers see their
	// canonical op.code (the README's non-HTTP contract for Code).
	// StatusCode stays the HTTP-compat view of http.status in both.
	samplerCode := in.code
	if start.Domain != DomainHTTP && in.scan.hasOpCode {
		samplerCode = in.scan.opCode
	}
	return SampleInput{
		Domain:     start.Domain,
		Operation:  opName,
		Outcome:    in.outcome,
		Code:       samplerCode,
		StatusCode: in.code,
		Method:     in.scan.method,
		Path:       in.scan.path,
		Duration:   in.duration,
		Level:      level,
		HasError:   hasError,
		ev:         ev,
	}
}

func resolveEventMessage(configured string, domain Domain, eventMessage string) string {
	if eventMessage != "" {
		return eventMessage
	}
	if configured != "" {
		return configured
	}
	if domain == DomainHTTP {
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
