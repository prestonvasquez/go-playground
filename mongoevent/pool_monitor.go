package mongoevent

import (
	"sync"

	"go.mongodb.org/mongo-driver/v2/event"
)

// PoolMonitor is a monitor that captures connection pool events.
type PoolMonitor struct {
	mu             sync.RWMutex
	connsPerServer map[string]int
}

// NewPoolMonitor creates a new PoolMonitor.
func NewPoolMonitor() *PoolMonitor {
	return &PoolMonitor{
		connsPerServer: make(map[string]int),
	}
}

// NewPoolEventMonitor creates an event.PoolMonitor that routes events to
// local PoolMonitor callbacks.
func NewPoolEventMonitor(monitor *PoolMonitor) *event.PoolMonitor {
	return &event.PoolMonitor{
		Event: func(evt *event.PoolEvent) {
			monitor.mu.Lock()
			defer monitor.mu.Unlock()

			switch evt.Type {
			case event.ConnectionReady:
				monitor.connsPerServer[evt.Address]++
			case event.ConnectionClosed:
				monitor.connsPerServer[evt.Address]--
			}
		},
	}
}

// ConnsReady returns the number of ready connections for the given server
// address.
func (pm *PoolMonitor) ConnsReady(serverAddr string) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.connsPerServer[serverAddr]
}
