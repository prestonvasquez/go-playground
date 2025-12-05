package mongoevent

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/event"
)

// CommandMonitor monitors MongoDB commands and records failed errors.
type CommandMonitor struct {
	FailedErrors []error
}

// NewCommandMonitor creates a new CommandMonitor instance.
func NewCommandMonitor() *CommandMonitor {
	return &CommandMonitor{}
}

// NewCommandEventMonitor creates a MongoDB event.CommandMonitor that records
// failed command errors.
func NewCommandEventMonitor(monitor *CommandMonitor) *event.CommandMonitor {
	return &event.CommandMonitor{
		Failed: func(_ context.Context, evt *event.CommandFailedEvent) {
			monitor.FailedErrors = append(monitor.FailedErrors, evt.Failure)
		},
	}
}
