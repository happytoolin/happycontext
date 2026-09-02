package hc

import (
	"sync"
	"sync/atomic"
	"time"
)

// The WAL state word packs a generation counter (high bits) and a state
// (low bits). Mutations perform one atomic load and compare both: the
// generation defeats the recycle ABA (a straggler holding a recycled
// event sees a stale generation and no-ops even after the event has been
// reset for a new request), and the state implements sealing and arming.
const (
	walStateBits = 2
	walStateMask = uint64(1<<walStateBits) - 1
	walGenOne    = uint64(1) << walStateBits
)

type walState uint64

const (
	walLive        walState = iota // unarmed fast path: pure append
	walArmed                       // watchdog armed: appends serialize under mu
	walSealed                      // committed or dropped: mutations are no-ops
	walSealedArmed                 // sealed, but WAS armed: owner post-seal
	// writes and (future) watchdog snapshots still serialize under mu
)

func packState(gen uint64, state walState) uint64 { return gen | uint64(state) }

// event is the per-request write-ahead log: an append-only slice of typed
// fields, request-confined, pooled. One writer (the request goroutine) on
// the unarmed fast path; armed events serialize appends and snapshots
// under mu (amendment 1). After End the event is sealed — straggler
// writes from async work must never touch a recycled buffer (amendment
// 20), which the generation check guarantees even after the pool hands
// the event to a new request.
type event struct {
	state atomic.Uint64

	mu sync.Mutex // serializes appends/snapshots/sealing while armed

	fields []Field // append-only, insertion order; backing array owned for pooling
	msg    string
	hasMsg bool

	hasErr          bool
	requestedLevel  Level
	hasRequestedLvl bool

	startedAt time.Time
}

var eventPool = sync.Pool{
	New: func() any {
		return &event{
			fields: make([]Field, 0, 16),
		}
	},
}

// walRef is the immutable handle stored in the request context; it pins
// the generation the request's writes belong to.
type walRef struct {
	ev  *event
	gen uint64
}

func newEvent() *event {
	ev := eventPool.Get().(*event)
	ev.reset()
	return ev
}

func (e *event) reset() {
	s := e.state.Add(walGenOne) // new generation; any straggler now mismatches
	e.state.Store(s&^walStateMask | uint64(walLive))
	e.fields = e.fields[:0]
	e.msg = ""
	e.hasMsg = false
	e.hasErr = false
	e.requestedLevel = 0
	e.hasRequestedLvl = false
	e.startedAt = time.Now()
}

// seal ends all mutations for this generation. Armed events seal under
// the mutex so an in-flight guarded append either lands before the seal
// or observes it and drops.
func (e *event) seal() {
	for {
		s := e.state.Load()
		switch walState(s & walStateMask) {
		case walSealed, walSealedArmed:
			return
		case walArmed:
			e.mu.Lock()
			e.state.Store(s&^walStateMask | uint64(walSealedArmed))
			e.mu.Unlock()
			return
		default:
			if e.state.CompareAndSwap(s, s&^walStateMask|uint64(walSealed)) {
				return
			}
		}
	}
}

// arm switches the live event to guarded mode: appends and snapshots
// serialize under the per-event mutex. The watchdog (v1.1) arms stalled
// requests; the protocol ships in the core now so arming is never a
// breaking change.
func (e *event) arm() {
	e.mu.Lock()
	if s := e.state.Load(); walState(s&walStateMask) == walLive {
		e.state.CompareAndSwap(s, s&^walStateMask|uint64(walArmed))
	}
	e.mu.Unlock()
}

// append adds one field for the given generation. One atomic load decides
// the path: stale generation or sealed drops the write, armed serializes
// under the mutex (rechecking there, since sealing also takes the mutex),
// live appends directly on the request-confined fast path.
//
// Residual (accepted by the design, amendments 1/20): a straggler that
// loads state as live and is then preempted across End+seal+recycle can
// still complete its append into the recycled buffer — a nanosecond-scale
// torn window inherent to the single-load protocol. The WAL is
// request-confined (one writer); stragglers are only guaranteed no-ops
// for writes initiated after End seals.
func (e *event) append(gen uint64, f Field) {
	s := e.state.Load()
	if s>>walStateBits != gen {
		return // stale generation: the event was released and reused
	}
	switch walState(s & walStateMask) {
	case walSealed, walSealedArmed:
		return // stragglers never write post-seal; only the owner does
	case walArmed:
		e.mu.Lock()
		if cur := e.state.Load(); cur>>walStateBits == gen && walState(cur&walStateMask) == walArmed {
			e.fields = append(e.fields, f)
		}
		e.mu.Unlock()
	default:
		e.fields = append(e.fields, f)
	}
}

