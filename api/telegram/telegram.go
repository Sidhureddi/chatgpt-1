package telegram

import (
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"log"
)

type Bot struct {
	api *tgbotapi.BotAPI
}

type UpdatesChannel <-chan Update
type Update tgbotapi.Update

func NewBot(token string) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	bot := &Bot{
		api: api,
	}

	log.Printf("Authorized on account %s", bot.api.Self.UserName)

	return bot, nil
}

func (botInstance *Bot) GetUpdateChannel(timeout int) UpdatesChannel {
	botInstance.api.Debug = false

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = timeout

	updates, _ := botInstance.api.GetUpdatesChan(updateConfig)

	ourChannel := make(chan Update)
	go func(channel tgbotapi.UpdatesChannel) {
		defer close(ourChannel)
		for update := range channel {
			ourChannel <- Update(update)
		}
	}(updates)

	return ourChannel
}

func (botInstance *Bot) Answer(chatId int64, replyTo int, message string) {
	msg := tgbotapi.NewMessage(chatId, message)
	msg.ReplyToMessageID = replyTo

	_, err := botInstance.api.Send(msg)
	if err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

func (botInstance *Bot) Admin(message string, adminId int64) {
	msg := tgbotapi.NewMessage(adminId, message)
	_, err := botInstance.api.Send(msg)
	if err != nil {
		log.Printf("Error sending message: %v", err)
	}
}