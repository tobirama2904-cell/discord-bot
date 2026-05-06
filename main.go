package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ===== SETTINGS =====
var aiEnabled = false // 🔥 можно включить позже

// ===== MEMORY =====
type Memory struct {
	Messages []string
}

var userMemory sync.Map

func addMemory(user, msg string) {
	v, _ := userMemory.LoadOrStore(user, &Memory{})
	mem := v.(*Memory)

	mem.Messages = append(mem.Messages, msg)

	if len(mem.Messages) > 20 {
		mem.Messages = mem.Messages[len(mem.Messages)-20:]
	}
}

func getMemory(user string) string {
	v, ok := userMemory.Load(user)
	if !ok {
		return ""
	}
	mem := v.(*Memory)
	return strings.Join(mem.Messages, "\n")
}

// ===== ANALYTICS =====
var stats sync.Map

func track(prompt string) {
	p := strings.ToLower(prompt)
	v, _ := stats.LoadOrStore(p, 0)
	stats.Store(p, v.(int)+1)
}

// ===== SMART RESPONSES =====
func smartFallback(p string) string {

	if strings.Contains(p, "деньги") {
		return "💰 Начни с простого продукта и протестируй спрос."
	}

	if strings.Contains(p, "сайт") {
		return "🌐 Создай лендинг с понятным оффером и кнопкой действия."
	}

	if strings.Contains(p, "бизнес") {
		return "🚀 Найди проблему и реши её быстрее конкурентов."
	}

	return "🤖 Попробуй разбить задачу на простые шаги и начать с MVP."
}

// ===== AI ROUTER =====
func askAI(user, prompt string) string {

	p := strings.ToLower(strings.TrimSpace(prompt))

	track(p)
	addMemory(user, p)

	// ===== RULES =====
	if strings.Contains(p, "привет") {
		return "Привет 👋 Чем помочь?"
	}

	if len(p) < 20 {
		return "🤖 " + p + " — можно сделать быстро."
	}

	// ===== NO AI MODE =====
	if !aiEnabled {
		return smartFallback(p)
	}

	// сюда можно потом вернуть AI
	return smartFallback(p)
}

// ===== SITE GENERATOR (NO AI) =====
func generateSite(prompt string) string {

	title := prompt

	return `
<html>
<head>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
body{margin:0;font-family:sans-serif;background:#0f0f0f;color:white}
.hero{padding:100px;text-align:center}
.btn{padding:12px 24px;background:#4f46e5;color:white;border:none;border-radius:8px}
.section{padding:50px;background:#111}
.card{background:#1f2937;padding:20px;margin:10px;border-radius:10px}
</style>
</head>

<body>

<div class="hero">
<h1>🚀 ` + title + `</h1>
<p>Современный продукт</p>
<button class="btn">Начать</button>
</div>

<div class="section">
<h2>Функции</h2>
<div class="card">⚡ Быстро</div>
<div class="card">💰 Эффективно</div>
<div class="card">🚀 Масштабируемо</div>
</div>

<div class="section">
<h2>Почему мы</h2>
<p>Минимум затрат — максимум результата</p>
</div>

</body>
</html>`
}

// ===== SITES =====
var sites sync.Map

func createSite(w http.ResponseWriter, r *http.Request) {

	var d struct {
		Prompt string
	}
	json.NewDecoder(r.Body).Decode(&d)

	html := generateSite(d.Prompt)

	id := strings.ReplaceAll(time.Now().String(), " ", "")[:10]
	sites.Store(id, html)

	json.NewEncoder(w).Encode(map[string]string{
		"id": id,
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

	res := askAI(d.User, d.Prompt)

	json.NewEncoder(w).Encode(map[string]string{
		"response": res,
	})
}

// ===== ANALYTICS API =====
func analytics(w http.ResponseWriter, r *http.Request) {

	out := map[string]int{}

	stats.Range(func(k, v interface{}) bool {
		out[k.(string)] = v.(int)
		return true
	})

	json.NewEncoder(w).Encode(out)
}

// ===== DISCORD =====
func runBot() {

	dg, _ := discordgo.New("Bot " + getenv("DISCORD_TOKEN"))

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		res := askAI(m.Author.ID, m.Content)
		s.ChannelMessageSend(m.ChannelID, res)
	})

	dg.Open()
}

// ===== UI =====
func ui(w http.ResponseWriter, r *http.Request) {

	w.Write([]byte(`
<!DOCTYPE html>
<html>
<body style="background:#0f0f0f;color:white;font-family:sans-serif">

<h2>🚀 AI SaaS Builder</h2>

<input id="inp">
<button onclick="send()">Send</button>

<hr>

<textarea id="prompt"></textarea>
<button onclick="gen()">Generate Site</button>

<iframe id="view" style="width:100%;height:400px"></iframe>

<script>
async function send(){
 let v=inp.value
 let r=await fetch("/api",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({user:"web",prompt:v})})
 let d=await r.json()
 alert(d.response)
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

// ===== HELP =====
func getenv(k string) string {
	v := strings.TrimSpace(strings.Trim(os.Getenv(k), " "))
	return v
}

// ===== MAIN =====
func main() {

	go runBot()

	http.HandleFunc("/", ui)
	http.HandleFunc("/api", api)
	http.HandleFunc("/create-site", createSite)
	http.HandleFunc("/site/", getSite)
	http.HandleFunc("/analytics", analytics)

	log.Println("🚀 SaaS RUNNING :8080")
	http.ListenAndServe(":8080", nil)
}
