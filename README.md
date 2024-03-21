# Mega Games Result Sender

This updated document outlines the configuration and operation guidelines for the enhanced automation program. This version introduces refined settings for improved integration with web services through Selenium, Discord message handling, and Telegram notifications. Docker remains a prerequisite for installation and deployment.

## Configuration

Before launching the program, a revised `config.yaml` file is necessary in the project's root. This file now accommodates additional parameters for enhanced functionality:

```yaml
app:
  middle:
    type: "selenium"
    middle_url: "http://localhost:4444/wd/hub" # Local URL for Selenium Hub
  source:
    type: "discord"
    token: "DISCORD_BOT_TOKEN" # Your unique Discord bot token
    channel_id: "DISCORD_CHANNEL_ID" # The targeted Discord channel's ID
  target:
    type: "telegram"
    token: "TELEGRAM_BOT_TOKEN" # Your unique Telegram bot token
    common_channel_id: "TELEGRAM_COMMON_CHANNEL_ID" # Telegram common channel or chat ID
    news_channel_id: "TELEGRAM_NEWS_CHANNEL_ID" # Telegram news channel or chat ID
  deep_history: NUMBER_OF_MESSAGES # Number of past messages to analyze
  file_storage_path: "PATH_TO_STORAGE" # Directory for screenshots or files
  games_url: "URL_TO_FETCH_GAMES" # URL for game information retrieval
  schedule_path: "PATH_TO_SCHEDULE_CSV" # Path to the game schedule CSV file
  players_path: "PATH_TO_PLAYERS_CSV" # Path to the players CSV file
  cache_size: CACHE_SIZE # Size of the cache for storing temporary data
```

### Field Descriptions:

- `middle_url`: The endpoint for accessing Selenium Hub, now defaulting to a local instance.
- `token` (Discord and Telegram): Authentication tokens for Discord and Telegram, enabling the app to interact with specified accounts.
- `channel_id` (Discord), `common_channel_id`, and `news_channel_id` (Telegram): Identifiers for the messaging channels where the bot will operate.
- `deep_history`: Specifies how many historical messages the application will process initially.
- `file_storage_path`: Designates a local storage path for saving generated content.
- `games_url`: The source URL for game-related data.
- `schedule_path` & `players_path`: Locations for CSV files containing game schedules and player information, respectively.
- `cache_size`: Defines the memory allocation for temporary data storage, enhancing performance and data management.

## Running the Program

To initiate the program with the updated configuration, use Docker Compose from the project's root directory:

```bash
docker compose up -d
```

This command activates all services outlined in your `docker-compose.yml`, operating in detached mode for seamless background execution.

## Security Note

Protect your `config.yaml` from exposure in public repositories to safeguard sensitive information. Incorporate `config.yaml` into your `.gitignore` to avoid accidental commits.

## Troubleshooting

Should any operational issues arise, verify the following:

- Docker's active status on your system.
- Correct placement and formatting of the `config.yaml` in the project's root.
- Accuracy and permission settings of all tokens and IDs in `config.yaml`.
- Accessibility and permission settings of the specified file storage path, particularly for Selenium's operational requirements.

For additional support, consult the application logs or reference the individual service documentations (Selenium, Discord, Telegram) for comprehensive troubleshooting guidance.