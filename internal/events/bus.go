package events

import "sync"

// Event represents a device/service event delivered via webhook.
type Event struct {
	Device  string
	Service string
	Name    string
	Payload []byte // raw body for now; TODO: structured decode
}

// Bus is a simple in-memory pub/sub.
type Bus struct {
	mu   sync.RWMutex
	subs map[int]chan Event
	next int
}

func NewBus() *Bus { return &Bus{subs: make(map[int]chan Event)} }

func (b *Bus) Subscribe(buffer int) (id int, ch <-chan Event, cancel func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id = b.next
	b.next++
	c := make(chan Event, buffer)
	b.subs[id] = c
	cancel = func() {
		b.mu.Lock()
		if sc, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(sc)
		}
		b.mu.Unlock()
	}
	return id, c, cancel
}

func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default: /* drop if full */
		}
	}
}
