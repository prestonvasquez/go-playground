package mongoevent

import (
	"sync"

	"go.mongodb.org/mongo-driver/v2/event"
)

// ServerMonitor is a monitor that captures server monitoring events.
type ServerMonitor struct {
	mu             sync.RWMutex
	latestTopology event.TopologyDescription
}

// NewServerMontior creates a new ServerMonitor.
func NewServerMontior() *ServerMonitor {
	return &ServerMonitor{}
}

// NewEventServerMonitor creates an event.ServerMonitor that routes events to
// the provided ServerMonitor.
func NewEventServerMonitor(monitor *ServerMonitor) *event.ServerMonitor {
	return &event.ServerMonitor{
		TopologyDescriptionChanged: func(evt *event.TopologyDescriptionChangedEvent) {
			monitor.mu.Lock()
			monitor.latestTopology = evt.NewDescription
			monitor.mu.Unlock()
		},
	}
}

// LatestTopologyDescription returns the latest TopologyDescription captured
// from the TopologyDescriptionChanged events.
func (sm *ServerMonitor) LatestTopologyDescription() event.TopologyDescription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.latestTopology
}
