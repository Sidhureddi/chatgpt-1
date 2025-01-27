package main

import (
	"GPTBot/api/gpt"
	"GPTBot/api/telegram"
	"GPTBot/storage"
	"GPTBot/util"
	"fmt"
	"log"
	"strings"
)

// worker function that processes updates
func worker(updateChan <-chan telegram.Update, bot *telegram.Bot, gptClient *gpt.GPTClient, config *Config) {
	for update := range updateChan {
		processUpdate(bot, update, gptClient, config)
		botStorage.Save()
	}
}

func processUpdate(bot *telegram.Bot, update telegram.Update, gptClient *gpt.GPTClient, config *Config) {
	chatID := update.Message.Chat.ID
	fromID := update.Message.From.ID

	chat, _ := botStorage.Get(chatID)

	// Check for command
	if update.Message.IsCommand() {
		command := update.Message.Command()
		switch command {
		case "start":
			commandStart(bot, update, chat)
		case "clear":
			commandClear(bot, update, chat)
		case "history":
			commandHistory(bot, update, chat)
		case "rollback":
			commandRollback(bot, update, chat)
		case "help":
			commandHelp(bot, update, chat)
		case "translate":
			commandTranslate(bot, update, gptClient, chat)
		case "grammar":
			commandGrammar(bot, update, gptClient, chat)
		case "enhance":
			commandEnhance(bot, update, gptClient, chat)
		case "imagine":
			commandImagine(bot, update, gptClient, chat, config)
		case "temperature":
			commandTemperature(bot, update, chat)
		case "model":
			commandModel(bot, update, chat)
		case "system":
			commandSystem(bot, update, chat)
		case "summarize":
			commandSummarize(bot, update, gptClient, chat, config)
		default:
			if fromID != config.AdminId {
				// bot.Reply(chat.ChatID, update.Message.MessageID, fmt.Sprintf("Неизвестная команда /%s", command))
				break
			}

			switch command {
			case "reload":
				commandReload(bot, update, chat)
			case "adduser":
				commandAddUser(bot, update, chat, config)
			case "removeuser":
				commandRemoveUser(bot, update, chat, config)
			}
		}

		return
	}

	gptChat(bot, update, gptClient, config, chat, fromID)
}

func gptText(bot *telegram.Bot, chat *storage.Chat, messageID int, gptClient *gpt.GPTClient, systemPrompt, userPrompt string) {
	responsePayload, err := gptClient.CallGPT35([]gpt.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, chat.Settings.Model, 0.6)

	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	response := "Произошла ошибка с получением ответа, пожалуйста, попробуйте позднее"
	if len(responsePayload.Choices) > 0 {
		response = strings.TrimSpace(responsePayload.Choices[0].Message.Content)
	}

	log.Printf("[%s] %s", "ChatGPT", response)
	bot.Reply(chat.ChatID, messageID, response)
}

func gptImage(bot *telegram.Bot, chatID int64, gptClient *gpt.GPTClient, prompt string, config *Config) {
	imageUrl, err := gptClient.GenerateImage(prompt, gpt.ImageSize1024)
	if err != nil {
		log.Printf("Error generating image: %v", err)
		return
	}

	enhancedCaption := prompt
	responsePayload, err := gptClient.CallGPT35([]gpt.Message{
		{Role: "system", Content: "You are an assistant that generates natural language description (prompt) for an artificial intelligence (AI) that generates images"},
		{Role: "user", Content: fmt.Sprintf("Please improve this prompt: \"%s\". Answer with improved prompt only. Keep prompt at most 200 characters long. Your prompt must be in one sentence.", prompt)},
	}, "gpt-3.5-turbo", 0.7)
	if err == nil {
		enhancedCaption = strings.TrimSpace(responsePayload.Choices[0].Message.Content)
	}

	err = bot.SendImage(chatID, imageUrl, enhancedCaption)
	if err != nil {
		log.Printf("Error sending image: %v", err)
		return
	}

	log.Printf("[ChatGPT] sent image %s", imageUrl)
	if config.AdminId > 0 {
		if chatID != config.AdminId {
			bot.Message(fmt.Sprintf("Image with prompt \"%s\" sent to chat %d", prompt, chatID), config.AdminId, false)
		}
	}
}

func gptChat(bot *telegram.Bot, update telegram.Update, gptClient *gpt.GPTClient, config *Config, chat *storage.Chat, fromID int64) {
	log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

	if chat.ChatID < 0 { // group chat
		isReplyToBot := update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.From.UserName == bot.Username
		if !strings.Contains(update.Message.Text, "@"+bot.Username) && !isReplyToBot {
			return
		}

		if strings.Contains(update.Message.Text, "@"+bot.Username) {
			update.Message.Text = strings.Replace(update.Message.Text, "@"+bot.Username, "", -1)
		}
	}

	// Maintain conversation history
	userMessage := storage.Message{Role: "user", Content: update.Message.Text}
	historyEntry := &storage.ConversationEntry{Prompt: userMessage, Response: storage.Message{}}

	chat.History = append(chat.History, historyEntry)
	if len(chat.History) > chat.Settings.MaxMessages {
		excessMessages := len(chat.History) - chat.Settings.MaxMessages
		chat.History = chat.History[excessMessages:]
	}

	var messages []gpt.Message
	if chat.Settings.SystemPrompt != "" {
		messages = append(messages, gpt.Message{Role: "system", Content: chat.Settings.SystemPrompt})
	}
	messages = append(messages, messagesFromHistory(chat.History)...)

	// quality of life, we do 2 requests if the first one fails
	tryLimit := 2
	response := "Произошла ошибка с получением ответа, пожалуйста, попробуйте позднее"
	for i := 0; i < tryLimit; i++ {
		responsePayload, err := gptClient.CallGPT35(messages, chat.Settings.Model, chat.Settings.Temperature)
		if err != nil {
			log.Printf("Error: %v", err)
			return
		}

		if len(responsePayload.Choices) > 0 {
			response = strings.TrimSpace(responsePayload.Choices[0].Message.Content)
			break
		}
	}

	// Add the assistant's response to the conversation history
	historyEntry.Response = storage.Message{Role: "assistant", Content: response}

	log.Printf("[%s] %s", "ChatGPT", response)
	if chat.Settings.UseMarkdown {
		bot.ReplyMarkdown(chat.ChatID, update.Message.MessageID, response)
	} else {
		bot.Reply(chat.ChatID, update.Message.MessageID, response)
	}

	if config.AdminId == 0 {
		return
	}

	if fromID == config.AdminId {
		return
	}

	if util.IsIdInList(fromID, config.IgnoreReportIds) {
		return
	}

	bot.Message(fmt.Sprintf("[User: %s %s (%s, ID: %d)] %s\n[ChatGPT] %s\n", update.Message.From.FirstName, update.Message.From.LastName, update.Message.From.UserName, update.Message.From.ID, update.Message.Text, response), config.AdminId, false)
}
