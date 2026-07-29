package brokertest

import (
	"context"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// TestNewRedpandaProducesAndConsumes is the harness's own smoke test.
// A container that merely reports itself healthy is not proof the
// broker works: this proves it by round-tripping a message through
// the same segmentio/kafka-go client the rest of the codebase uses,
// leaving a working, checked-in example for the next task (V5-23) to
// build on.
func TestNewRedpandaProducesAndConsumes(t *testing.T) {
	brokers := NewRedpanda(t)
	if len(brokers) == 0 {
		t.Fatal("NewRedpanda returned no broker addresses")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const topic = "brokertest-smoke"
	want := []byte("redpanda smoke test")

	writer := &kafkago.Writer{
		Addr:                   kafkago.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafkago.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		AllowAutoTopicCreation: true,
	}
	defer writer.Close()

	if err := writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte("smoke"),
		Value: want,
	}); err != nil {
		t.Fatalf("write message: %v", err)
	}

	// GroupID rather than a hardcoded partition: the topic was just
	// auto-created, and this way the test does not need to assume how
	// many partitions Redpanda gave it.
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:  brokers,
		Topic:    topic,
		GroupID:  "brokertest-smoke-reader",
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	msg, err := reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	if string(msg.Value) != string(want) {
		t.Errorf("got %q, want %q", msg.Value, want)
	}
}
