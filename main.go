package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// --- Config, loaded from environment ---
var (
	discordToken   string
	discordChannel string
	webhookURL     string
	mcHost         string
)

// --- Shared state ---
var (
	mcConn          *minecraft.Conn
	mcConnMu        sync.RWMutex
	onlinePlayers   = map[string]string{} // uuid -> name
	onlinePlayersMu sync.Mutex
	botName         string
)

func main() {
	godotenv.Load() // silently ignores if .env doesn't exist; env vars still work

	discordToken = os.Getenv("DISCORD_TOKEN")
	discordChannel = os.Getenv("DISCORD_CHANNEL_ID")
	webhookURL = os.Getenv("DISCORD_WEBHOOK_URL")
	mcHost = os.Getenv("MC_HOST")

	if discordToken == "" || discordChannel == "" || mcHost == "" {
		log.Fatal("Missing required env vars: DISCORD_TOKEN, DISCORD_CHANNEL_ID, MC_HOST")
	}

	dg, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		log.Fatal("Discord session error:", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	dg.AddHandler(onDiscordMessage)
	dg.AddHandler(onReady)

	if err := dg.Open(); err != nil {
		log.Fatal("Discord connect error:", err)
	}
	defer dg.Close()
	log.Println("Connected to Discord.")

	// Run the Minecraft side with auto-reconnect, in the background.
	go minecraftLoop(dg)

	log.Println("Bridge running. Press Ctrl+C to stop.")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("Shutting down...")
}

func onReady(s *discordgo.Session, r *discordgo.Ready) {
	s.UpdateGameStatus(0, "Minecraft Bedrock")
}

// --- Minecraft connection lifecycle, with reconnect ---
func minecraftLoop(dg *discordgo.Session) {
	for {
		conn, err := connectMinecraft()
		if err != nil {
			log.Println("Minecraft connect failed, retrying in 15s:", err)
			sendSystemEmbed(dg, "⚠️ Failed to connect to Minecraft server, retrying...", 0xE74C3C)
			time.Sleep(15 * time.Second)
			continue
		}

		mcConnMu.Lock()
		mcConn = conn
		botName = conn.IdentityData().DisplayName
		mcConnMu.Unlock()

		log.Println("Connected to Minecraft as", botName)
		sendSystemEmbed(dg, "✅ Bridge connected to Minecraft server.", 0x2ECC71)

		readMinecraftPackets(conn, dg) // blocks until disconnected

		mcConnMu.Lock()
		mcConn = nil
		mcConnMu.Unlock()

		onlinePlayersMu.Lock()
		onlinePlayers = map[string]string{}
		onlinePlayersMu.Unlock()

		log.Println("Minecraft connection lost, reconnecting in 15s...")
		sendSystemEmbed(dg, "🔌 Lost connection to Minecraft server, reconnecting...", 0xE67E22)
		time.Sleep(15 * time.Second)
	}
}

func connectMinecraft() (*minecraft.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialer := minecraft.Dialer{
		DownloadResourcePack: func(id uuid.UUID, version string, current, total int) bool {
			return false
		},
		TokenSource: nil,
		IdentityData: login.IdentityData{
			DisplayName: "BridgeBot",
			Identity:    "79675fd6-711c-4884-a0ce-85df17a2088c",
		},
	}

	conn, err := dialer.DialContext(ctx, "raknet", mcHost)
	if err != nil {
		return nil, err
	}
	if err := conn.DoSpawn(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// --- Minecraft -> Discord ---
func readMinecraftPackets(conn *minecraft.Conn, dg *discordgo.Session) {
	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			return // triggers reconnect loop in minecraftLoop
		}

		switch p := pk.(type) {
		case *packet.Text:
			handleChat(p, dg)
		case *packet.PlayerList:
			log.Printf("[MC] PlayerList packet received: %T\n", p)

			handlePlayerList(p, dg)
		}
	}
}

func handleChat(text *packet.Text, dg *discordgo.Session) {
	if text.TextType != packet.TextTypeChat {
		return
	}
	if text.SourceName == (botName + "§r") {
		return // our own echoed message
	}

	name := strings.TrimSuffix(text.SourceName, "§r")
	log.Printf("[MC] %s: %s", name, text.Message)

	if webhookURL != "" {
		sendWebhookMessage(name, text.Message)
	} else {
		dg.ChannelMessageSend(discordChannel, fmt.Sprintf("**%s**: %s", name, text.Message))
	}
}

func handlePlayerList(pk *packet.PlayerList, dg *discordgo.Session) {
	for _, entry := range pk.Entries {
		log.Println("[MC] ActionType: ", entry.ActionType)
		name := strings.TrimSuffix(entry.Username, "§r")
		if name == "BridgeBot" {
			log.Println("Skipped", name)
			continue // skip the bridge bot itself
		}
		log.Println("Proceed", name)

		key := entry.UUID.String()
		avatarUrl := func(name string) string {
			return "https://mc-heads.net/avatar/" + name + "/64"
		}

		switch entry.ActionType {
		case protocol.PlayerListActionAdd:
			onlinePlayersMu.Lock()
			_, already := onlinePlayers[key]
			onlinePlayers[key] = name
			onlinePlayersMu.Unlock()

			if !already {
				log.Println("[MC] Join:", name)
				sendSystemEmbedWithImage(dg, fmt.Sprintf("%s joined the game", name), 0x2ECC71, avatarUrl(name))
			}
		case protocol.PlayerListActionRemove:
			onlinePlayersMu.Lock()
			storedName, existed := onlinePlayers[key]
			delete(onlinePlayers, key)
			onlinePlayersMu.Unlock()

			if existed {
				log.Println("[MC] Leave:", storedName)
				sendSystemEmbedWithImage(dg, fmt.Sprintf("%s left the game", storedName), 0xE74C3C, avatarUrl(storedName))
			}
		}
	}
}

// --- Discord -> Minecraft ---
func onDiscordMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}
	if m.ChannelID != discordChannel {
		return
	}

	// Simple command: !players
	if strings.TrimSpace(m.Content) == "!players" {
		sendPlayerList(s)
		return
	}

	mcConnMu.RLock()
	conn := mcConn
	mcConnMu.RUnlock()

	if conn == nil {
		s.ChannelMessageSend(discordChannel, "⚠️ Not connected to the Minecraft server right now.")
		return
	}

	msg := fmt.Sprintf("[Discord] %s: %s", m.Author.Username, m.Content)
	log.Println("[Discord ->]", msg)

	conn.WritePacket(&packet.Text{
		TextType: packet.TextTypeChat,
		Message:  msg,
	})
}

