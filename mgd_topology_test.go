package goplayground

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/mongoevent"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGDLatestTopologyDescription(t *testing.T) {
	// You can't get a topology description directly from the mongo.Client object.
	// You have to provide the client with an event monitor that captures the
	// latest topology description.

	const minPoolSize = 3

	serverM := mongoevent.NewServerMontior()
	poolM := mongoevent.NewPoolMonitor()

	opts := options.Client().
		SetPoolMonitor(mongoevent.NewPoolEventMonitor(poolM)).
		SetServerMonitor(mongoevent.NewEventServerMonitor(serverM)).
		// Choose min of 3 to ensure we can hit a minimum number of connections per
		// server.
		SetMinPoolSize(minPoolSize)

	client, teardown := mongolocal.New(t, context.Background(), mongolocal.WithMongoClientOptions(opts))
	defer teardown(t)

	require.NoError(t, client.Ping(context.Background(), nil))

	// awaitMinimumPoolSize waits for the client's connection pool to reach the
	// specified minimum size. This is a best effort operation that times out after
	// some predefined amount of time to avoid blocking tests indefinitely.
	awaitMinimumPoolSize := func(
		ctx context.Context,
		sm *mongoevent.ServerMonitor,
		pm *mongoevent.PoolMonitor,
		minPoolSize uint64,
	) error {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return fmt.Errorf("timed out waiting for client to reach minPoolSize")
			case <-ticker.C:
				ready := true
				for _, server := range sm.LatestTopologyDescription().Servers {
					if pm.ConnsReady(server.Addr.String()) < int(minPoolSize) {
						ready = false

						// If any server has less than minPoolSize connections, continue
						// waiting.
						break
					}
				}

				if ready {
					return nil
				}
			}
		}
	}

	// Don't spend longer than 5s awaiting minPoolSize.
	awaitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, awaitMinimumPoolSize(awaitCtx, serverM, poolM, minPoolSize))
}
