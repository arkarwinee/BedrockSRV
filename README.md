# BedrockSRV

A lightweight Discord ↔ Minecraft Bedrock chat bridge, written in Go. Inspired by [DiscordSRV](https://github.com/DiscordSRV/DiscordSRV) (the popular Java/Paper plugin), but built for **Bedrock Edition** servers, which have no equivalent plugin ecosystem — BedrockSRV connects to your Bedrock server as a regular client (using [gophertunnel](https://github.com/Sandertv/gophertunnel)) rather than requiring a server-side mod or behavior pack.

## Features

- **Two-way chat relay** — Minecraft chat appears in Discord, Discord messages appear in Minecraft.
- **Per-player identity via webhook** — each player's message posts to Discord under their own name and avatar (fetched from their skin), instead of everything showing up as one generic bot.
- **Join/leave notifications** — posted to Discord as embeds, driven by the game's player list packets (not fragile chat-text parsing).
- **`!players` command** — lists everyone currently online, right from Discord.
- **Auto-reconnect** — if the Minecraft server restarts or the connection drops, BedrockSRV retries automatically and posts a status update to Discord.
- **No server-side install required** — connects as an offline/guest client. You don't need admin access to the Bedrock server, behavior pack support, or experimental APIs enabled.
- **Single static binary** — no runtime dependencies once built; easy to deploy anywhere Go can build for (Linux, Windows, macOS).

## How it works

BedrockSRV runs as a small standalone Go program with two things happening concurrently:

1. **Discord side** — connects to Discord's gateway using [discordgo](https://github.com/bwmarrin/discordgo), listening for messages in a configured channel and relaying them to Minecraft as chat packets.
2. **Minecraft side** — connects to your Bedrock server using [gophertunnel](https://github.com/Sandertv/gophertunnel), joining as an offline "bot" player, reading chat/player-list packets, and relaying them to Discord.

Because it connects like a normal player rather than modifying the server, it works with any Bedrock Dedicated Server (BDS) you can reach over the network — no plugins, add-ons, or world file changes needed.

## Requirements

- Go 1.23 or later (check [go.dev/dl](https://go.dev/dl/) for the current release)
- A Discord bot application and token ([Discord Developer Portal](https://discord.com/developers/applications))
- A Discord channel to bridge, and (optionally) a webhook URL for that channel
- Network access from wherever you run BedrockSRV to your Bedrock server's IP and port (outbound UDP)

## Setup

### 1. Clone and build

```bash
git clone https://github.com/arkarwinee/BedrockSRV.git
cd BedrockSRV
go mod tidy
go build -o bedrocksrv .
```

### 2. Create a Discord bot

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications) → **New Application**.
2. Under **Bot**, create a bot and copy its token.
3. Under **Bot → Privileged Gateway Intents**, enable **Message Content Intent**.
4. Under **OAuth2 → URL Generator**, select the `bot` scope and permissions to read/send messages, then use the generated URL to invite it to your server.
5. Right-click the channel you want to bridge (with Developer Mode enabled in Discord) → **Copy Channel ID**.

### 3. (Optional but recommended) Create a webhook

In the target channel's settings → **Integrations → Webhooks → New Webhook**. Copy its URL. This lets each Minecraft player's message appear in Discord under their own name and avatar, instead of all messages coming from the bot account.

### 4. Configure environment variables

Copy the example file and fill it in:

```bash
cp .env.example .env
```

```env
DISCORD_TOKEN=your_bot_token
DISCORD_CHANNEL_ID=your_channel_id
DISCORD_WEBHOOK_URL=your_webhook_url   # optional
MC_HOST=your.server.address:19132
```

### 5. Run it

```bash
./bedrocksrv
```

You should see log output confirming both the Discord and Minecraft connections, followed by `Bridge running.`

## Deploying on Ubuntu (systemd)

For a persistent deployment that survives reboots and restarts automatically on failure:

```bash
sudo nano /etc/systemd/system/bedrocksrv.service
```

```ini
[Unit]
Description=BedrockSRV - Minecraft Bedrock <-> Discord bridge
After=network.target

[Service]
Type=simple
User=YOUR_USERNAME
WorkingDirectory=/home/YOUR_USERNAME/BedrockSRV
ExecStart=/home/YOUR_USERNAME/BedrockSRV/bedrocksrv
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now bedrocksrv
journalctl -u bedrocksrv -f   # view logs
```

## Configuration reference

| Variable | Required | Description |
|---|---|---|
| `DISCORD_TOKEN` | Yes | Your Discord bot's token. |
| `DISCORD_CHANNEL_ID` | Yes | The Discord channel to relay chat to/from. |
| `DISCORD_WEBHOOK_URL` | No | If set, Minecraft messages post under each player's own name/avatar instead of the bot account. |
| `MC_HOST` | Yes | The Bedrock server's address, in `host:port` form (e.g. `play.example.com:19132`). |

## Commands

| Command | Where | Description |
|---|---|---|
| `!players` | Discord | Lists all players currently online. |

## Notes and limitations

- BedrockSRV connects in **offline mode** — it doesn't authenticate with Xbox Live, so it only works against servers that accept offline/guest connections (most self-hosted BDS servers with online-mode disabled).
- The bot joins as an actual player slot on your server. Make sure your player limit accounts for this.
- Reusing an existing Discord bot token (e.g. one already used by DiscordSRV on a Java server) is fine — just point BedrockSRV at a separate channel. Discord allows multiple simultaneous sessions per bot token.
- Resource pack downloading is intentionally skipped (`DownloadResourcePack` returns `false`) since the bridge never renders anything — this also avoids a known chunk-request loop bug in some client libraries against certain server builds.

## Built with

- [gophertunnel](https://github.com/Sandertv/gophertunnel) — Bedrock protocol client
- [discordgo](https://github.com/bwmarrin/discordgo) — Discord API client
- [godotenv](https://github.com/joho/godotenv) — `.env` file loading

## License

Add a license of your choice (MIT is a common pick for small utility projects like this).
