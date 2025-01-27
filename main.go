package main

import (
	"GPTBot/api/gpt"
	"GPTBot/api/telegram"
	"GPTBot/storage"
	"GPTBot/util"
	"fmt"
	"log"
	"strings"
	"time"
)

var botStorage storage.Storage

type ConversationEntry struct {
	Prompt   gpt.Message
	Response gpt.Message
}

func main() {
	// CONFIGURATION
	config, err := readConfig("bot.conf")
	if err != nil {
		log.Fatalf("Error reading bot.conf: %v", err)
	}

	// TELEGRAM BOT INITIALIZATION
	bot, err := telegram.NewBot(config.TelegramToken)
	if err != nil {
		log.Fatal(err)
	}
	bot.SetCommandList(config.CommandMenu)

	// GPT CLIENT INITIALIZATION
	gptClient := &gpt.GPTClient{
		ApiKey: config.GPTToken,
	}

	// buffer up to 100 update messages
	updateChan := make(chan telegram.Update, 100)

	// create a pool of worker goroutines
	numWorkers := 10
	for i := 0; i < numWorkers; i++ {
		go worker(updateChan, bot, gptClient, config)
	}

	// Here we can choose any type of implemented storage
	botStorage, err = storage.NewFileStorage("data")
	if err != nil {
		log.Fatalf("Error creating storage: %v", err)
	}

	for update := range bot.GetUpdateChannel(config.TimeoutValue) {
		// Ignore any non-Message Updates
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID
		chat, ok := botStorage.Get(chatID)
		if !ok {
			chat = &storage.Chat{
				ChatID: update.Message.Chat.ID,
				Settings: storage.ChatSettings{
					Temperature:  0.8,
					Model:        "gpt-3.5-turbo",
					MaxMessages:  config.MaxMessages,
					UseMarkdown:  false,
					SystemPrompt: "You are a helpful ChatGPT bot based on OpenAI GPT Language model. You are a helpful assistant that always tries to help and answer with relevant information as possible.",
				},
				History:          make([]*storage.ConversationEntry, 0),
				ImageGenNextTime: time.Now(),
			}
			_ = botStorage.Set(chatID, chat)
		}

		if !update.Message.IsCommand() {
			// putting history to log file
			// every newline is a new message
			var lines []string
			name := update.Message.From.FirstName + " " + update.Message.From.LastName
			for _, v := range strings.Split(update.Message.Text, "\n") {
				if v != "" {
					lines = append(lines, v)
				}
			}

			// для групповых чатов указываем имя пользователя
			if chat.ChatID < 0 {
				for i := range lines {
					lines[i] = fmt.Sprintf("%s: %s", name, lines[i])
				}
			}

			// saving lines to log file
			util.AddLines(fmt.Sprintf("log/%d.log", chat.ChatID), lines)
		}

		// If no authorized users are provided, make the bot public
		if len(config.AuthorizedUserIds) > 0 {
			if !util.IsIdInList(update.Message.From.ID, config.AuthorizedUserIds) {
				if update.Message.Chat.Type == "private" {
					bot.Reply(chatID, update.Message.MessageID, "Sorry, you do not have access to this bot.")
					log.Printf("Unauthorized access attempt by user %d: %s %s (%s)", update.Message.From.ID, update.Message.From.FirstName, update.Message.From.LastName, update.Message.From.UserName)

					// Notify the admin
					if config.AdminId > 0 {
						adminMessage := fmt.Sprintf("Unauthorized access attempt by user %d: %s %s (%s)", update.Message.From.ID, update.Message.From.FirstName, update.Message.From.LastName, update.Message.From.UserName)
						bot.Message(adminMessage, config.AdminId, false)
					}
				}
				continue
			}
		}

		// Send the Update to the worker goroutines via the channel
		updateChan <- update
	}
}

func formatHistory(history []gpt.Message) []string {
	if len(history) == 0 {
		return []string{"История разговоров пуста."}
	}

	var historyMessage string
	var historyMessages []string
	characterCount := 0

	for i, message := range history {
		formattedLine := fmt.Sprintf("%d. %s: %s\n", i+1, util.Title(message.Role), message.Content)
		lineLength := len(formattedLine)

		if characterCount+lineLength > 4096 {
			historyMessages = append(historyMessages, historyMessage)
			historyMessage = ""
			characterCount = 0
		}

		historyMessage += formattedLine
		characterCount += lineLength
	}

	if len(historyMessage) > 0 {
		historyMessages = append(historyMessages, historyMessage)
	}

	return historyMessages
}

func messagesFromHistory(storageHistory []*storage.ConversationEntry) []gpt.Message {
	var history []*ConversationEntry
	for _, entry := range storageHistory {
		prompt := entry.Prompt
		response := entry.Response

		history = append(history, &ConversationEntry{
			Prompt:   gpt.Message{Role: prompt.Role, Content: prompt.Content},
			Response: gpt.Message{Role: response.Role, Content: response.Content},
		})
	}

	var messages []gpt.Message
	for _, entry := range history {
		messages = append(messages, entry.Prompt)
		if entry.Response != (gpt.Message{}) {
			messages = append(messages, entry.Response)
		}
	}
	return messages
}
