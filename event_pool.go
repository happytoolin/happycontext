package hc

import "sync"

var eventPool = sync.Pool{
	New: func() any {
		return &Event{}
	},
}

func newPooledEvent() *Event {
	event := eventPool.Get().(*Event)
	event.resetPooled()
	event.pooled = true
	return event
}

func releaseEvent(event *Event) {
	if event == nil || !event.pooled {
		return
	}
	event.resetLocal()
	clear(event.fieldBuf[:])
	event.pooled = false
	eventPool.Put(event)
}
