package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"
)

type Producer struct {
	writer *kafkago.Writer
	log    zerolog.Logger
}

func NewProducer(brokers []string, topic, clientID string, log zerolog.Logger) (*Producer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers list is empty")
	}
	if topic == "" {
		return nil, fmt.Errorf("kafka topic is empty")
	}

	writer := &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        topic,
		BatchTimeout: 200 * time.Millisecond,
		RequiredAcks: kafkago.RequireAll,
		Balancer:     &kafkago.LeastBytes{},
		Transport: &kafkago.Transport{
			ClientID: clientID,
		},
	}

	return &Producer{writer: writer, log: log}, nil
}

func (p *Producer) WriteMessage(ctx context.Context, messageID string) error {
	msg := kafkago.Message{
		Key:   []byte(messageID),
		Value: []byte(messageID),
		Time:  time.Now(),
	}

	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("write message to kafka: %w", err)
	}
	return nil
}

func (p *Producer) Close() error {
	if err := p.writer.Close(); err != nil {
		p.log.Error().Err(err).Msg("failed to close kafka writer")
		return err
	}
	return nil
}
