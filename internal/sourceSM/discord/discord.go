// Package discord provides a client for interacting with the Discord API, specifically tailored
// for managing game-related messages and schedules in a Discord server.
package discord

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colzphml/mega_games/internal/model"
	"github.com/colzphml/mega_games/pkg/config"
	"github.com/colzphml/mega_games/pkg/utils"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

// log initializes a logger for the discord package with structured logging.
var log = zerolog.New(os.Stdout).With().Str("package", "discord").Timestamp().Logger()

// Client defines a Discord client that interacts with the Discord API for managing game-related
// activities such as sending messages, processing incoming messages, and handling game schedules.
type Client struct {
	Dg           *discordgo.Session         // Discord session
	ChannelId    string                     // Discord channel ID for message operations
	MessageChan  chan<- model.DiscordGame   // Channel for outbound game-related messages
	TargetChan   chan<- model.TargetMessage // Channel for outbound target-specific messages
	InternalChan chan model.DiscordMessage  // Internal channel for processing Discord messages
	Schedule     model.Schedule             // Schedule of games
	Teams        model.Teams                // Teams involved in the games
}

// NewClient initializes a new Discord client using provided configuration and channels for message communication.
// It sets up a new Discord session, parses team information, and prepares the client for operation.
func NewClient(ctx context.Context, cfg *config.Config, messageChan chan<- model.DiscordGame, targetChan chan<- model.TargetMessage) (*Client, error) {
	dg, err := discordgo.New("Bot " + cfg.App.Source.Token)
	if err != nil {
		log.Error().Err(err).Msg("failed to create Discord session")
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMessages)

	teams, err := utils.ParseCSVFileToTeams(cfg.App.PlayersPath)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse teams from CSV")
		return nil, fmt.Errorf("error parsing teams from CSV: %w", err)
	}
	return &Client{
		Schedule:     utils.ParseScheduleFromCSV(cfg.App.SchedulePath),
		Teams:        teams,
		Dg:           dg,
		ChannelId:    cfg.App.Source.ChannelID,
		InternalChan: make(chan model.DiscordMessage),
		MessageChan:  messageChan,
		TargetChan:   targetChan,
	}, nil
}

// HandleMessages listens for new messages in the specified Discord channel and processes them accordingly.
// It runs continuously until the context is canceled, ensuring that all incoming messages are handled.
func (c *Client) HandleMessages(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	messageHandler := func(s *discordgo.Session, m *discordgo.MessageCreate) {
		msg := model.DiscordMessage{
			MessageId: m.ID,
			Proceed:   false,
		}
		log.Info().Msg(msg.MessageId)
		time.Sleep(5 * time.Second)
		c.InternalChan <- msg
	}

	c.Dg.AddHandler(messageHandler)

	if err := c.Dg.Open(); err != nil {
		log.Error().Err(err).Msg("error opening connection to Discord")
		return
	}

	<-ctx.Done()
	log.Info().Msg("stopping Discord message listener")
}

// ProceedMessages processes the messages received from the Discord server.
// It attempts to process each message, handling them based on their content, until the context is canceled.
func (c *Client) ProceedMessages(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("stopping Discord message processor")
			return
		case msg := <-c.InternalChan:
			for i := 0; i < 50; i++ {
				time.Sleep(100 * time.Millisecond)
				message, err := c.Dg.ChannelMessage(c.ChannelId, msg.MessageId)
				if err != nil {
					log.Error().Err(err).Msg("error fetching message")
					break // Exit the loop on error to avoid infinite loops
				}
				if len(message.Embeds) != 0 {
					msg = c.readMessageEmbeds(message)
					if err := c.proceedMessage(msg); err != nil {
						log.Error().Err(err).Msg("error processing message")
					}
					break // Message processed, exit the loop
				}
			}
		}
	}
}

// proceedMessage handles the logic for processing Discord messages, dispatching them based on their type.
func (c *Client) proceedMessage(msg model.DiscordMessage) error {
	if msg.NewWeek {
		log.Info().Msg("new week message")
		c.TargetChan <- model.TargetMessage{
			Action: "newWeek",
			Value:  msg.NewWeekText,
		}
		return nil
	}

	for _, game := range msg.Games {
		c.MessageChan <- game
	}
	return nil
}

// Close terminates the Discord client session, ensuring all resources are properly released.
func (c *Client) Close() error {
	log.Info().Msg("closing Discord client")
	return c.Dg.Close()
}

