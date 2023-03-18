package main

import (
	"GPTBot/api/gpt"
	"GPTBot/api/telegram"
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

var chatHistory = make(map[int64][]gpt.Message)

type Config struct {
	TelegramToken string
	GPTToken      string
	TimeoutValue  int
	MaxMessages   int
	AdminId       int64
}

func main() {
	config, err := readConfig("bot.conf")
	if err != nil {
		log.Fatalf("Error reading bot.conf: %v", err)
	}

	bot, err := telegram.NewBot(config.TelegramToken)
	if err != nil {
		log.Fatal(err)
	}

	gptClient := &gpt.GPTClient{
		ApiKey: config.GPTToken,
	}

	for update := range bot.GetUpdateChannel(config.TimeoutValue) {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID

		// Check for commands
		if update.Message.IsCommand() {
			command := update.Message.Command()
			switch command {
			case "start":
				bot.Answer(chatID, update.Message.MessageID, "Здравствуйте! Я помощник GPT-3.5 Turbo, и я здесь, чтобы помочь вам с любыми вопросами или задачами. Просто напишите ваш вопрос или запрос, и я сделаю все возможное, чтобы помочь вам!")
			case "clear":
				chatHistory[chatID] = nil
				bot.Answer(chatID, update.Message.MessageID, "История разговоров была очищена.")
			case "history":
				historyMessages := formatHistory(chatHistory[chatID])
				for _, message := range historyMessages {
					bot.Answer(chatID, update.Message.MessageID, message)
				}
			case "help":
				helpText := `Список доступных команд и их описание:
/help - Показывает список доступных команд и их описание.
/start - Отправляет приветственное сообщение, описывающее цель бота.
/clear - Очищает историю разговоров для текущего чата.
/history - Показывает всю сохраненную на данный момент историю разговоров в красивом форматировании.`
				bot.Answer(chatID, update.Message.MessageID, helpText)
			}

			continue
		}

		go processUpdate(bot, update, gptClient, config) // Launch a goroutine for each update
	}
}

func formatHistory(history []gpt.Message) []string {
	if len(history) == 0 {
		return []string{"История разговоров пуста."}
	}

	var formattedHistory strings.Builder
	var messages []string
	characterCount := 0

	for i, message := range history {
		formattedLine := fmt.Sprintf("%d. %s: %s\n", i+1, strings.Title(message.Role), message.Content)
		lineLength := len(formattedLine)

		if characterCount+lineLength > 4096 {
			messages = append(messages, formattedHistory.String())
			formattedHistory.Reset()
			characterCount = 0
		}

		formattedHistory.WriteString(formattedLine)
		characterCount += lineLength
	}

	if formattedHistory.Len() > 0 {
		messages = append(messages, formattedHistory.String())
	}

	return messages
}

func processUpdate(bot *telegram.Bot, update telegram.Update, gptClient *gpt.GPTClient, config *Config) {
	if update.Message == nil {
		return
	}

	chatID := update.Message.Chat.ID

	log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

	// Maintain conversation history
	chatHistory[chatID] = append(chatHistory[chatID], gpt.Message{Role: "user", Content: update.Message.Text})
	if len(chatHistory[chatID]) > config.MaxMessages {
		chatHistory[chatID] = chatHistory[chatID][1:]
	}

	responsePayload, err := gptClient.CallGPT35(chatHistory[chatID])
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	response := "I'm sorry, I don't have an answer for that."
	if len(responsePayload.Choices) > 0 {
		response = strings.TrimSpace(responsePayload.Choices[0].Message.Content)
	}

	// Add the assistant's response to the conversation history
	chatHistory[chatID] = append(chatHistory[chatID], gpt.Message{Role: "assistant", Content: response})

	log.Printf("[%s] %s", "ChatGPT", response)
	bot.Answer(chatID, update.Message.MessageID, response)
	if config.AdminId > 0 {
		if chatID != config.AdminId {
			adminMessage := fmt.Sprintf("[User: %s %s (%s, ID: %d)] %s\n[ChatGPT] %s\n",
				update.Message.From.FirstName, update.Message.From.LastName, update.Message.From.UserName, update.Message.From.ID, update.Message.Text,
				response)
			bot.Admin(adminMessage, config.AdminId)
		}
	}
}

func readConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			config[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	timeoutValue, err := strconv.Atoi(config["timeout_value"])
	if err != nil {
		log.Fatalf("Error converting timeout_value to integer: %v", err)
	}
	maxMessages, err := strconv.Atoi(config["max_messages"])
	if err != nil {
		log.Fatalf("Error converting max_messages to integer: %v", err)
	}

	var adminID int64
	if config["admin_id"] != "" {
		adminID, err = strconv.ParseInt(config["admin_id"], 10, 64)
		if err != nil {
			log.Fatalf("Error converting admin_id to integer: %v", err)
		}
	}

	return &Config{
		TelegramToken: config["telegram_token"],
		GPTToken:      config["gpt_token"],
		TimeoutValue:  timeoutValue,
		MaxMessages:   maxMessages,
		AdminId:       adminID,
	}, scanner.Err()
}