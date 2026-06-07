package hc

import (
	"sync"
	"sync/atomic"
)

const defaultAsyncSinkBuffer = 1024

// AsyncSinkOptions controls asynchronous sink behavior.
type AsyncSinkOptions struct {
	// Buffer is the channel capacity for pending events.
	// Values <= 0 use a default buffer.
	Buffer int

	// DropWhenFull makes Write non-blocking when the buffer is full.
	DropWhenFull bool

	// OnDrop is called when DropWhenFull drops an event.
	OnDrop func(DroppedEvent)
}

// DroppedEvent describes an event dropped by AsyncSink.
type DroppedEvent struct {
	Level   Level
	Message string
	Fields  map[string]any
}

type asyncEntry struct {
	level     Level
	message   string
	fields    map[string]any
	fieldList []BorrowedField
	duration  int64
	code      int
	outcome   Outcome
	start     OperationStart
	operation bool
	complete  bool
	flush     chan struct{}
}

// AsyncSink writes events on a background goroutine.
type AsyncSink struct {
	sink         Sink
	entries      chan asyncEntry
	done         chan struct{}
	dropWhenFull bool
	onDrop       func(DroppedEvent)

	mu      sync.RWMutex
	closed  bool
	wg      sync.WaitGroup
	dropped atomic.Uint64
}

// NewAsyncSink wraps sink with a buffered asynchronous writer.
func NewAsyncSink(sink Sink, opts AsyncSinkOptions) *AsyncSink {
	buffer := opts.Buffer
	if buffer <= 0 {
		buffer = defaultAsyncSinkBuffer
	}

	async := &AsyncSink{
		sink:         sink,
		entries:      make(chan asyncEntry, buffer),
		done:         make(chan struct{}),
		dropWhenFull: opts.DropWhenFull,
		onDrop:       opts.OnDrop,
	}
	async.wg.Add(1)
	go async.run()
	return async
}

// Write implements Sink.
func (a *AsyncSink) Write(level Level, message string, fields map[string]any) {
	if a == nil || a.sink == nil {
		return
	}

	fields = copyFieldMap(fields)
	entry := asyncEntry{
		level:   level,
		message: message,
		fields:  fields,
	}

	a.enqueue(entry, func() map[string]any { return copyFieldMap(fields) })
}

// WriteFields implements fieldSink by copying borrowed fields for background use.
func (a *AsyncSink) WriteFields(level Level, message string, fields []Field) {
	if a == nil || a.sink == nil {
		return
	}

	entry := asyncEntry{
		level:     level,
		message:   message,
		fieldList: borrowedFieldsFromFields(fields),
	}

	a.enqueue(entry, func() map[string]any { return mapFromFieldList(entry.fieldList) })
}

// WriteBorrowedFields implements borrowedFieldSink by copying borrowed fields for background use.
func (a *AsyncSink) WriteBorrowedFields(level Level, message string, fields []BorrowedField) {
	if a == nil || a.sink == nil {
		return
	}

	cp := make([]BorrowedField, len(fields))
	copy(cp, fields)

	entry := asyncEntry{
		level:     level,
		message:   message,
		fieldList: cp,
	}

	a.enqueue(entry, func() map[string]any { return mapFromFieldList(entry.fieldList) })
}

// WriteFieldsWithCompletion implements completionFieldSink by copying borrowed fields for background use.
func (a *AsyncSink) WriteFieldsWithCompletion(level Level, message string, fields []Field, durationMS int64, code int, outcome Outcome) {
	if a == nil || a.sink == nil {
		return
	}

	entry := asyncEntry{
		level:     level,
		message:   message,
		fieldList: borrowedFieldsFromFields(fields),
		duration:  durationMS,
		code:      code,
		outcome:   outcome,
		complete:  true,
	}

	a.enqueue(entry, func() map[string]any {
		return mapFromFieldListWithCompletion(entry.fieldList, durationMS, code, outcome)
	})
}

