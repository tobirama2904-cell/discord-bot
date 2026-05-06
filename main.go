package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
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

var client = &http.Client{Timeout: 10 * time.Second}

// ===== GROQ KEYS =====
var keys = strings.Split(os.Getenv("GROQ_KEYS"), ",")
var ki int
var mu sync.Mutex

func nextKey() string {
	mu.Lock()
	defer mu.Unlock()

	if len(keys) == 0 {
		return ""
	}
	k := strings.TrimSpace(keys[ki])
	ki = (ki + 1) % len(keys)
	return k
}

// ===== CACHE =====
var cache sync.Map

func hash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
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

	if len(s) > 800 {
		s = s[len(s)-800:]
	}

	memory.Store(user, s)
}

// ===== SMART TOKENS =====
func smartTokens(prompt string) int {
	l := len(prompt)

	if l < 50 {
		return 80
	}
	if l < 200 {
		return 150
	}
	return 250
}

// ===== GROQ AI =====
func askAI(prompt string) string {

	prompt = strings.TrimSpace(prompt)
	keyHash := hash(prompt)

	// cache
	if v, ok := cache.Load(keyHash); ok {
		return "⚡ " + v.(string)
	}

	key := nextKey()
	if key == "" {
		return "❌ GROQ_KEYS не задан"
	}

	// обрезка
	if len(prompt) > 700 {
		prompt = prompt[len(prompt)-700:]
	}

	body := map[string]interface{}{
		"model": "llama3-70b-8192",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": smartTokens(prompt),
	}

	j, _ := json.Marshal(body)

	req, err := http.NewRequest("POST",
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewBuffer(j),
	)

	if err != nil {
		return "❌ request error"
	}

	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "❌ сеть или API недоступен"
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "❌ GROQ: " + string(data)
	}

	var r map[string]interface{}
	if err := json.Unmarshal(data, &r); err != nil {
		return "❌ JSON error"
	}

	choices, ok := r["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "❌ пустой ответ"
	}

	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	out := msg["content"].(string)

	cache.Store(keyHash, out)

	return out
}

// ===== SITES =====
var sites sync.Map

func createSite(w http.ResponseWriter, r *http.Request) {

	var d struct {
		Prompt string
	}
	json.NewDecoder(r.Body).Decode(&d)

	html := askAI("создай красивый HTML сайт:\n" + d.Prompt)

	id := hash(time.Now().String())[:8]

	sites.Store(id, html)

	json.NewEncoder(w).Encode(map[string]string{
		"id":  id,
		"url": "/site/" + id,
	})
}

func getSite(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/site/")

	v, ok := sites.Load(id)
	if !ok {
		w.Write([]byte("404"))
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(v.(string)))
}

// ===== API =====
func api(w http.ResponseWriter, r *http.Request) {

	var d struct {
		User   string
		Prompt string
	}

	json.NewDecoder(r.Body).Decode(&d)

	ctx := getCtx(d.User)
	full := ctx + "\n" + d.Prompt

	res := askAI(full)

	updateCtx(d.User, d.Prompt)
	updateCtx(d.User, res)

	json.NewEncoder(w).Encode(map[string]string{
		"response": res,
	})
}

// ===== DISCORD =====
func runBot() {

	dg, err := discordgo.New("Bot " + os.Getenv("DISCORD_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		s.ChannelTyping(m.ChannelID)

		go func() {
			defer func() {
				if err := recover(); err != nil {
					s.ChannelMessageSend(m.ChannelID, "❌ error")
				}
			}()

			res := askAI(m.Content)
			s.ChannelMessageSend(m.ChannelID, res)
		}()
	})

	err = dg.Open()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Discord bot started")
}

// ===== UI =====
func ui(w http.ResponseWriter, r *http.Request) {

	w.Write([]byte(`
<!DOCTYPE html>
<html>
<body style="background:#0f0f0f;color:white;font-family:sans-serif">

<h2>AI Builder (Groq)</h2>

<div id="chat"></div>

<input id="inp">
<button onclick="send()">Send</button>

<hr>

<textarea id="prompt"></textarea>
<button onclick="gen()">Generate Site</button>

<iframe id="view" style="width:100%;height:400px"></iframe>

<script>
async function send(){
 let v=inp.value
 chat.innerHTML+="<div>"+v+"</div>"

 let r=await fetch("/api",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({user:"web",prompt:v})})
 let d=await r.json()

 chat.innerHTML+="<div>"+d.response+"</div>"
}

async function gen(){
 let p=prompt.value

 let r=await fetch("/create-site",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({prompt:p})})
 let d=await r.json()

 view.src="/site/"+d.id
}
</script>

</body>
</html>
`))
}

// ===== MAIN =====
func main() {

	log.Println("GROQ_KEYS:", os.Getenv("GROQ_KEYS"))

	go runBot()

	http.HandleFunc("/", ui)
	http.HandleFunc("/api", api)
	http.HandleFunc("/create-site", createSite)
	http.HandleFunc("/site/", getSite)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("SERVER RUNNING:", port)
	http.ListenAndServe(":"+port, nil)
}
