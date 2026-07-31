package brokertest

import (
	"context"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// TestNewKafkaProducesAndConsumes is the harness's own smoke test. A
// container that merely reports itself healthy is not proof the broker
// works: this proves it by round-tripping a message through the same
// segmentio/kafka-go client the rest of the codebase uses against the
// same broker (Kafka in KRaft mode, V5-38) production runs.
func TestNewKafkaProducesAndConsumes(t *testing.T) {
	brokers := NewKafka(t)
	if len(brokers) == 0 {
		t.Fatal("NewKafka returned no broker addresses")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const topic = "brokertest-smoke"
	want := []byte("kafka smoke test")

	// Created explicitly rather than via the writer's
	// AllowAutoTopicCreation: real Kafka's auto-create (unlike
	// Redpanda's, which this harness ran before V5-38) is asynchronous
	// - the Metadata request that triggers it still reports the topic
	// missing in that same response - so a Writer that both creates and
	// immediately produces to a topic races the broker and fails with
	// UnknownTopicOrPartition. Production never hits this: kafka-init
	// always creates topics up front, before any service produces to
	// them. Conn.CreateTopics is a synchronous admin call, so it does
	// not have the same race.
	conn, err := kafkago.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		t.Fatalf("dial broker: %v", err)
	}
	defer conn.Close()
	if err := conn.CreateTopics(kafkago.TopicConfig{
		Topic:             topic,
		NumPartitions:     1,
		ReplicationFactor: 1,
	}); err != nil {
		t.Fatalf("create topic: %v", err)
	}

	writer := &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafkago.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
	}
	defer writer.Close()

	if err := writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte("smoke"),
		Value: want,
	}); err != nil {
		t.Fatalf("write message: %v", err)
	}

	// GroupID rather than a hardcoded partition: every real consumer in
	// this codebase reads this way (confirmed against a live compose
	// stack for V5-38 - see task-V5-38-report.md), so the harness's own
	// smoke test should too, rather than taking a shortcut real code
	// never gets to take.
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
