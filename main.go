package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ===== HTTP CLIENT =====
var client = &http.Client{
	Timeout: 25 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	},
}

// ===== KEYS =====
var groqKeys = strings.Split(os.Getenv("GROQ_KEYS"), ",")
var togetherKeys = strings.Split(os.Getenv("TOGETHER_KEYS"), ",")
var openaiKeys = strings.Split(os.Getenv("OPENAI_KEYS"), ",")

var gi, ti, oi int
var keyMu sync.Mutex

func next(keys []string, i *int) string {
	keyMu.Lock()
	defer keyMu.Unlock()

	if len(keys) == 0 {
		return ""
	}
	k := keys[*i]
	*i = (*i + 1) % len(keys)
	return k
}

// ===== CACHE =====
var cache = make(map[string]string)
var cacheMu sync.Mutex

func getCache(k string) (string, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	v, ok := cache[k]
	return v, ok
}

func setCache(k, v string) {
	cacheMu.Lock()
	cache[k] = v
	cacheMu.Unlock()
}

// ===== MEMORY =====
var memory = make(map[string]string)
var memMu sync.Mutex

func updateMemory(user, text string) {
	memMu.Lock()
	defer memMu.Unlock()

	memory[user] += "\n" + text
	if len(memory[user]) > 2000 {
		memory[user] = memory[user][len(memory[user])-2000:]
	}
}

func getContext(user string) string {
	memMu.Lock()
	defer memMu.Unlock()
	return memory[user]
}

// ===== SAFE PARSE =====
func safeParse(r map[string]interface{}) (string, error) {
	choices, ok := r["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("no choices")
	}

	msg, ok := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no message")
	}

	content, ok := msg["content"].(string)
	if !ok {
		return "", fmt.Errorf("no content")
	}

	return content, nil
}

// ===== PROVIDERS =====
func askGroq(ctx context.Context, p string) (string, error) {
	key := next(groqKeys, &gi)
	if key == "" {
		return "", fmt.Errorf("no groq key")
	}

	body := map[string]interface{}{
		"model": "llama3-70b-8192",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}
	j, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("groq error: %s", string(body))
	}

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)

	return safeParse(r)
}

func askTogether(ctx context.Context, p string) (string, error) {
	key := next(togetherKeys, &ti)
	if key == "" {
		return "", fmt.Errorf("no together key")
	}

	body := map[string]interface{}{
		"model": "meta-llama/Llama-3-70b-chat-hf",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}
	j, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.together.xyz/v1/chat/completions", bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("together error: %s", string(body))
	}

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)

	return safeParse(r)
}

func askOpenAI(ctx context.Context, p string) (string, error) {
	key := next(openaiKeys, &oi)
	if key == "" {
		return "", fmt.Errorf("no openai key")
	}

	body := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}
	j, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("openai error: %s", string(body))
	}

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)

	return safeParse(r)
}

// ===== ULTRA AI =====
func askAI(p string) string {

	if v, ok := getCache(p); ok {
		return "⚡ " + v
	}

	if len(p) > 2000 {
		p = p[len(p)-2000:]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	type result struct {
		text string
		err  error
	}

	ch := make(chan result, 3)

	providers := []func(context.Context, string) (string, error){
		askGroq,
		askTogether,
		askOpenAI,
	}

	for _, fn := range providers {
		go func(f func(context.Context, string) (string, error)) {
			res, err := f(ctx, p)
			ch <- result{res, err}
		}(fn)
	}

	for i := 0; i < len(providers); i++ {
		select {
		case r := <-ch:
			if r.err == nil && r.text != "" {
				setCache(p, r.text)
				cancel()
				return r.text
			}
		case <-ctx.Done():
			return "❌ timeout"
		}
	}

	return "❌ AI error"
}

// ===== TOOLS =====
func browse(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return "❌ ошибка загрузки"
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if len(text) > 3000 {
		text = text[:3000]
	}

	return askAI("проанализируй:\n" + text)
}

func runPython(code string) string {
	body := map[string]interface{}{
		"language": "python",
		"source":   code,
	}
	j, _ := json.Marshal(body)

	resp, err := http.Post("https://emkc.org/api/v2/piston/execute", "application/json", bytes.NewBuffer(j))
	if err != nil {
		return "❌ ошибка"
	}
	defer resp.Body.Close()

	out, _ := io.ReadAll(resp.Body)
	return string(out)
}

func deploy(html string) string {
	token := os.Getenv("GITHUB_TOKEN")
	repo := os.Getenv("GITHUB_REPO")

	api := "https://api.github.com/repos/" + repo + "/contents/index.html"

	content := map[string]interface{}{
		"message": "update",
		"content": base64.StdEncoding.EncodeToString([]byte(html)),
	}
	j, _ := json.Marshal(content)

	req, _ := http.NewRequest("PUT", api, bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+token)

	client.Do(req)

	user := strings.Split(repo, "/")[0]
	return "https://" + user + ".github.io/"
}

// ===== AGENT =====
func agent(userID, prompt string) string {

	ctx := getContext(userID)
	full := ctx + "\n" + prompt

	var res string

	switch {
	case strings.Contains(prompt, "браузер"):
		res = browse(strings.Replace(prompt, "браузер", "", 1))

	case strings.Contains(prompt, "код"):
		code := askAI("напиши python:\n" + full)
		res = runPython(code)

	case strings.Contains(prompt, "сайт"):
		html := askAI("html сайт:\n" + full)
		res = deploy(html)

	default:
		res = askAI(full)
	}

	updateMemory(userID, prompt)
	updateMemory(userID, res)

	return res
}

// ===== QUEUE =====
var queue = make(chan func(), 100)

func worker() {
	for job := range queue {
		job()
	}
}

// ===== MAIN =====
func main() {

	for i := 0; i < 5; i++ {
		go worker()
	}

	dg, _ := discordgo.New("Bot " + os.Getenv("DISCORD_TOKEN"))

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		s.ChannelTyping(m.ChannelID)

		queue <- func() {
			defer func() {
				if r := recover(); r != nil {
					log.Println("PANIC:", r)
					s.ChannelMessageSend(m.ChannelID, "❌ ошибка")
				}
			}()

			res := agent(m.Author.ID, m.Content)

			if res == "" {
				res = "❌ пустой ответ"
			}

			s.ChannelMessageSend(m.ChannelID, res)
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	go http.ListenAndServe(":"+port, nil)

	dg.Open()
	log.Println("ULTRA BOT RUNNING")

	select {}
}
