# Mega games result sender

This document describes the setup and execution instructions for the automation program. This program integrates with various services including web automation through Selenium, message fetching from Discord, and sending notifications to Telegram. Ensure you have Docker installed on your system to follow the setup instructions.

## Configuration

Before running the program, you must create a `config.yaml` file in the root directory of the project with the following structure:

```yaml
app:
  middle:
    type: "selenium"
    middle_url: "http://selenium-chrome:4444/wd/hub" # URL to the Selenium Hub
  source:
    type: "discord"
    token: "DISCORD_BOT_TOKEN" # Your Discord bot token
    channel_id: "DISCORD_CHANNEL_ID" # ID of the Discord channel
  target:
    type: "telegram"
    token: "TELEGRAM_BOT_TOKEN" # Your Telegram bot token
    channel_id: "TELEGRAM_CHANNEL_ID" # ID of the Telegram channel or chat
  deep_history: NUMBER_OF_MESSAGES # Number of past messages to process
  file_storage_path: "PATH_TO_STORAGE" # Path to store screenshots or files
  games_url: "URL_TO_FETCH_GAMES" # URL to fetch game information
```

### Field Descriptions:

- `middle_url`: The URL where your Selenium Hub is accessible.
- `token` (Discord and Telegram): The bot tokens for Discord and Telegram. These are necessary for the application to interact with your Discord and Telegram accounts.
- `channel_id` (Discord and Telegram): The IDs of the channels where messages will be fetched and sent, respectively.
- `deep_history`: The number of past messages the program will process on startup.
- `file_storage_path`: The local path where the program will save any screenshots or files it generates.
- `games_url`: The URL from which the program will fetch game-related information. Example: "https://neonsportz.com/leagues/MEGA/games/"

## Running the Program

Once you have configured `config.yaml` as described above, you can start the program using Docker Compose. Ensure Docker Compose is installed and run the following command from the root directory of your project:

```bash
docker compose up -d
```

This command will start all the necessary services defined in your `docker-compose.yml` file in detached mode.

## Security Note

Ensure that your `config.yaml` does not get committed to any public repositories as it contains sensitive tokens. It is recommended to add `config.yaml` to your `.gitignore` file.

## Troubleshooting

If you encounter any issues while running the program, ensure that:

- Docker is running on your system.
- The `config.yaml` file is correctly placed in the root directory.
- The tokens and IDs provided in `config.yaml` are valid and have the necessary permissions.
- The file storage path specified in `config.yaml` is accessible and writable by the Selenium user. If Selenium is unable to write to this path, you may encounter errors related to file handling or screenshot saving.

For further assistance, consider checking the application logs or consulting the documentation of the individual services (Selenium, Discord, Telegram) for more detailed troubleshooting steps.