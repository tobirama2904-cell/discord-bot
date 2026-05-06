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

// ===== DATA =====
type User struct {
	Username string
	Password string
}

type Site struct {
	ID     string
	Owner  string
	HTML   string
	Prompt string
}

var users sync.Map     // username -> User
var sessions sync.Map  // token -> username
var sites sync.Map     // id -> Site

// ===== AI =====
var keys = strings.Split(os.Getenv("DEEPSEEK_KEYS"), ",")
var ki int

func nextKey() string {
	if len(keys) == 0 {
		return ""
	}
	k := strings.TrimSpace(keys[ki])
	ki = (ki + 1) % len(keys)
	return k
}

func askAI(prompt string) string {

	body := map[string]interface{}{
		"model": "deepseek-chat",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 400,
	}

	j, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST",
		"https://api.deepseek.com/v1/chat/completions",
		bytes.NewBuffer(j),
	)

	req.Header.Set("Authorization", "Bearer "+nextKey())
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "AI error"
	}
	defer res.Body.Close()

	b, _ := io.ReadAll(res.Body)

	var r map[string]interface{}
	json.Unmarshal(b, &r)

	return r["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string)
}

// ===== AUTH =====
func genToken() string {
	return time.Now().Format("20060102150405")
}

func register(w http.ResponseWriter, r *http.Request) {
	var d User
	json.NewDecoder(r.Body).Decode(&d)

	users.Store(d.Username, d)

	w.Write([]byte("ok"))
}

func login(w http.ResponseWriter, r *http.Request) {
	var d User
	json.NewDecoder(r.Body).Decode(&d)

	u, ok := users.Load(d.Username)
	if !ok || u.(User).Password != d.Password {
		w.Write([]byte("error"))
		return
	}

	token := genToken()
	sessions.Store(token, d.Username)

	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func auth(r *http.Request) string {
	token := r.Header.Get("Authorization")
	v, ok := sessions.Load(token)
	if !ok {
		return ""
	}
	return v.(string)
}

// ===== SITES =====
func createSite(w http.ResponseWriter, r *http.Request) {

	user := auth(r)
	if user == "" {
		w.Write([]byte("auth error"))
		return
	}

	var d struct {
		Prompt string
	}
	json.NewDecoder(r.Body).Decode(&d)

	html := askAI("сделай современный сайт:\n" + d.Prompt)

	id := time.Now().Format("150405")

	sites.Store(id, Site{
		ID:     id,
		Owner:  user,
		HTML:   html,
		Prompt: d.Prompt,
	})

	json.NewEncoder(w).Encode(map[string]string{
		"id":  id,
		"url": "/site/" + id,
	})
}

func listSites(w http.ResponseWriter, r *http.Request) {

	user := auth(r)

	var res []Site

	sites.Range(func(_, v interface{}) bool {
		s := v.(Site)
		if s.Owner == user {
			res = append(res, s)
		}
		return true
	})

	json.NewEncoder(w).Encode(res)
}

func getSite(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/site/")
	v, ok := sites.Load(id)
	if !ok {
		w.Write([]byte("404"))
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(v.(Site).HTML))
}

// ===== DISCORD =====
func runBot() {

	dg, _ := discordgo.New("Bot " + os.Getenv("DISCORD_TOKEN"))

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {

		if m.Author.Bot {
			return
		}

		s.ChannelTyping(m.ChannelID)

		go func() {
			res := askAI(m.Content)
			s.ChannelMessageSend(m.ChannelID, res)
		}()
	})

	dg.Open()
}

// ===== UI =====
func ui(w http.ResponseWriter, r *http.Request) {

	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
<style>
body{margin:0;background:#0f0f0f;color:white;font-family:sans-serif;display:flex;height:100vh}
#left{width:300px;background:#111;padding:10px}
#right{flex:1;background:white}
input,textarea{width:100%;margin-top:5px;padding:8px}
button{margin-top:5px;padding:8px}
.site{background:#1f2937;margin-top:5px;padding:5px;cursor:pointer}
</style>
</head>

<body>

<div id="left">

<h3>Auth</h3>
<input id="user" placeholder="username">
<input id="pass" placeholder="password">

<button onclick="reg()">Register</button>
<button onclick="log()">Login</button>

<h3>Создать сайт</h3>
<textarea id="prompt"></textarea>
<button onclick="create()">🚀 Создать</button>

<h3>Мои сайты</h3>
<div id="sites"></div>

</div>

<iframe id="right"></iframe>

<script>
let token=""

async function reg(){
 await fetch("/register",{method:"POST",body:JSON.stringify({
  Username:user.value,Password:pass.value
 })})
 alert("ok")
}

async function log(){
 let r=await fetch("/login",{method:"POST",body:JSON.stringify({
  Username:user.value,Password:pass.value
 })})
 let d=await r.json()
 token=d.token
 loadSites()
}

async function create(){
 let r=await fetch("/create-site",{
  method:"POST",
  headers:{"Authorization":token},
  body:JSON.stringify({Prompt:prompt.value})
 })
 loadSites()
}

async function loadSites(){
 let r=await fetch("/sites",{headers:{"Authorization":token}})
 let d=await r.json()

 sites.innerHTML=""
 d.forEach(s=>{
  sites.innerHTML+=\`<div class="site" onclick="openSite('${s.ID}')">\${s.Prompt}</div>\`
 })
}

function openSite(id){
 right.src="/site/"+id
}
</script>

</body>
</html>
`))
}

// ===== MAIN =====
func main() {

	go runBot()

	http.HandleFunc("/", ui)
	http.HandleFunc("/register", register)
	http.HandleFunc("/login", login)
	http.HandleFunc("/create-site", createSite)
	http.HandleFunc("/sites", listSites)
	http.HandleFunc("/site/", getSite)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("RUNNING", port)
	http.ListenAndServe(":"+port, nil)
}
