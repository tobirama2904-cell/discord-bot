package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)


// ================= MEMORY =================

type Memory struct {
	Messages []string
}

var memory sync.Map

func addMemory(user, msg string) {
	v, _ := memory.LoadOrStore(user, &Memory{})
	mem := v.(*Memory)

	mem.Messages = append(mem.Messages, msg)

	if len(mem.Messages) > 12 {
		mem.Messages = mem.Messages[len(mem.Messages)-12:]
	}
}

func getContext(user string) string {
	v, ok := memory.Load(user)
	if !ok {
		return ""
	}
	return strings.Join(v.(*Memory).Messages, " ")
}


// ================= KNOWLEDGE BASE =================

type KBItem struct {
	Key  string
	Text string
}

var kb []KBItem

func loadKB() {
	file, err := os.Open("knowledge.txt")
	if err != nil {
		log.Println("⚠ knowledge.txt не найден — работаем без базы")
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 {
			kb = append(kb, KBItem{
				Key:  strings.ToLower(parts[0]),
				Text: parts[1],
			})
		}
	}

	log.Println("📚 Загружено знаний:", len(kb))
}


// ================= SEARCH =================

func searchKB(text string) string {

	text = strings.ToLower(text)

	bestScore := 0
	bestText := ""

	for _, item := range kb {
		score := 0

		for _, w := range strings.Fields(item.Key) {
			if strings.Contains(text, w) {
				score++
			}
		}

		if score > bestScore {
			bestScore = score
			bestText = item.Text
		}
	}

	if bestScore > 0 {
		return bestText
	}

	return ""
}


// ================= PSEUDO AI =================

func pseudoAI(input string) string {

	words := strings.Fields(input)

	if len(words) < 3 {
		return "🤖 Уточни вопрос — нужно больше деталей."
	}

	return "🤖 Анализ:\n" + input +
		"\n\n👉 Совет: начни с простого решения и постепенно улучшай."
}


// ================= INTELLIGENCE =================

func smartReply(user, prompt string) string {

	ctx := getContext(user)
	full := ctx + " " + prompt

	addMemory(user, prompt)

	p := strings.ToLower(prompt)

	// 1. база знаний
	if res := searchKB(full); res != "" {
		return res
	}

	// 2. правила
	if strings.Contains(p, "привет") {
		return "Привет 👋 Чем помочь?"
	}

	if strings.Contains(p, "как") {
		return "📌 План:\n1) цель\n2) шаги\n3) действие"
	}

	if strings.Contains(p, "деньги") {
		return "💰 Деньги = ценность + спрос."
	}

	// 3. fallback интеллект
	return pseudoAI(full)
}


// ================= API =================

func apiHandler(w http.ResponseWriter, r *http.Request) {

	var d struct {
		User   string
		Prompt string
	}

	err := json.NewDecoder(r.Body).Decode(&d)
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	res := smartReply(d.User, d.Prompt)

	json.NewEncoder(w).Encode(map[string]string{
		"response": res,
	})
}


// ================= DISCORD =================

func runBot() {

	token := os.Getenv("DISCORD_TOKEN")

	if token == "" {
		log.Println("❌ DISCORD_TOKEN не задан")
		return
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Println("❌ ошибка создания бота:", err)
		return
	}

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		res := smartReply(m.Author.ID, m.Content)

		_, err := s.ChannelMessageSend(m.ChannelID, res)
		if err != nil {
			log.Println("❌ ошибка отправки:", err)
		}
	})

	err = dg.Open()
	if err != nil {
		log.Println("❌ ошибка запуска:", err)
		return
	}

	log.Println("✅ BOT ONLINE")
}


// ================= UI =================

func uiHandler(w http.ResponseWriter, r *http.Request) {

	w.Write([]byte(`
<!DOCTYPE html>
<html>
<body style="background:#0f0f0f;color:white;font-family:sans-serif">

<h2>🤖 Offline AI</h2>

<input id="inp" style="width:300px">
<button onclick="send()">Send</button>

<div id="chat"></div>

<script>
async function send(){
 let v=inp.value

 let r=await fetch("/api",{
   method:"POST",
   headers:{"Content-Type":"application/json"},
   body:JSON.stringify({user:"web",prompt:v})
 })

 let d=await r.json()

 chat.innerHTML += "<div><b>Ты:</b> "+v+"</div>"
 chat.innerHTML += "<div><b>Бот:</b> "+d.response+"</div><hr>"
}
</script>

</body>
</html>
`))
}


// ================= MAIN =================

func main() {

	loadKB()
	go runBot()

	http.HandleFunc("/", uiHandler)
	http.HandleFunc("/api", apiHandler)

	log.Println("🚀 SERVER RUNNING :8080")

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
