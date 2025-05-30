# Mega Games Result Sender

Mega Games Result Sender is an automation service that connects Discord, Selenium, and Telegram to streamline the process of capturing, processing, and forwarding game results and notifications. It is designed for tournament organizers and communities who need to automate the flow of match results from Discord to Telegram channels, including screenshots and notifications.

## Features
- **Discord Integration:** Listens to messages in a specified Discord channel.
- **Selenium Automation:** Interacts with web pages (e.g., for screenshots or data extraction) using Selenium running in a container.
- **Telegram Notifications:** Sends processed results, announcements, and screenshots to designated Telegram channels.
- **Configurable Workflow:** All behavior is managed via a YAML configuration file.
- **Dockerized Deployment:** Easily run all components in isolated containers.

## Architecture
```
Discord Channel → [Discord Bot] → [Selenium Automation] → [Telegram Bot]
```
- The Discord bot listens for relevant messages (e.g., match results) and passes them to the automation pipeline.
- Selenium performs any required web automation (e.g., taking screenshots of match results).
- The Telegram bot sends notifications and files to the appropriate Telegram channels.

## Configuration
Create a `config.yaml` file in the project root with the following structure:

```yaml
app:
  middle:
    type: "selenium"
    middle_url: "http://localhost:4444/wd/hub"   # Selenium Hub URL
  source:
    type: "discord"
    token: "DISCORD_BOT_TOKEN"                    # Discord bot token
    channel_id: "DISCORD_CHANNEL_ID"              # Discord channel ID
  target:
    type: "telegram"
    token: "TELEGRAM_BOT_TOKEN"                   # Telegram bot token
    common_channel_id: "TELEGRAM_COMMON_CHANNEL_ID" # Common Telegram channel/chat ID
    news_channel_id: "TELEGRAM_NEWS_CHANNEL_ID"     # News Telegram channel/chat ID
  deep_history: 50                                 # Number of past messages to analyze
  file_storage_path: "./screens"                  # Where screenshots/files are saved
  games_url: "https://example.com/games"          # URL to fetch game information
  schedule_path: "./MEGA_games.csv"               # Path to game schedule CSV
  players_path: "./MEGA_teams.csv"                # Path to players CSV
  cache_size: 100                                  # Cache size for temporary data
```

### Parameter Descriptions
- **Discord Section:**
  - `token`: Discord bot authentication token
  - `channel_id`: Channel where the bot listens for messages
- **Telegram Section:**
  - `token`: Telegram bot authentication token
  - `common_channel_id`: Main Telegram channel for general notifications
  - `news_channel_id`: Telegram channel for news/announcements
- **Selenium Section:**
  - `middle_url`: URL of the Selenium Hub (default: local container)
- **Other Fields:**
  - `deep_history`: How many previous messages to process on startup
  - `file_storage_path`: Local directory for screenshots/files
  - `games_url`: Source of game information
  - `schedule_path` & `players_path`: CSV files for schedules and players
  - `cache_size`: In-memory cache size for performance

## Usage
1. **Build and Start Services:**
   Ensure Docker is installed and running. From the project root, run:
   ```bash
   docker compose up -d
   ```
   This will start both the Selenium service and the main application.

2. **Logs and Monitoring:**
   Check logs using:
   ```bash
   docker compose logs -f
   ```

3. **Stopping Services:**
   ```bash
   docker compose down
   ```

## Security & Best Practices
- Do **NOT** commit `config.yaml` or any secrets to version control. This file is already listed in `.gitignore`.
- Restrict access to your storage directories and keep bot tokens secure.

## Troubleshooting
- Ensure all tokens and channel IDs in `config.yaml` are valid and have correct permissions.
- The Selenium service must be running and accessible at the specified `middle_url`.
- The storage path (e.g., `./screens`) must be writable by the Docker container.
- Place all referenced CSV files (`MEGA_games.csv`, `MEGA_teams.csv`) in the project root.
- For advanced issues, consult logs or refer to the documentation for Discord, Selenium, or Telegram bots.

---
For further assistance, check application logs or contact the project maintainer.