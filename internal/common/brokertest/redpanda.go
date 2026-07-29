// Package brokertest starts a throwaway Redpanda broker for integration
// tests.
//
// Redpanda speaks the Kafka wire protocol, so a test built on this
// harness exercises the same segmentio/kafka-go client the services
// use in production, not a stand-in for it. It lives next to, rather
// than inside, pgtest: that package's name is short for "Postgres
// test", and a broker harness has nothing to do with Postgres.
package brokertest

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
)

// redpandaImage pins the broker version started for tests. v5.0 runs
// Redpanda in production (V5-23); this harness lets that migration,
// and everything tested after it, rely on a broker that starts in a
// few seconds instead of the better part of a minute Kafka needed.
const redpandaImage = "docker.redpanda.com/redpandadata/redpanda:v24.2.18"

// NewRedpanda starts a single-node Redpanda broker and returns its
// Kafka-API bootstrap addresses.
func NewRedpanda(t *testing.T) []string {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

	ctx := context.Background()
	container, err := redpanda.Run(ctx,
		redpandaImage,
		redpanda.WithAutoCreateTopics(),
	)
	if err != nil {
		t.Fatalf("start redpanda: %v", err)
	}

	addr, err := container.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatalf("seed broker: %v", err)
	}

	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate redpanda: %v", err)
		}
	})

	return []string{addr}
}