// WriteBorrowedFieldsWithCompletion implements borrowedCompletionFieldSink by copying borrowed fields for background use.
func (a *AsyncSink) WriteBorrowedFieldsWithCompletion(level Level, message string, fields []BorrowedField, durationMS int64, code int, outcome Outcome) {
	if a == nil || a.sink == nil {
		return
	}

	cp := make([]BorrowedField, len(fields))
	copy(cp, fields)

	entry := asyncEntry{
		level:     level,
		message:   message,
		fieldList: cp,
		duration:  durationMS,
		code:      code,
		outcome:   outcome,
		complete:  true,
	}

	a.enqueue(entry, func() map[string]any {
		return mapFromFieldListWithCompletion(entry.fieldList, durationMS, code, outcome)
	})
}

// WriteFieldsWithOperationCompletion implements operationCompletionFieldSink by copying borrowed fields for background use.
func (a *AsyncSink) WriteFieldsWithOperationCompletion(level Level, message string, fields []Field, start OperationStart, durationMS int64, code int, outcome Outcome) {
	if a == nil || a.sink == nil {
		return
	}

	entry := asyncEntry{
		level:     level,
		message:   message,
		fieldList: borrowedFieldsFromFields(fields),
		duration:  durationMS,
		code:      code,
		outcome:   outcome,
		start:     start,
		operation: true,
		complete:  true,
	}

	a.enqueue(entry, func() map[string]any {
		return mapFromFieldListWithOperationCompletion(entry.fieldList, start, durationMS, code, outcome)
	})
}

// WriteBorrowedFieldsWithOperationCompletion implements borrowedOperationCompletionFieldSink by copying borrowed fields for background use.
func (a *AsyncSink) WriteBorrowedFieldsWithOperationCompletion(level Level, message string, fields []BorrowedField, start OperationStart, durationMS int64, code int, outcome Outcome) {
	if a == nil || a.sink == nil {
		return
	}

	cp := make([]BorrowedField, len(fields))
	copy(cp, fields)

	entry := asyncEntry{
		level:     level,
		message:   message,
		fieldList: cp,
		duration:  durationMS,
		code:      code,
		outcome:   outcome,
		start:     start,
		operation: true,
		complete:  true,
	}

	a.enqueue(entry, func() map[string]any {
		return mapFromFieldListWithOperationCompletion(entry.fieldList, start, durationMS, code, outcome)
	})
}

func (a *AsyncSink) enqueue(entry asyncEntry, dropFields func() map[string]any) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return
	}

	if a.dropWhenFull {
		select {
		case a.entries <- entry:
		default:
			a.dropped.Add(1)
			if a.onDrop != nil {
				a.onDrop(DroppedEvent{Level: entry.level, Message: entry.message, Fields: dropFields()})
			}
		}
		return
	}

	a.entries <- entry
}

// Flush waits until all events written before Flush have reached the wrapped sink.
func (a *AsyncSink) Flush() bool {
	if a == nil || a.sink == nil {
		return false
	}

	ack := make(chan struct{})
	entry := asyncEntry{flush: ack}

	a.mu.RLock()
	if a.closed {
		a.mu.RUnlock()
		return false
	}
	a.entries <- entry
	a.mu.RUnlock()

	<-ack
	return true
}