// readMessageEmbeds analyzes message embeds to extract and construct game-related messages.
func (c *Client) readMessageEmbeds(message *discordgo.Message) model.DiscordMessage {
	var result model.DiscordMessage
	result.MessageId = message.ID
	for _, embed := range message.Embeds {
		if strings.Contains(embed.Title, "has advanced to") && (strings.Contains(embed.Title, "Pre Season") || strings.Contains(embed.Title, "Regular Season")) {
			games := c.parseAdvanceMessage(embed.Title)
			result.NewWeek = true
			result.NewWeekText = c.buildScheduleText(embed.Title, games)
			return result // Early return for efficiency
		} else if embed.Fields != nil {
			for _, field := range embed.Fields {
				if strings.Contains(field.Value, "**") && strings.Contains(field.Value, "MEGA/games") {
					url, err := extractURL(field.Value)
					if err != nil {
						log.Error().Err(err).Msg("error extracting URL from message")
						continue // Skip this field on error
					}

					result.Games = append(result.Games, model.DiscordGame{
						MessageId:  message.ID,
						GameNumber: field.Name,
						GameUrl:    url,
					})
				}
			}
		} else if embed.Description != "" {
			if strings.Contains(embed.Description, "**") && strings.Contains(embed.Description, "MEGA/games") {
				url, err := extractURL(embed.Description)
				if err != nil {
					log.Error().Err(err).Msg("error extracting URL from message")
					continue // Skip this field on error
				}

				result.Games = append(result.Games, model.DiscordGame{
					MessageId:  message.ID,
					GameNumber: embed.Description,
					GameUrl:    url,
				})
			}
		}
	}

	return result
}

// extractURL extracts a URL from a string field, ensuring correct formatting and error handling.
func extractURL(fieldValue string) (string, error) {
	start := strings.LastIndex(fieldValue, "(") + 2
	end := strings.LastIndex(fieldValue, ")") - 1
	if start <= 0 || end <= start {
		log.Debug().Msg("URL not found in the field value")
		return "", fmt.Errorf("URL not found")
	}
	return fieldValue[start:end], nil
}

// parseAdvanceMessage extracts the season stage and week number from the title of a Discord message.
// It returns a slice of games scheduled for that week. If the message does not correspond to a scheduled
// week or if there's an error parsing the week number, it logs an error and returns nil.
func (c *Client) parseAdvanceMessage(title string) []model.Game {
	words := strings.Split(title, " ")
	weekNumber, err := strconv.Atoi(words[len(words)-1])
	if err != nil {
		log.Error().Err(err).Msg("error parsing week number from title")
		return nil
	}

	stage := strings.Contains(title, "Regular Season")
	if !stage && weekNumber == 4 {
		// Specific logic for pre-season handling; week 4 is typically not included in pre-season.
		return nil
	}

	return utils.GetAllGamesForWeek(c.Schedule, stage, weekNumber)
}

// buildScheduleText creates a formatted string representing the schedule for the upcoming week, based on the games provided.
// It uses the team short names and constructs a Markdown-friendly message for displaying in Discord, including links to teams' profiles.
func (c *Client) buildScheduleText(title string, games []model.Game) string {
	var textBuilder strings.Builder
	textBuilder.WriteString(fmt.Sprintf("*%s*\n", title))

	for _, game := range games {
		home := c.Teams.Teams[game.Home]
		away := c.Teams.Teams[game.Away]
		var gameInfo string
		if away.Player == "CPU" && home.Player == "CPU" {
			gameInfo = fmt.Sprintf("\n%s(CPU) @ %s(CPU)", away.ShortName, home.ShortName)
		} else if away.Player == "CPU" {
			gameInfo = fmt.Sprintf("\n%s(CPU) @ [%s](%s)", away.ShortName, home.ShortName, "t.me/"+home.Player[1:])
		} else if home.Player == "CPU" {
			gameInfo = fmt.Sprintf("\n[%s](%s) @ %s(CPU)", away.ShortName, "t.me/"+away.Player[1:], home.ShortName)
		} else {
			gameInfo = fmt.Sprintf("\n[%s](%s) @ [%s](%s)", away.ShortName, "t.me/"+away.Player[1:], home.ShortName, "t.me/"+home.Player[1:])
		}

		textBuilder.WriteString(gameInfo)
	}

	return textBuilder.String()
}
