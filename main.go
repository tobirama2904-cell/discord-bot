package main

import (
	"bytes"
	"encoding/base64"
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

var client = &http.Client{Timeout: 15 * time.Second}

// ===== KEYS =====
var groqKeys = strings.Split(os.Getenv("GROQ_KEYS"), ",")
var togetherKeys = strings.Split(os.Getenv("TOGETHER_KEYS"), ",")
var openaiKeys = strings.Split(os.Getenv("OPENAI_KEYS"), ",")

var gi, ti, oi int

func next(keys []string, i *int) string {
	if len(keys) == 0 {
		return ""
	}
	k := keys[*i]
	*i = (*i + 1) % len(keys)
	return k
}

// ===== CACHE =====
var cache = make(map[string]string)
var mu sync.Mutex

func getCache(k string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	v, ok := cache[k]
	return v, ok
}

func setCache(k, v string) {
	mu.Lock()
	cache[k] = v
	mu.Unlock()
}

// ===== MEMORY =====
var memory = make(map[string]string)

func updateMemory(user, text string) {
	memory[user] += "\n" + text
	if len(memory[user]) > 2000 {
		memory[user] = askFast("сожми:\n" + memory[user])
	}
}

func getContext(user string) string {
	return memory[user]
}

// ===== AI =====
func askFast(p string) string {
	res, _ := askGroq(p)
	return res
}

func askAI(p string) string {

	if v, ok := getCache(p); ok {
		return "⚡ " + v
	}

	if len(p) > 2000 {
		p = p[len(p)-2000:]
	}

	providers := []func(string) (string, error){
		askGroq,
		askTogether,
		askOpenAI,
	}

	for _, fn := range providers {
		res, err := fn(p)
		if err == nil && res != "" {
			setCache(p, res)
			return res
		}
	}

	return "❌ AI error"
}

// ===== PROVIDERS =====
func askGroq(p string) (string, error) {
	key := next(groqKeys, &gi)

	body := map[string]interface{}{
		"model": "llama3-70b-8192",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)

	return r["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string), nil
}

func askTogether(p string) (string, error) {
	key := next(togetherKeys, &ti)

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

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)

	return r["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string), nil
}

func askOpenAI(p string) (string, error) {
	key := next(openaiKeys, &oi)

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

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)

	return r["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string), nil
}

// ===== BROWSER =====
func browse(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return "❌ ошибка"
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if len(text) > 3000 {
		text = text[:3000]
	}

	return askAI("проанализируй:\n" + text)
}

// ===== CODE =====
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

// ===== DEPLOY =====
func deploy(html string) string {

	token := os.Getenv("GITHUB_TOKEN")
	repo := os.Getenv("GITHUB_REPO")

	id := time.Now().Format("20060102150405")
	path := "site-" + id + "/index.html"

	api := "https://api.github.com/repos/" + repo + "/contents/" + path

	content := map[string]interface{}{
		"message": "deploy site",
		"content": base64.StdEncoding.EncodeToString([]byte(html)),
	}

	j, _ := json.Marshal(content)

	req, _ := http.NewRequest("PUT", api, bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+token)

	client.Do(req)

	user := strings.Split(repo, "/")[0]

	return "https://" + user + ".github.io/" + strings.Split(repo, "/")[1] + "/" + "site-" + id + "/"
}

// ===== AGENT =====
func agent(userID, prompt string) string {

	ctx := getContext(userID)
	full := ctx + "\n" + prompt

	var res string

	switch {
	case strings.Contains(prompt, "сайт"):
		html := askAI("сделай красивый современный сайт (как saas):\n" + full)
		link := deploy(html)
		res = "🚀 сайт готов:\n" + link

	case strings.Contains(prompt, "код"):
		code := askAI("напиши python код:\n" + full)
		res = runPython(code)

	case strings.Contains(prompt, "браузер"):
		res = browse(strings.Replace(prompt, "браузер", "", 1))

	default:
		res = askAI(full)
	}

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
			res := agent(m.Author.ID, m.Content)

			editor := "https://" + strings.Split(os.Getenv("GITHUB_REPO"), "/")[0] +
				".github.io/" + strings.Split(os.Getenv("GITHUB_REPO"), "/")[1] + "/editor.html"

			s.ChannelMessageSend(m.ChannelID,
				res+"\n\n🧩 Editor:\n"+editor)
		}()
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	go http.ListenAndServe(":"+port, nil)

	dg.Open()
	log.Println("BOT RUNNING")

	select {}
}
