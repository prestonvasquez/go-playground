package monitor

import (
	"context"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/event"
)

type EventType int

const (
	EventCommandStarted EventType = iota
	EventCommandFailed
	EventConnectionCheckedOut
	EventConnectionCheckedIn
	EventConnectionClosed
)

type RecordedEvent struct {
	Type  EventType
	Event any
}

type Monitor struct {
	CommandMonitor *event.CommandMonitor
	PoolMonitor    *event.PoolMonitor

	allEvents []RecordedEvent
	eventMu   sync.Mutex
}

func New(t *testing.T, shouldLog bool, cmds ...string) *Monitor {
	t.Helper()

	monitor := &Monitor{}
	monitor.Reset()

	monitor.CommandMonitor = &event.CommandMonitor{
		Started: func(ctx context.Context, cse *event.CommandStartedEvent) {
			for _, cmd := range cmds {
				if cse.CommandName == cmd {
					if shouldLog {
						t.Logf("command started: %+v\n", cse)
					}

					monitor.eventMu.Lock()
					monitor.allEvents = append(monitor.allEvents, RecordedEvent{Type: EventCommandStarted, Event: cse})
					monitor.eventMu.Unlock()
				}
			}
		},
		Succeeded: func(ctx context.Context, cse *event.CommandSucceededEvent) {
			for _, cmd := range cmds {
				if cse.CommandName == cmd {
					if shouldLog {
						t.Logf("command Succeeded: %+v\n", cse)
					}
				}
			}
		},
		Failed: func(ctx context.Context, cse *event.CommandFailedEvent) {
			for _, cmd := range cmds {
				if cse.CommandName == cmd {
					if shouldLog {
						t.Logf("command failed: %+v\n", cse)
					}

					monitor.eventMu.Lock()
					monitor.allEvents = append(monitor.allEvents, RecordedEvent{Type: EventCommandFailed, Event: cse})
					monitor.eventMu.Unlock()
				}
			}
		},
	}

	monitor.PoolMonitor = &event.PoolMonitor{
		Event: func(pe *event.PoolEvent) {
			switch pe.Type {
			case event.ConnectionCheckedIn:
				if shouldLog {
					t.Logf("connection checked in: %+v\n", pe)
				}

				monitor.eventMu.Lock()
				monitor.allEvents = append(monitor.allEvents, RecordedEvent{Type: EventConnectionCheckedIn, Event: pe})
				monitor.eventMu.Unlock()
			case event.ConnectionCheckedOut:
				if shouldLog {
					t.Logf("connection checked out: %+v\n", pe)
				}

				monitor.eventMu.Lock()
				monitor.allEvents = append(monitor.allEvents, RecordedEvent{Type: EventConnectionCheckedOut, Event: pe})
				monitor.eventMu.Unlock()
			case event.ConnectionClosed:
				if shouldLog {
					t.Logf("connection closed: %+v\n", pe)
				}

				monitor.eventMu.Lock()
				monitor.allEvents = append(monitor.allEvents, RecordedEvent{Type: EventConnectionClosed, Event: pe})
				monitor.eventMu.Unlock()
			}
		},
	}

	return monitor
}

func (m *Monitor) Reset() {
	m.allEvents = nil
	m.eventMu = sync.Mutex{}
}

// Events returns a copy of all recorded events in order.
func (m *Monitor) Events() []RecordedEvent {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()

	return append([]RecordedEvent(nil), m.allEvents...)
}

// CommandStartedEvents returns all command started events in order.
func (m *Monitor) CommandStartedEvents() []*event.CommandStartedEvent {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()

	var events []*event.CommandStartedEvent
	for _, e := range m.allEvents {
		if e.Type == EventCommandStarted {
			events = append(events, e.Event.(*event.CommandStartedEvent))
		}
	}

	return events
}

// CommandFailedEvents returns all command failed events in order.
func (m *Monitor) CommandFailedEvents() []*event.CommandFailedEvent {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()

	var events []*event.CommandFailedEvent
	for _, e := range m.allEvents {
		if e.Type == EventCommandFailed {
			events = append(events, e.Event.(*event.CommandFailedEvent))
		}
	}

	return events
}

// ConnectionCheckedOutEvents returns all connection checked out events in order.
func (m *Monitor) ConnectionCheckedOutEvents() []*event.PoolEvent {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()

	var events []*event.PoolEvent
	for _, e := range m.allEvents {
		if e.Type == EventConnectionCheckedOut {
			events = append(events, e.Event.(*event.PoolEvent))
		}
	}

	return events
}

// ConnectionCheckedInEvents returns all connection checked in events in order.
func (m *Monitor) ConnectionCheckedInEvents() []*event.PoolEvent {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()

	var events []*event.PoolEvent
	for _, e := range m.allEvents {
		if e.Type == EventConnectionCheckedIn {
			events = append(events, e.Event.(*event.PoolEvent))
		}
	}

	return events
}

// ConnectionClosedEvents returns all connection closed events in order.
func (m *Monitor) ConnectionClosedEvents() []*event.PoolEvent {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()

	var events []*event.PoolEvent
	for _, e := range m.allEvents {
		if e.Type == EventConnectionClosed {
			events = append(events, e.Event.(*event.PoolEvent))
		}
	}

	return events
}
