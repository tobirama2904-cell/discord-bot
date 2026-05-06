package main

import (
	"bytes"
	"context"
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

var client = &http.Client{Timeout: 25 * time.Second}

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

// ===== AI CORE =====
func askAI(prompt string) string {

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	type res struct {
		text string
		err  error
	}

	ch := make(chan res, 3)

	providers := []func(context.Context, string) (string, error){
		askGroq, askTogether, askOpenAI,
	}

	for _, fn := range providers {
		go func(f func(context.Context, string) (string, error)) {
			r, e := f(ctx, prompt)
			ch <- res{r, e}
		}(fn)
	}

	for i := 0; i < len(providers); i++ {
		select {
		case r := <-ch:
			if r.err == nil && r.text != "" {
				return r.text
			}
		case <-ctx.Done():
			return "❌ timeout"
		}
	}

	return "❌ AI error"
}

// ===== PROVIDERS =====
func askGroq(ctx context.Context, p string) (string, error) {
	key := next(groqKeys, &gi)

	body := map[string]interface{}{
		"model": "llama3-70b-8192",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST",
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewBuffer(j))

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

func askTogether(ctx context.Context, p string) (string, error) {
	key := next(togetherKeys, &ti)

	body := map[string]interface{}{
		"model": "meta-llama/Llama-3-70b-chat-hf",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST",
		"https://api.together.xyz/v1/chat/completions",
		bytes.NewBuffer(j))

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

func askOpenAI(ctx context.Context, p string) (string, error) {
	key := next(openaiKeys, &oi)

	body := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "user", "content": p},
		},
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST",
		"https://api.openai.com/v1/chat/completions",
		bytes.NewBuffer(j))

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

// ===== STORAGE =====
type Site struct {
	ID   string
	Type string
	HTML string
	Time time.Time
}

var sites = make(map[string]Site)
var mu sync.Mutex

// ===== GENERATION =====
func generate(prompt string) Site {

	mode := "site"
	if strings.Contains(prompt, "app") {
		mode = "app"
	}

	html := askAI(`
создай ultra современный продукт как Framer:
- красивый UI
- tailwind
- glassmorphism
- анимации
- интерактивность
- кнопки
- адаптивность

тип: ` + mode + `
тема: ` + prompt)

	id := fmt.Sprintf("%d", time.Now().UnixNano())

	return Site{
		ID:   id,
		Type: mode,
		HTML: html,
		Time: time.Now(),
	}
}

// ===== PANEL UI =====
func panel(domain string) string {

	mu.Lock()
	defer mu.Unlock()

	html := `
<style>
body{background:#0f172a;color:white;font-family:sans-serif;padding:20px}
.card{background:#1e293b;padding:15px;margin:10px;border-radius:15px}
a{color:#3b82f6}
</style>
<h1>🚀 AI Platform</h1>
`

	for _, s := range sites {
		html += fmt.Sprintf(`
<div class="card">
<b>%s</b><br>
тип: %s<br>
<a href="%s/site/%s">Открыть</a>
</div>
`, s.ID, s.Type, domain, s.ID)
	}

	return html
}

// ===== WEB =====
func startWeb(domain string) {

	http.HandleFunc("/site/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/site/")
		mu.Lock()
		s, ok := sites[id]
		mu.Unlock()

		if ok {
			w.Write([]byte(s.HTML))
			return
		}
		http.NotFound(w, r)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(panel(domain)))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	go http.ListenAndServe(":"+port, nil)
}

// ===== MAIN =====
func main() {

	domain := os.Getenv("DOMAIN")
	if domain == "" {
		domain = "http://localhost:8080"
	}

	startWeb(domain)

	dg, _ := discordgo.New("Bot " + os.Getenv("DISCORD_TOKEN"))

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		s.ChannelTyping(m.ChannelID)

		go func() {

			site := generate(m.Content)

			mu.Lock()
			sites[site.ID] = site
			mu.Unlock()

			link := domain + "/site/" + site.ID

			s.ChannelMessageSend(m.ChannelID,
				"🚀 Готово:\n"+link+"\n\n📊 Панель:\n"+domain)
		}()
	})

	dg.Open()
	log.Println("ULTRA AI PLATFORM RUNNING")

	select {}
}
