package discord

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

type EventType string

const (
	EventReady        EventType = "ready"
	EventResumed      EventType = "resumed"
	EventDisconnected EventType = "disconnected"
)

type Event struct {
	Type EventType
	At   time.Time
}

type Client struct {
	session   *discordgo.Session
	channelID string
	out       chan<- string
	log       zerolog.Logger
	ready     atomic.Bool
	events    chan Event
	enqueueTO time.Duration
}

func NewClient(token, channelID string, out chan<- string, enqueueTimeout time.Duration, log zerolog.Logger) (*Client, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	return &Client{
		session:   session,
		channelID: channelID,
		out:       out,
		log:       log,
		events:    make(chan Event, 8),
		enqueueTO: enqueueTimeout,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	c.session.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMessages)

	c.session.AddHandler(func(s *discordgo.Session, _ *discordgo.Ready) {
		c.ready.Store(true)
		c.log.Info().Msg("discord session ready")
		c.emit(EventReady)
	})
	c.session.AddHandler(func(s *discordgo.Session, _ *discordgo.Resumed) {
		c.ready.Store(true)
		c.log.Info().Msg("discord session resumed")
		c.emit(EventResumed)
	})
	c.session.AddHandler(func(s *discordgo.Session, _ *discordgo.Disconnect) {
		c.ready.Store(false)
		c.log.Warn().Msg("discord session disconnected")
		c.emit(EventDisconnected)
	})

	c.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m == nil || m.Message == nil {
			return
		}
		if c.channelID != "" && m.ChannelID != c.channelID {
			return
		}

		if c.enqueue(m.ID) {
			c.log.Info().Str("message_id", m.ID).Msg("received discord message")
		} else {
			c.log.Warn().Str("message_id", m.ID).Msg("message buffer full, dropping")
		}
	})

	if err := c.session.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}

	go func() {
		<-ctx.Done()
		c.ready.Store(false)
		_ = c.session.Close()
	}()

	return nil
}

func (c *Client) Close() error {
	return c.session.Close()
}

func (c *Client) Ready() bool {
	return c.ready.Load()
}

func (c *Client) Events() <-chan Event {
	return c.events
}

func (c *Client) BackfillSince(ctx context.Context, afterID string, pageSize, maxMessages int) (int, error) {
	if strings.TrimSpace(c.channelID) == "" || strings.TrimSpace(afterID) == "" {
		return 0, nil
	}
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > 100 {
		pageSize = 100
	}

	total := 0
	lastID := afterID
	for {
		if maxMessages > 0 && total >= maxMessages {
			return total, nil
		}
		limit := pageSize
		if maxMessages > 0 && maxMessages-total < limit {
			limit = maxMessages - total
		}
		messages, err := c.session.ChannelMessages(c.channelID, limit, "", lastID, "")
		if err != nil {
			return total, fmt.Errorf("fetch channel messages: %w", err)
		}
		if len(messages) == 0 {
			return total, nil
		}
		sort.Slice(messages, func(i, j int) bool {
			return snowflakeLess(messages[i].ID, messages[j].ID)
		})
		maxID := lastID
		for _, msg := range messages {
			if msg == nil || msg.ID == "" {
				continue
			}
			select {
			case c.out <- msg.ID:
				total++
				if snowflakeLess(maxID, msg.ID) {
					maxID = msg.ID
				}
			case <-ctx.Done():
				return total, ctx.Err()
			}
		}
		if maxID == lastID {
			return total, nil
		}
		lastID = maxID
	}
}

func (c *Client) emit(eventType EventType) {
	select {
	case c.events <- Event{Type: eventType, At: time.Now()}:
	default:
	}
}

func (c *Client) enqueue(id string) bool {
	if c.enqueueTO <= 0 {
		select {
		case c.out <- id:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(c.enqueueTO)
	defer timer.Stop()
	select {
	case c.out <- id:
		return true
	case <-timer.C:
		return false
	}
}

func snowflakeLess(a, b string) bool {
	ai, aerr := strconv.ParseUint(a, 10, 64)
	bi, berr := strconv.ParseUint(b, 10, 64)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}