// Close stops the background writer after draining queued events.
func (a *AsyncSink) Close() {
	if a == nil {
		return
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	close(a.done)
	a.mu.Unlock()

	a.wg.Wait()
}

// Dropped returns the number of events dropped because the buffer was full.
func (a *AsyncSink) Dropped() uint64 {
	if a == nil {
		return 0
	}
	return a.dropped.Load()
}

func (a *AsyncSink) run() {
	defer a.wg.Done()

	for {
		select {
		case entry := <-a.entries:
			a.write(entry)
		case <-a.done:
			a.drain()
			return
		}
	}
}

func (a *AsyncSink) drain() {
	for {
		select {
		case entry := <-a.entries:
			a.write(entry)
		default:
			return
		}
	}
}

func (a *AsyncSink) write(entry asyncEntry) {
	if entry.flush != nil {
		close(entry.flush)
		return
	}
	if entry.operation {
		if sink, ok := a.sink.(borrowedOperationCompletionFieldSink); ok {
			sink.WriteBorrowedFieldsWithOperationCompletion(entry.level, entry.message, entry.fieldList, entry.start, entry.duration, entry.code, entry.outcome)
			return
		}
		if sink, ok := a.sink.(operationCompletionFieldSink); ok {
			sink.WriteFieldsWithOperationCompletion(entry.level, entry.message, fieldsFromBorrowedFields(entry.fieldList), entry.start, entry.duration, entry.code, entry.outcome)
			return
		}
		a.sink.Write(entry.level, entry.message, mapFromFieldListWithOperationCompletion(entry.fieldList, entry.start, entry.duration, entry.code, entry.outcome))
		return
	}
	if entry.complete {
		if sink, ok := a.sink.(borrowedCompletionFieldSink); ok {
			sink.WriteBorrowedFieldsWithCompletion(entry.level, entry.message, entry.fieldList, entry.duration, entry.code, entry.outcome)
			return
		}
		if sink, ok := a.sink.(completionFieldSink); ok {
			sink.WriteFieldsWithCompletion(entry.level, entry.message, fieldsFromBorrowedFields(entry.fieldList), entry.duration, entry.code, entry.outcome)
			return
		}
		a.sink.Write(entry.level, entry.message, mapFromFieldListWithCompletion(entry.fieldList, entry.duration, entry.code, entry.outcome))
		return
	}
	if entry.fieldList != nil {
		if sink, ok := a.sink.(borrowedFieldSink); ok {
			sink.WriteBorrowedFields(entry.level, entry.message, entry.fieldList)
			return
		}
		if sink, ok := a.sink.(fieldSink); ok {
			sink.WriteFields(entry.level, entry.message, fieldsFromBorrowedFields(entry.fieldList))
			return
		}
		a.sink.Write(entry.level, entry.message, mapFromFieldList(entry.fieldList))
		return
	}
	a.sink.Write(entry.level, entry.message, entry.fields)
}

func copyFieldMap(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	cp := make(map[string]any, len(fields))
	for key, value := range fields {
		cp[key] = value
	}
	return cp
}

func mapFromFieldList(fields []BorrowedField) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	m := make(map[string]any, len(fields))
	for _, field := range fields {
		m[field.Key] = field.Any()
	}
	return m
}

func mapFromFieldListWithCompletion(fields []BorrowedField, durationMS int64, code int, outcome Outcome) map[string]any {
	m := make(map[string]any, len(fields)+3)
	for _, field := range fields {
		m[field.Key] = field.Any()
	}
	m["duration_ms"] = durationMS
	m["op.code"] = code
	m["op.outcome"] = string(outcome)
	return m
}

func mapFromFieldListWithOperationCompletion(fields []BorrowedField, start OperationStart, durationMS int64, code int, outcome Outcome) map[string]any {
	m := make(map[string]any, len(fields)+operationFieldCount(start)+3)
	for _, field := range fields {
		m[field.Key] = field.Any()
	}
	addOperationFieldsToMap(m, start)
	m["duration_ms"] = durationMS
	m["op.code"] = code
	m["op.outcome"] = string(outcome)
	return m
}

func borrowedFieldsFromFields(fields []Field) []BorrowedField {
	if len(fields) == 0 {
		return nil
	}
	borrowed := make([]BorrowedField, len(fields))
	for i, field := range fields {
		borrowed[i] = borrowedFieldFromAny(field.Key, field.Value)
	}
	return borrowed
}

func fieldsFromBorrowedFields(fields []BorrowedField) []Field {
	if len(fields) == 0 {
		return nil
	}
	legacy := make([]Field, len(fields))
	for i, field := range fields {
		legacy[i] = Field{Key: field.Key, Value: field.Any()}
	}
	return legacy
}

var _ Sink = (*AsyncSink)(nil)
var _ borrowedFieldSink = (*AsyncSink)(nil)
var _ fieldSink = (*AsyncSink)(nil)
var _ borrowedCompletionFieldSink = (*AsyncSink)(nil)
var _ completionFieldSink = (*AsyncSink)(nil)
var _ borrowedOperationCompletionFieldSink = (*AsyncSink)(nil)
var _ operationCompletionFieldSink = (*AsyncSink)(nil)
