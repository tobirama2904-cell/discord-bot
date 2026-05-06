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
var groqKeys = strings.Split(os.Getenv("GROQ_KEYS"), ",")
var togetherKeys = strings.Split(os.Getenv("TOGETHER_KEYS"), ",")
var openaiKeys = strings.Split(os.Getenv("OPENAI_KEYS"), ",")

var gi, ti, oi int
var deadKeys = make(map[string]time.Time)
var mu sync.Mutex

func next(keys []string, idx *int) string {
	mu.Lock()
	defer mu.Unlock()

	for i := 0; i < len(keys); i++ {
		k := strings.TrimSpace(keys[*idx])
		*idx = (*idx + 1) % len(keys)

		if t, dead := deadKeys[k]; dead && time.Since(t) < 1*time.Minute {
			continue
		}

		return k
	}

	return ""
}

func markDead(k string) {
	mu.Lock()
	deadKeys[k] = time.Now()
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

func updateMemory(user, text string) {
	val, _ := memory.LoadOrStore(user, "")
	s := val.(string) + "\n" + text

	if len(s) > 1500 {
		s = s[len(s)-1500:] // 🔥 сжатие
	}

	memory.Store(user, s)
}

func getContext(user string) string {
	v, _ := memory.LoadOrStore(user, "")
	return v.(string)
}

// ===== HTTP SAFE =====
func doReq(req *http.Request) ([]byte, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)

	if res.StatusCode != 200 {
		return nil, err
	}

	return body, nil
}

func parse(body []byte) (string, error) {
	var r map[string]interface{}
	json.Unmarshal(body, &r)

	choices, ok := r["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", nil
	}

	return choices[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string), nil
}

// ===== PROVIDERS =====
func askGroq(p string) (string, error) {
	key := next(groqKeys, &gi)
	if key == "" {
		return "", nil
	}

	body := map[string]interface{}{
		"model": "llama3-8b-8192",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	b, err := doReq(req)
	if err != nil {
		markDead(key)
		return "", err
	}

	return parse(b)
}

func askTogether(p string) (string, error) {
	key := next(togetherKeys, &ti)
	if key == "" {
		return "", nil
	}

	body := map[string]interface{}{
		"model": "meta-llama/Llama-3-70b-chat-hf",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.together.xyz/v1/chat/completions", bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	b, err := doReq(req)
	if err != nil {
		markDead(key)
		return "", err
	}

	return parse(b)
}

func askOpenAI(p string) (string, error) {
	key := next(openaiKeys, &oi)
	if key == "" {
		return "", nil
	}

	body := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	b, err := doReq(req)
	if err != nil {
		markDead(key)
		return "", err
	}

	return parse(b)
}

// ===== AI CORE =====
func askAI(p string) string {

	if v, ok := getCache(p); ok {
		return "⚡ " + v
	}

	ctx := p
	providers := []func(string) (string, error){
		askGroq,
		askTogether,
		askOpenAI,
	}

	for i := 0; i < 2; i++ {
		for _, fn := range providers {

			res, err := fn(ctx)
			if err != nil {
				continue
			}

			if res != "" {
				setCache(p, res)
				return res
			}
		}
	}

	return "⚠️ AI временно недоступен"
}

// ===== STREAMING =====
func streamSend(s *discordgo.Session, ch string, text string) {

	msg, _ := s.ChannelMessageSend(ch, "⏳ думаем...")

	out := ""
	words := strings.Split(text, " ")

	for i, w := range words {
		out += w + " "

		if i%8 == 0 {
			s.ChannelMessageEdit(ch, msg.ID, out)
			time.Sleep(200 * time.Millisecond)
		}
	}

	s.ChannelMessageEdit(ch, msg.ID, out)
}

// ===== AGENT =====
func agent(userID, prompt string) string {

	ctx := getContext(userID)
	full := ctx + "\n" + prompt

	res := askAI(full)

	updateMemory(userID, prompt)
	updateMemory(userID, res)

	return res
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
					log.Println("panic:", err)
					s.ChannelMessageSend(m.ChannelID, "❌ ошибка")
				}
			}()

			// ⚡ ultra fast
			s.ChannelMessageSend(m.ChannelID, "⚡ думаю...")

			res := agent(m.Author.ID, m.Content)

			streamSend(s, m.ChannelID, res)

		}()
	})

	dg.Open()
	log.Println("ULTRA BOT RUNNING")

	select {}
}
