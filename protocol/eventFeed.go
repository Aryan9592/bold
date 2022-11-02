package protocol

import (
	"context"
	"sync"
)

type EventFeed[Event any] struct {
	mutex    sync.RWMutex
	pingChan chan struct{} // this gets closed and re-generated whenever an event is appended
	ctx      context.Context
	closed   bool
	events   []Event
}

func NewEventFeed[Event any](ctx context.Context) *EventFeed[Event] {
	feed := &EventFeed[Event]{
		mutex:    sync.RWMutex{},
		pingChan: make(chan struct{}),
		ctx:      ctx,
		closed:   false,
		events:   []Event{},
	}
	return feed
}

func (feed *EventFeed[Event]) Append(event Event) {
	feed.mutex.Lock()
	feed.events = append(feed.events, event)
	close(feed.pingChan)
	feed.pingChan = make(chan struct{})
	feed.mutex.Unlock()
}

func (feed *EventFeed[Event]) StartFilteredListener(ctx context.Context, filter func(Event) bool) <-chan Event {
	c := make(chan Event)
	go func() {
		defer close(c)
		numRead := 0
		for {
			feed.mutex.RLock()
			closed := feed.closed
			var event *Event
			if numRead < len(feed.events) {
				ev := feed.events[numRead]
				if filter(ev) {
					event = &ev
				}
				numRead++
			}
			pingChan := feed.pingChan
			feed.mutex.RUnlock()

			if closed {
				return
			} else if event != nil {
				select {
				case c <- *event:
				case <-ctx.Done():
					return
				case <-feed.ctx.Done():
					return
				}
			} else {
				select {
				case <-pingChan:
				case <-ctx.Done():
					return
				case <-feed.ctx.Done():
					return
				}
			}
		}
	}()
	return c
}

func (feed *EventFeed[Event]) StartListener(ctx context.Context) <-chan Event {
	return feed.StartFilteredListener(ctx, func(Event) bool { return true })
}
