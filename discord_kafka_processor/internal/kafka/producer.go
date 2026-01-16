package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"
)

type Producer struct {
	weekWriter *kafkago.Writer
	gameWriter *kafkago.Writer
	log        zerolog.Logger
}

func NewProducer(brokers []string, weekTopic, gameTopic, clientID string, log zerolog.Logger) (*Producer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers list is empty")
	}
	if weekTopic == "" || gameTopic == "" {
		return nil, fmt.Errorf("kafka topics are required")
	}

	transport := &kafkago.Transport{ClientID: clientID}

	weekWriter := &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        weekTopic,
		BatchTimeout: 200 * time.Millisecond,
		RequiredAcks: kafkago.RequireAll,
		Balancer:     &kafkago.LeastBytes{},
		Transport:    transport,
	}
	gameWriter := &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        gameTopic,
		BatchTimeout: 200 * time.Millisecond,
		RequiredAcks: kafkago.RequireAll,
		Balancer:     &kafkago.LeastBytes{},
		Transport:    transport,
	}

	return &Producer{weekWriter: weekWriter, gameWriter: gameWriter, log: log}, nil
}

func (p *Producer) WriteWeek(ctx context.Context, key string, payload []byte) error {
	msg := kafkago.Message{
		Key:   []byte(key),
		Value: payload,
		Time:  time.Now(),
	}
	if err := p.weekWriter.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("write week message: %w", err)
	}
	return nil
}

func (p *Producer) WriteGame(ctx context.Context, key, gameNumber string) error {
	msg := kafkago.Message{
		Key:   []byte(key),
		Value: []byte(gameNumber),
		Time:  time.Now(),
	}
	if err := p.gameWriter.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("write game message: %w", err)
	}
	return nil
}

func (p *Producer) Close() {
	if err := p.weekWriter.Close(); err != nil {
		p.log.Error().Err(err).Msg("failed to close week kafka writer")
	}
	if err := p.gameWriter.Close(); err != nil {
		p.log.Error().Err(err).Msg("failed to close game kafka writer")
	}
}
