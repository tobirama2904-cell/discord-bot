package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

var client = &http.Client{Timeout: 12 * time.Second}

// ===== KEYS =====
var keys = strings.Split(os.Getenv("DEEPSEEK_KEYS"), ",")
var ki int
var dead = make(map[string]time.Time)
var mu sync.Mutex

func nextKey() string {
	mu.Lock()
	defer mu.Unlock()

	for i := 0; i < len(keys); i++ {
		k := strings.TrimSpace(keys[ki])
		ki = (ki + 1) % len(keys)

		if t, ok := dead[k]; ok && time.Since(t) < time.Minute {
			continue
		}
		return k
	}
	return ""
}

func markDead(k string) {
	mu.Lock()
	dead[k] = time.Now()
	mu.Unlock()
}

// ===== CACHE =====
var cache sync.Map

func getCache(k string) (string, bool) {
	v, ok := cache.Load(k)
	if !ok {
		return "", false
	}
	return v.(string), true
}

func setCache(k, v string) {
	cache.Store(k, v)
}

// ===== MEMORY =====
var memory sync.Map

func getCtx(user string) string {
	v, _ := memory.LoadOrStore(user, "")
	return v.(string)
}

func updateCtx(user, text string) {
	v, _ := memory.LoadOrStore(user, "")
	s := v.(string) + "\n" + text

	// 🔥 сжатие (экономия)
	if len(s) > 1200 {
		s = s[len(s)-1200:]
	}

	memory.Store(user, s)
}

// ===== AI =====
func askDeepSeek(prompt string) string {

	// ⚡ cache
	if v, ok := getCache(prompt); ok {
		return "⚡ " + v
	}

	// 🔥 экономия токенов
	if len(prompt) > 1200 {
		prompt = prompt[len(prompt)-1200:]
	}

	key := nextKey()
	if key == "" {
		return "⚠️ нет доступных ключей"
	}

	body := map[string]interface{}{
		"model": "deepseek-chat",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST",
		"https://api.deepseek.com/v1/chat/completions",
		bytes.NewBuffer(j),
	)

	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		markDead(key)
		return "⚠️ сеть / API ошибка"
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 429 {
		markDead(key)
		return "⚠️ лимит ключа"
	}

	if resp.StatusCode != 200 {
		return "⚠️ ошибка AI"
	}

	var r map[string]interface{}
	json.Unmarshal(b, &r)

	choices, ok := r["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "⚠️ пустой ответ"
	}

	out := choices[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string)

	setCache(prompt, out)
	return out
}

// ===== MAIN =====
func main() {

	dg, _ := discordgo.New("Bot " + os.Getenv("DISCORD_TOKEN"))

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		s.ChannelTyping(m.ChannelID)

		go func() {
			defer func() {
				if err := recover(); err != nil {
					s.ChannelMessageSend(m.ChannelID, "❌ ошибка")
				}
			}()

			ctx := getCtx(m.Author.ID)
			full := ctx + "\n" + m.Content

			res := askDeepSeek(full)

			updateCtx(m.Author.ID, m.Content)
			updateCtx(m.Author.ID, res)

			s.ChannelMessageSend(m.ChannelID, res)
		}()
	})

	dg.Open()
	log.Println("DEEPSEEK BOT RUNNING")

	select {}
}