func sendPlayerList(s *discordgo.Session) {
	onlinePlayersMu.Lock()
	defer onlinePlayersMu.Unlock()

	if len(onlinePlayers) == 0 {
		s.ChannelMessageSend(discordChannel, "No players online.")
		return
	}

	var names []string
	for _, name := range onlinePlayers {
		names = append(names, name)
	}
	s.ChannelMessageSend(discordChannel, fmt.Sprintf("**Online (%d):** %s", len(names), strings.Join(names, ", ")))
}

// --- Helpers ---
func sendSystemEmbed(dg *discordgo.Session, text string, color int) {
	dg.ChannelMessageSendEmbed(discordChannel, &discordgo.MessageEmbed{
		Description: text,
		Color:       color,
	})
}

func sendSystemEmbedWithImage(dg *discordgo.Session, text string, color int, icon string) {
	dg.ChannelMessageSendEmbed(discordChannel, &discordgo.MessageEmbed{
		Color: color,
		Author: &discordgo.MessageEmbedAuthor{
			Name:    text,
			IconURL: icon,
		},
	})
}

func sendWebhookMessage(username, content string) {
	params := &discordgo.WebhookParams{
		Content:   content,
		Username:  username,
		AvatarURL: fmt.Sprintf("https://mc-heads.net/avatar/%s/64", username),
	}
	req, _ := discordgo.New("")
	_ = req
	// Using raw HTTP since we're not attaching this to a bot session for the webhook call.
	postWebhook(params)
}

func postWebhook(params *discordgo.WebhookParams) {
	body, _ := json.Marshal(params)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Println("Webhook send failed:", err)
		return
	}
	resp.Body.Close()
}
