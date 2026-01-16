package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bwmarrin/discordgo"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./main.go <message_id>")
		os.Exit(2)
	}

	messageID := os.Args[1]
	token := os.Getenv("DISCORD_TOKEN")
	channelID := os.Getenv("DISCORD_CHANNEL_ID")

	if token == "" {
		fmt.Fprintln(os.Stderr, "DISCORD_TOKEN is required")
		os.Exit(2)
	}
	if channelID == "" {
		fmt.Fprintln(os.Stderr, "DISCORD_CHANNEL_ID is required")
		os.Exit(2)
	}

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create Discord session: %v\n", err)
		os.Exit(1)
	}
	defer session.Close()

	msg, err := session.ChannelMessage(channelID, messageID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch message: %v\n", err)
		os.Exit(1)
	}

	payload, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal message: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(payload))
}
