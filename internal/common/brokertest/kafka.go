// Package brokertest starts a throwaway Kafka broker for integration
// tests.
//
// It lives next to, rather than inside, pgtest: that package's name is
// short for "Postgres test", and a broker harness has nothing to do
// with Postgres.
package brokertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// kafkaImage pins the broker started for tests to what v5.0 actually
// runs in production (V5-38): Apache Kafka in KRaft mode (no
// Zookeeper). Production ran Redpanda first (V5-23) - same wire
// protocol, much faster container startup - but Redpanda's binary
// requires ARMv8.1 atomic instructions the target Raspberry Pi 4's
// Cortex-A72 (ARMv8.0: fp asimd evtstrm crc32 cpuid, no LSE) does not
// have. Every measurement of it had been taken on Apple Silicon; it
// never ran on the real host even once before the deploy that finally
// tried, and failed outright there (exit 133, no usable log line).
// This harness has to track whatever production actually runs, or a
// green test suite stops meaning anything about the real broker.
const kafkaImage = "apache/kafka:3.9.0"

const kafkaPort = "9092/tcp"

// starterScript is copied into the container only once its mapped host
// port is known, and only then executed. Kafka's advertised listener
// has to be a routable, host-reachable address - the broker rejects a
// bare 0.0.0.0 outright, confirmed the hard way probing this exact
// image on the target Pi - but the container has no host port assigned
// until after Docker has already started it, so the real broker start
// has to wait until that's known. Mirrors the technique
// testcontainers-go's own (Confluent-based) kafka module uses for the
// identical chicken-and-egg problem.
const starterScript = "/tmp/testcontainers_start.sh"

// NewKafka starts a single-node Kafka broker in KRaft mode and returns
// its Kafka-API bootstrap address.
func NewKafka(t *testing.T) []string {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

	ctx := context.Background()

	container, err := testcontainers.Run(ctx, kafkaImage,
		testcontainers.WithExposedPorts(kafkaPort),
		testcontainers.WithEnv(map[string]string{
			"KAFKA_NODE_ID":                   "1",
			"KAFKA_PROCESS_ROLES":             "broker,controller",
			"KAFKA_LISTENERS":                 "PLAINTEXT://:9092,CONTROLLER://:9093",
			"KAFKA_CONTROLLER_LISTENER_NAMES": "CONTROLLER",
			// "localhost", not the container's own hostname: production
			// hit a real dead end here too. A bare `docker run` outside
			// the compose network can't resolve its own container name,
			// and the controller quorum connection then fails. Loopback
			// needs no DNS at all, so it works the same way everywhere.
			"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@localhost:9093",
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
			"KAFKA_INTER_BROKER_LISTENER_NAME":               "PLAINTEXT",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
			// No KAFKA_AUTO_CREATE_TOPICS_ENABLE override: production
			// doesn't set one either (kafka-init always creates topics
			// explicitly before any service touches them), and this
			// harness's own smoke test creates its topic explicitly too
			// - see smoke_test.go for why. Matching production's env
			// set exactly here is the point of this harness.
		}),
		testcontainers.WithEntrypoint("sh"),
		// This command blocks until the PostStarts hook below has
		// copied the starter script in, then hands off to it. Without
		// this indirection Kafka would boot before its advertised
		// listener could be set correctly (see starterScript).
		testcontainers.WithCmd("-c", "while [ ! -f "+starterScript+" ]; do sleep 0.1; done; exec bash "+starterScript),
		testcontainers.WithLifecycleHooks(testcontainers.ContainerLifecycleHooks{
			PostStarts: []testcontainers.ContainerHook{
				func(ctx context.Context, c testcontainers.Container) error {
					if err := wait.ForMappedPort(kafkaPort).WaitUntilReady(ctx, c); err != nil {
						return fmt.Errorf("wait for mapped port: %w", err)
					}

					endpoint, err := c.PortEndpoint(ctx, kafkaPort, "PLAINTEXT")
					if err != nil {
						return fmt.Errorf("port endpoint: %w", err)
					}

					script := "export KAFKA_ADVERTISED_LISTENERS=" + endpoint + "\nexec /etc/kafka/docker/run\n"
					if err := c.CopyToContainer(ctx, []byte(script), starterScript, 0o755); err != nil {
						return fmt.Errorf("copy starter script: %w", err)
					}

					// The JVM is slow to come up compared to Redpanda's
					// (the whole reason the earlier harness could get
					// away with no explicit startup timeout); give it
					// real room instead of relying on a default sized
					// for a C++ binary.
					return wait.ForLog(".*Transitioning from RECOVERY to RUNNING.*").
						AsRegexp().
						WithStartupTimeout(90*time.Second).
						WaitUntilReady(ctx, c)
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("start kafka: %v", err)
	}
	// Register cleanup immediately after the container starts, before any
	// call that can fail (PortEndpoint below): t.Fatalf halts the test
	// goroutine via runtime.Goexit, so a t.Cleanup registered only after
	// such a call never runs on that failure path, leaking the container
	// until Ryuk's session-end reaper catches it. Matches
	// pgtest.NewPostgres's fix for the identical bug.
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate kafka: %v", err)
		}
	})

	addr, err := container.PortEndpoint(ctx, kafkaPort, "")
	if err != nil {
		t.Fatalf("bootstrap address: %v", err)
	}

	return []string{addr}
}
