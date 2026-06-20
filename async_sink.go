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
	level   Level
	message string
	fields  map[string]any
	flush   chan struct{}
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

	entry := asyncEntry{level: level, message: message, fields: copyFieldMap(fields)}
	a.enqueue(entry)
}

func (a *AsyncSink) enqueue(entry asyncEntry) {
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
				a.onDrop(DroppedEvent{Level: entry.level, Message: entry.message, Fields: copyFieldMap(entry.fields)})
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

var _ Sink = (*AsyncSink)(nil)
