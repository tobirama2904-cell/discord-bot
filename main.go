package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func askUnlimitedAI(prompt, role string) string {
	systemDesc := "Ты менеджер империи. Твоя цель управлять системой."
	if role == "realizer" {
		systemDesc = "Ты реализатор империи. Ты пишешь код и создаешь проекты."
	}

	cleanPrompt := url.QueryEscape(prompt)
	cleanSystem := url.QueryEscape(systemDesc)

	apiURL := fmt.Sprintf("https://pollinations.ai/%s?system=%s", cleanPrompt, cleanSystem)

	resp, err := http.Get(apiURL)
	if err != nil {
		return "❌ Ошибка: ИИ недоступен."
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "❌ Ошибка чтения ответа."
	}

	return string(body)
}

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN не найден")
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal("Ошибка создания сессии:", err)
	}

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.ID == s.State.User.ID {
			return
		}

		if strings.HasPrefix(m.Content, "!") {
			s.ChannelTyping(m.ChannelID)
			query := strings.TrimPrefix(m.Content, "!")
			ans := askUnlimitedAI(query, "manager")
			s.ChannelMessageSend(m.ChannelID, "**[МЕНЕДЖЕР]:**\n"+ans)
		}

		if strings.HasPrefix(m.Content, ".") {
			s.ChannelTyping(m.ChannelID)
			query := strings.TrimPrefix(m.Content, ".")
			ans := askUnlimitedAI(query, "realizer")
			s.ChannelMessageSend(m.ChannelID, "**[РЕАЛИЗАТОР]:**\n"+ans)
		}
	})

	// retry подключение
	for i := 0; i < 5; i++ {
		err = dg.Open()
		if err == nil {
			break
		}
		log.Println("Ошибка подключения, пробую снова...", err)
		time.Sleep(5 * time.Second)
	}

	if err != nil {
		log.Fatal("Не удалось запустить бота:", err)
	}

	log.Println("Бот запущен")

	select {}
}
