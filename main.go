package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

var client = &http.Client{Timeout: 20 * time.Second}

// ===== KEYS =====
var groqKeys = strings.Split(os.Getenv("GROQ_KEYS"), ",")
var togetherKeys = strings.Split(os.Getenv("TOGETHER_KEYS"), ",")
var openaiKeys = strings.Split(os.Getenv("OPENAI_KEYS"), ",")

var gi, ti, oi int
var mu sync.Mutex

func next(keys []string, i *int) string {
	mu.Lock()
	defer mu.Unlock()
	if len(keys) == 0 {
		return ""
	}
	k := keys[*i]
	*i = (*i + 1) % len(keys)
	return strings.TrimSpace(k)
}

// ===== AI =====
func askAI(prompt string) string {

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	providers := []func(context.Context, string) (string, error){
		askGroq, askTogether, askOpenAI,
	}

	type res struct {
		t string
		e error
	}

	ch := make(chan res, 3)

	for _, fn := range providers {
		go func(f func(context.Context, string) (string, error)) {
			r, e := f(ctx, prompt)
			ch <- res{r, e}
		}(fn)
	}

	for i := 0; i < len(providers); i++ {
		select {
		case r := <-ch:
			if r.e == nil && r.t != "" {
				return r.t
			}
		case <-ctx.Done():
			return ""
		}
	}

	return ""
}

// ===== PROVIDERS =====
func askGroq(ctx context.Context, p string) (string, error) {
	key := next(groqKeys, &gi)

	body := map[string]interface{}{
		"model": "llama3-8b-8192",
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

// ===== GITHUB DEPLOY =====
var token = os.Getenv("GITHUB_TOKEN")
var repo = os.Getenv("GITHUB_REPO")

func deploy(path, content string) {

	api := "https://api.github.com/repos/" + repo + "/contents/" + path

	body := map[string]string{
		"message": "deploy",
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequest("PUT", api, bytes.NewBuffer(j))
	req.Header.Set("Authorization", "Bearer "+token)

	http.DefaultClient.Do(req)
}

// ===== SERVER =====
func main() {

	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {

		body, _ := io.ReadAll(r.Body)

		var data map[string]string
		json.Unmarshal(body, &data)

		html := data["html"]

		id := fmt.Sprintf("site-%d", time.Now().Unix())

		deploy(id+"/index.html", html)

		w.Write([]byte("ok"))
	})

	go http.ListenAndServe(":8080", nil)

	dg, _ := discordgo.New("Bot " + os.Getenv("DISCORD_TOKEN"))

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		s.ChannelTyping(m.ChannelID)

		go func() {

			editorURL := "http://your-server:8080/editor.html"

			s.ChannelMessageSend(m.ChannelID,
				"🧩 Editor:\n"+editorURL)
		}()
	})

	dg.Open()

	select {}
}