// addKV appends the leading pair plus any well-formed kv pairs
// (k, v, k, v, ...). Malformed tails are skipped silently; the first
// key is positional and cannot be malformed (amendment 19).
func (e *event) addKV(ref *walRef, key string, value any, kv ...any) {
	e.append(ref.gen, fieldOf(key, value))
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok || k == "" {
			continue
		}
		e.append(ref.gen, fieldOf(k, kv[i+1]))
	}
}

// appendSealed is the owner's post-seal write: same generation check
// against torn recycling; the state check accepts sealed (by
// construction the owner performs these after sealing). Events that
// were armed keep serializing under mu so watchdog snapshots taken
// during the seal window can never race the owner's final appends.
func (e *event) appendSealed(gen uint64, f Field) {
	s := e.state.Load()
	if s>>walStateBits != gen {
		return // event recycled under us: not our buffer anymore
	}
	if walState(s&walStateMask) == walSealedArmed {
		e.mu.Lock()
		defer e.mu.Unlock()
	}
	e.fields = append(e.fields, f)
}

// Typed append helpers for the canonical fields: no interface boundary,
// no boxing, unlike fieldOf.
func (e *event) appendStr(gen uint64, key, value string) {
	e.append(gen, Field{key: key, kind: KindString, str: value})
}

func (e *event) appendInt64(gen uint64, key string, value int64) {
	e.append(gen, Field{key: key, kind: KindInt, num: value})
}

func (e *event) appendAny(gen uint64, key string, value any) {
	e.append(gen, Field{key: key, kind: KindAny, val: value})
}

func (e *event) setRaw(ref *walRef, key string, raw []byte) {
	e.append(ref.gen, Field{key: key, kind: KindRaw, val: raw})
}

// setError records the structured error field and latches hasErr.
// Armed events serialize the append + latch under mu so a concurrent
// seal cannot split them (the P5 matrix pins the discipline).
func (e *event) setError(ref *walRef, err error) {
	if err == nil {
		return
	}
	s := e.state.Load()
	if s>>walStateBits != ref.gen {
		return
	}
	switch walState(s & walStateMask) {
	case walArmed:
		e.mu.Lock()
		defer e.mu.Unlock()
		if cur := e.state.Load(); cur>>walStateBits == ref.gen && walState(cur&walStateMask) == walArmed {
			e.fields = append(e.fields, Field{key: "error", kind: KindAny, val: structuredErrorField(err)})
			e.hasErr = true // only when the write belonged to this generation
		}
	case walLive:
		e.fields = append(e.fields, Field{key: "error", kind: KindAny, val: structuredErrorField(err)})
		e.hasErr = true
	}
}

// setMessage overrides the event message. Only walLive and walArmed
// are mutable: walSealedArmed is sealed — the owner's post-seal writes
// go through appendSealed under mu, never through the setter family —
// so a setter landing on a sealedArmed event would be a post-seal
// straggler write (the P5 matrix pins this). Armed events write under
// mu.
func (e *event) setMessage(ref *walRef, msg string) {
	if msg == "" {
		return
	}
	s := e.state.Load()
	if s>>walStateBits != ref.gen {
		return
	}
	switch walState(s & walStateMask) {
	case walArmed:
		e.mu.Lock()
		defer e.mu.Unlock()
		if cur := e.state.Load(); cur>>walStateBits == ref.gen && walState(cur&walStateMask) == walArmed {
			e.msg = msg
			e.hasMsg = true
		}
	case walLive:
		e.msg = msg
		e.hasMsg = true
	}
}

func (e *event) setRoute(ref *walRef, route string) {
	if route == "" {
		return
	}
	e.appendStr(ref.gen, "http.route", route)
}

// setLevel records the requested level floor. Same liveness rules and
// armed-mu discipline as setMessage.
func (e *event) setLevel(ref *walRef, level Level) {
	if !IsValidLevel(level) {
		return
	}
	s := e.state.Load()
	if s>>walStateBits != ref.gen {
		return
	}
	switch walState(s & walStateMask) {
	case walArmed:
		e.mu.Lock()
		defer e.mu.Unlock()
		if cur := e.state.Load(); cur>>walStateBits == ref.gen && walState(cur&walStateMask) == walArmed {
			e.requestedLevel = level
			e.hasRequestedLvl = true
		}
	case walLive:
		e.requestedLevel = level
		e.hasRequestedLvl = true
	}
}

// snapshotFields returns a copy of the current WAL tail for an armed
// event's watchdog read; it shares the append mutex so snapshots are
// race-clean against concurrent guarded appends.
func (e *event) snapshotFields() []Field {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Field, len(e.fields))
	copy(out, e.fields)
	return out
}

// lookup returns the last value written under key (last-write-wins view
// of the un-deduped WAL).
func (e *event) lookup(key string) (any, bool) {
	for i := len(e.fields) - 1; i >= 0; i-- {
		if e.fields[i].key == key {
			return valueOf(e.fields[i]), true
		}
	}
	return nil, false
}

func (e *event) release() {
	e.seal()
	if cap(e.fields) <= 1024 {
		eventPool.Put(e)
	}
}
