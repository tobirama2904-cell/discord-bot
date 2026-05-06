package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

var client = &http.Client{
	Timeout: 10 * time.Second,
}

// ===== KEYS =====
var groqKeys = strings.Split(os.Getenv("GROQ_KEYS"), ",")
var gi int

func nextKey() string {
	if len(groqKeys) == 0 {
		return ""
	}
	k := strings.TrimSpace(groqKeys[gi])
	gi = (gi + 1) % len(groqKeys)
	return k
}

// ===== SAFE AI =====
func askAI(prompt string) string {

	key := nextKey()
	if key == "" {
		return "❌ Нет API ключа"
	}

	body := map[string]interface{}{
		"model": "llama3-8b-8192",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	j, _ := json.Marshal(body)

	req, err := http.NewRequest("POST",
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewBuffer(j),
	)
	if err != nil {
		return "❌ Ошибка запроса"
	}

	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Println("HTTP ERROR:", err)
		return "❌ AI не отвечает"
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		log.Println("API ERROR:", string(b))
		return "❌ Ошибка AI API"
	}

	var r map[string]interface{}
	err = json.Unmarshal(b, &r)
	if err != nil {
		log.Println("JSON ERROR:", err)
		return "❌ Ошибка обработки ответа"
	}

	choices, ok := r["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "❌ Пустой ответ AI"
	}

	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string)

	if msg == "" {
		return "❌ AI вернул пусто"
	}

	return msg
}

// ===== MAIN =====
func main() {

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("Нет DISCORD_TOKEN")
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal("Ошибка Discord:", err)
	}

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		log.Println("MSG:", m.Content)

		s.ChannelTyping(m.ChannelID)

		go func() {

			defer func() {
				if err := recover(); err != nil {
					log.Println("PANIC:", err)
					s.ChannelMessageSend(m.ChannelID, "❌ Бот словил ошибку")
				}
			}()

			res := askAI(m.Content)

			if res == "" {
				res = "❌ Пустой ответ"
			}

			_, err := s.ChannelMessageSend(m.ChannelID, res)
			if err != nil {
				log.Println("SEND ERROR:", err)
			}

		}()
	})

	err = dg.Open()
	if err != nil {
		log.Fatal("Ошибка запуска:", err)
	}

	log.Println("✅ BOT WORKING")

	select {}
}
