package panel

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/proto"
)

// fakeAgent speaks the agent side of the protocol and records what the panel asks of it.
type fakeAgent struct {
	t    *testing.T
	ws   *websocket.Conn
	mu   sync.Mutex
	got  []proto.Envelope
	seen chan proto.Envelope
}

func connectAgent(t *testing.T, ts *httptest.Server, token string) *fakeAgent {
	t.Helper()
	ctx := context.Background()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/agent/v1/connect",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		t.Fatalf("agent dial: %v", err)
	}
	a := &fakeAgent{t: t, ws: ws, seen: make(chan proto.Envelope, 100)}
	t.Cleanup(func() { ws.CloseNow() })
	hello, _ := proto.Encode("", proto.TypeHello, proto.Hello{Protocol: proto.Version, AgentVersion: "test", Hostname: "fake",
		CgroupV2: true, CPUs: 4, MemoryBytes: 8 << 30, DiskBytes: 100 << 30})
	if err := ws.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			_, raw, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var env proto.Envelope
			_ = json.Unmarshal(raw, &env)
			a.mu.Lock()
			a.got = append(a.got, env)
			a.mu.Unlock()
			reply := []byte(nil)
			if env.Type == proto.TypeLogsSubscribe {
				reply, _ = proto.Encode(env.ID, proto.TypeOK, proto.LogBatch{Lines: []proto.LogLine{{T: 1, Stream: "stdout", Text: "earlier line"}}})
			} else {
				reply, _ = proto.Encode(env.ID, proto.TypeOK, nil)
			}
			_ = ws.Write(ctx, websocket.MessageText, reply)
			a.seen <- env
		}
	}()
	return a
}

// next waits for the next request of the given type.
func (a *fakeAgent) next(typ string) proto.Envelope {
	a.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case env := <-a.seen:
			if env.Type == typ {
				return env
			}
		case <-deadline:
			a.t.Fatalf("agent never received %s", typ)
		}
	}
}

func (a *fakeAgent) event(typ string, v any) {
	a.t.Helper()
	b, _ := proto.Encode("", typ, v)
	if err := a.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		a.t.Fatal(err)
	}
}

func specOf(t *testing.T, env proto.Envelope) proto.BotSpec {
	t.Helper()
	var s proto.BotSpec
	if err := json.Unmarshal(env.Data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func signIn(t *testing.T, ts *httptest.Server, email, password string) *http.Client {
	t.Helper()
	c := newClient(t)
	if code, b := do(t, c, ts, "POST", "/api/v1/auth/login", `{"email":"`+email+`","password":"`+password+`"}`); code != 200 {
		t.Fatalf("sign in %s: %d %v", email, code, b)
	}
	return c
}

func mustDo(t *testing.T, c *http.Client, ts *httptest.Server, want int, method, path, body string) map[string]any {
	t.Helper()
	code, b := do(t, c, ts, method, path, body)
	if code != want {
		t.Fatalf("%s %s: got %d want %d: %v", method, path, code, want, b)
	}
	return b
}

func doList(t *testing.T, c *http.Client, ts *httptest.Server, path string) []map[string]any {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("GET %s: %d %v", path, res.StatusCode, err)
	}
	return out
}

func upload(t *testing.T, c *http.Client, ts *httptest.Server, path string, file []byte, headers ...string) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "bot.zip")
	fw.Write(file)
	mw.Close()
	req, _ := http.NewRequest("POST", ts.URL+path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", ts.URL)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func zipOf(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func tarNames(t *testing.T, gz []byte) []string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	sort.Strings(names)
	return names
}

// TestHostingFlow walks the whole v0 path: plan → user → node → bot → deploy → live.
func TestHostingFlow(t *testing.T) {
	ts, q := testServer(t)
	createUser(t, q, "admin@example.com", "a-long-password", db.UserRoleAdmin)
	admin := signIn(t, ts, "admin@example.com", "a-long-password")

	plan := mustDo(t, admin, ts, 201, "POST", "/api/v1/plans",
		`{"slug":"starter","name":"Starter","maxBots":2,"memoryMb":512,"cpuMillicores":1000,"diskMb":1024}`)
	mustDo(t, admin, ts, 201, "POST", "/api/v1/users",
		`{"email":"alice@example.com","name":"Alice","password":"alice-password","planId":"`+plan["id"].(string)+`"}`)
	mustDo(t, admin, ts, 201, "POST", "/api/v1/users", `{"email":"bob@example.com","name":"Bob","password":"bob-password-1"}`)

	alice := signIn(t, ts, "alice@example.com", "alice-password")
	bob := signIn(t, ts, "bob@example.com", "bob-password-1")

	// Users cannot reach admin endpoints.
	if code, _ := do(t, alice, ts, "GET", "/api/v1/users", ""); code != 403 {
		t.Fatalf("user listed users: %d", code)
	}
	subs := doList(t, alice, ts, "/api/v1/me/subscriptions")
	if len(subs) != 1 {
		t.Fatalf("alice subscriptions: %v", subs)
	}
	subID := subs[0]["id"].(string)

	botBody := func(name string, mem int) string {
		return `{"subscriptionId":"` + subID + `","name":"` + name + `","template":"discord-js","memoryMb":` + itoaT(mem) +
			`,"cpuMillicores":250,"diskMb":256,"env":{"DISCORD_TOKEN":"tok-` + name + `"}}`
	}

	// No node yet: nowhere to place a bot.
	if code, b := do(t, alice, ts, "POST", "/api/v1/bots", botBody("early", 128)); code != 409 || errCode(b) != "no_capacity" {
		t.Fatalf("bot without nodes: %d %v", code, b)
	}

	node := mustDo(t, admin, ts, 201, "POST", "/api/v1/nodes", `{"name":"fra-1","region":"eu","memoryMb":4096,"cpuMillicores":4000,"diskMb":20480}`)
	token := node["setup"].(map[string]any)["token"].(string)
	agent := connectAgent(t, ts, token)
	if sync := agent.next(proto.TypeSync); !strings.Contains(string(sync.Data), `"bots":[]`) {
		t.Fatalf("first sync should be empty: %s", sync.Data)
	}

	// Required template env is enforced.
	if code, b := do(t, alice, ts, "POST", "/api/v1/bots", `{"subscriptionId":"`+subID+`","name":"x","template":"discord-js","memoryMb":128,"cpuMillicores":250,"diskMb":256}`); code != 400 {
		t.Fatalf("missing token accepted: %d %v", code, b)
	}

	bot := mustDo(t, alice, ts, 201, "POST", "/api/v1/bots", botBody("ticket-tool", 256))
	botID := bot["id"].(string)
	spec := specOf(t, agent.next(proto.TypeBotApply))
	if spec.BotID != botID || spec.Env["DISCORD_TOKEN"] != "tok-ticket-tool" || spec.Limits.MemoryMB != 256 ||
		spec.Limits.Pids != 128 || spec.UID < 100001 || spec.DeployID != "" || spec.Desired != proto.DesiredRunning {
		t.Fatalf("unexpected spec: %+v", spec)
	}

	// Quotas: 256 used of 512; another 384 does not fit, 256 does, then the bot count is full.
	if code, b := do(t, alice, ts, "POST", "/api/v1/bots", botBody("too-big", 384)); code != 409 || errCode(b) != "quota" {
		t.Fatalf("over-quota bot: %d %v", code, b)
	}
	bot2 := mustDo(t, alice, ts, 201, "POST", "/api/v1/bots", botBody("music", 256))
	spec2 := specOf(t, agent.next(proto.TypeBotApply))
	if spec2.UID == spec.UID {
		t.Fatal("two bots share a Linux UID")
	}
	if code, b := do(t, alice, ts, "POST", "/api/v1/bots", botBody("third", 64)); code != 409 || errCode(b) != "quota" {
		t.Fatalf("third bot on a 2-bot plan: %d %v", code, b)
	}

	// Other users cannot see or touch it, and cannot use Alice's subscription.
	if code, _ := do(t, bob, ts, "GET", "/api/v1/bots/"+botID, ""); code != 404 {
		t.Fatalf("bob saw alice's bot: %d", code)
	}
	if code, _ := do(t, bob, ts, "POST", "/api/v1/bots/"+botID+"/actions", `{"action":"stop"}`); code != 404 {
		t.Fatalf("bob stopped alice's bot: %d", code)
	}
	if code, _ := do(t, bob, ts, "POST", "/api/v1/bots", botBody("stolen", 64)); code != 400 {
		t.Fatalf("bob used alice's subscription: %d", code)
	}
	if got := doList(t, bob, ts, "/api/v1/bots"); len(got) != 0 {
		t.Fatalf("bob's bot list leaks: %v", got)
	}

	// Secrets are write-only through the API.
	env := doList(t, alice, ts, "/api/v1/bots/"+botID+"/env")
	if len(env) != 1 || env[0]["key"] != "DISCORD_TOKEN" || env[0]["value"] != "" || env[0]["secret"] != true {
		t.Fatalf("env read leaks a secret: %v", env)
	}
	// Saving with an empty secret keeps it; a new plain var is added.
	if code, b := do(t, alice, ts, "PUT", "/api/v1/bots/"+botID+"/env", `{"vars":[{"key":"DISCORD_TOKEN","value":"","secret":true},{"key":"PREFIX","value":"!"}]}`); code != 204 {
		t.Fatalf("put env: %d %v", code, b)
	}
	spec = specOf(t, agent.next(proto.TypeBotApply))
	if spec.Env["DISCORD_TOKEN"] != "tok-ticket-tool" || spec.Env["PREFIX"] != "!" {
		t.Fatalf("env after update: %v", spec.Env)
	}

	// Deploy a zipped folder: the top-level folder is stripped.
	code, dep := upload(t, alice, ts, "/api/v1/bots/"+botID+"/deploys", zipOf(t, map[string]string{
		"my-bot/index.js": "console.log('hi')", "my-bot/package.json": "{}", "my-bot/__MACOSX/._index.js": "junk",
	}))
	if code != 201 {
		t.Fatalf("deploy: %d %v", code, dep)
	}
	spec = specOf(t, agent.next(proto.TypeBotApply))
	if spec.DeployID != dep["id"] || spec.ArtifactPath != "/agent/v1/artifacts/"+spec.DeployID || len(spec.ArtifactSHA256) != 64 {
		t.Fatalf("deploy spec: %+v", spec)
	}

	// The node fetches the artifact with its token; nobody else can.
	get := func(tok string) (int, []byte) {
		req, _ := http.NewRequest("GET", ts.URL+spec.ArtifactPath, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b
	}
	code, art := get(token)
	if code != 200 {
		t.Fatalf("artifact fetch: %d", code)
	}
	if names := tarNames(t, art); strings.Join(names, ",") != "index.js,package.json" {
		t.Fatalf("artifact contents: %v", names)
	}
	if code, _ := get(""); code != 401 {
		t.Fatalf("artifact without token: %d", code)
	}
	other := mustDo(t, admin, ts, 201, "POST", "/api/v1/nodes", `{"name":"fra-2","memoryMb":4096,"cpuMillicores":4000,"diskMb":20480}`)
	if code, _ := get(other["setup"].(map[string]any)["token"].(string)); code != 404 {
		t.Fatalf("another node fetched this node's artifact: %d", code)
	}

	// The agent reports progress; the deploy goes live and the bot runs.
	agent.event(proto.TypeDeployProgress, proto.DeployProgress{DeployID: spec.DeployID, BotID: botID, Phase: proto.PhaseInstalling, Lines: []proto.LogLine{{Text: "npm ci"}}})
	agent.event(proto.TypeDeployProgress, proto.DeployProgress{DeployID: spec.DeployID, BotID: botID, Phase: proto.PhaseLive})
	agent.event(proto.TypeBotState, proto.BotState{BotID: botID, DeployID: spec.DeployID, State: proto.StateRunning, At: time.Now()})
	waitFor(t, func() bool {
		b := mustDo(t, alice, ts, 200, "GET", "/api/v1/bots/"+botID, "")
		return b["state"] == "running"
	})
	deploys := doList(t, alice, ts, "/api/v1/bots/"+botID+"/deploys")
	if len(deploys) != 1 || deploys[0]["status"] != "live" || deploys[0]["current"] != true {
		t.Fatalf("deploys: %v", deploys)
	}
	logged := mustDo(t, alice, ts, 200, "GET", "/api/v1/bots/"+botID+"/deploys/"+spec.DeployID, "")
	if !strings.Contains(logged["log"].(string), "npm ci") {
		t.Fatalf("deploy log: %v", logged["log"])
	}

	// A failed second deploy points the bot back at the live one.
	_, dep2 := upload(t, alice, ts, "/api/v1/bots/"+botID+"/deploys", zipOf(t, map[string]string{"index.js": "broken("}))
	agent.next(proto.TypeBotApply)
	agent.event(proto.TypeDeployProgress, proto.DeployProgress{DeployID: dep2["id"].(string), BotID: botID, Phase: proto.PhaseFailed, Error: "install failed"})
	spec = specOf(t, agent.next(proto.TypeBotApply))
	if spec.DeployID != dep["id"] {
		t.Fatalf("after a failed deploy the spec should point at the live one, got %q", spec.DeployID)
	}

	// Stopping is reflected in the spec.
	mustDo(t, alice, ts, 200, "POST", "/api/v1/bots/"+botID+"/actions", `{"action":"stop"}`)
	if spec = specOf(t, agent.next(proto.TypeBotApply)); spec.Desired != proto.DesiredStopped {
		t.Fatalf("stop: desired %q", spec.Desired)
	}
	mustDo(t, alice, ts, 200, "POST", "/api/v1/bots/"+botID+"/actions", `{"action":"start"}`)
	agent.next(proto.TypeBotApply)

	// Suspending the user stops every bot they have, and blocks their session.
	users := doList(t, admin, ts, "/api/v1/users")
	var aliceID string
	for _, u := range users {
		if u["email"] == "alice@example.com" {
			aliceID = u["id"].(string)
		}
	}
	mustDo(t, admin, ts, 200, "POST", "/api/v1/users/"+aliceID+"/suspend", `{"suspended":true}`)
	for range 2 {
		if s := specOf(t, agent.next(proto.TypeBotApply)); s.Desired != proto.DesiredStopped {
			t.Fatalf("suspended user's bot %s still desired %s", s.BotID, s.Desired)
		}
	}
	if code, _ := do(t, alice, ts, "GET", "/api/v1/me", ""); code != 401 {
		t.Fatalf("suspended user still signed in: %d", code)
	}

	// Deleting a bot tells the agent to remove it.
	mustDo(t, admin, ts, 200, "POST", "/api/v1/users/"+aliceID+"/suspend", `{"suspended":false}`)
	if code, _ := do(t, admin, ts, "DELETE", "/api/v1/bots/"+bot2["id"].(string), ""); code != 204 {
		t.Fatalf("delete bot: %d", code)
	}
	removed := agent.next(proto.TypeBotRemove)
	if !strings.Contains(string(removed.Data), bot2["id"].(string)) {
		t.Fatalf("bot.remove for the wrong bot: %s", removed.Data)
	}

	// A reconnecting agent gets the full desired state again.
	agent.ws.Close(websocket.StatusNormalClosure, "")
	agent = connectAgent(t, ts, token)
	var sync proto.Sync
	json.Unmarshal(agent.next(proto.TypeSync).Data, &sync)
	if len(sync.Bots) != 1 || sync.Bots[0].BotID != botID || sync.Bots[0].DeployID != dep["id"] {
		t.Fatalf("resync: %+v", sync.Bots)
	}
}

func TestAPIKeys(t *testing.T) {
	ts, q := testServer(t)
	createUser(t, q, "a@example.com", "a-long-password", db.UserRoleUser)
	c := signIn(t, ts, "a@example.com", "a-long-password")
	if code, _ := do(t, c, ts, "POST", "/api/v1/me/keys", `{"name":"ci","scopes":["operator"]}`); code != 403 {
		t.Fatalf("user created an operator key: %d", code)
	}
	key := mustDo(t, c, ts, 201, "POST", "/api/v1/me/keys", `{"name":"ci","scopes":["bots:read"]}`)
	secret := key["secret"].(string)
	if !strings.HasPrefix(secret, "mk_") {
		t.Fatalf("key format: %s", secret)
	}
	if keys := doList(t, c, ts, "/api/v1/me/keys"); len(keys) != 1 || keys[0]["secret"] != nil {
		t.Fatalf("key list leaks the secret: %v", keys)
	}

	bare := &http.Client{}
	call := func(method, path, body string) int {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")
		res, err := bare.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := call("GET", "/api/v1/bots", ""); code != 200 {
		t.Fatalf("read with a read key: %d", code)
	}
	if code := call("POST", "/api/v1/bots", `{}`); code != 403 {
		t.Fatalf("write with a read key: %d", code)
	}
	if code := call("POST", "/api/v1/me/keys", `{"name":"x","scopes":["bots:read"]}`); code != 403 {
		t.Fatalf("an API key minted another key: %d", code)
	}
	mustDo(t, c, ts, 204, "DELETE", "/api/v1/me/keys/"+key["id"].(string), "")
	if code := call("GET", "/api/v1/bots", ""); code != 401 {
		t.Fatalf("revoked key still works: %d", code)
	}
}

func TestMaliciousUploadsAreRejected(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		"traversal": zipOf(t, map[string]string{"../../etc/passwd": "x"}),
		"absolute":  zipOf(t, map[string]string{"/etc/passwd": "x"}),
		"empty":     zipOf(t, map[string]string{}),
		"not zip":   []byte("hello, I am not an archive"),
	}
	var tgz bytes.Buffer
	gz := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "evil", Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"})
	tw.Close()
	gz.Close()
	cases["symlink"] = tgz.Bytes()

	var bomb bytes.Buffer
	gz = gzip.NewWriter(&bomb)
	tw = tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "big", Typeflag: tar.TypeReg, Size: 64 << 20, Mode: 0o644})
	tw.Write(make([]byte, 64<<20))
	tw.Close()
	gz.Close()
	cases["bigger than disk"] = bomb.Bytes()

	for name, file := range cases {
		if _, err := storeUpload(dir, bytes.NewReader(file), 16<<20); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for range 50 {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

func itoaT(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + jsonNum(n)) }

func jsonNum(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestSubscriptionOverrides(t *testing.T) {
	ts, q := testServer(t)
	createUser(t, q, "admin@example.com", "a-long-password", db.UserRoleAdmin)
	admin := signIn(t, ts, "admin@example.com", "a-long-password")
	plan := mustDo(t, admin, ts, 201, "POST", "/api/v1/plans", `{"slug":"tiny","name":"Tiny","maxBots":1,"memoryMb":256,"cpuMillicores":250,"diskMb":512}`)
	mustDo(t, admin, ts, 201, "POST", "/api/v1/users", `{"email":"c@example.com","name":"Carol","password":"carol-password","planId":"`+plan["id"].(string)+`"}`)
	node := mustDo(t, admin, ts, 201, "POST", "/api/v1/nodes", `{"name":"n1","memoryMb":8192,"cpuMillicores":8000,"diskMb":40960}`)
	agent := connectAgent(t, ts, node["setup"].(map[string]any)["token"].(string))
	agent.next(proto.TypeSync)

	carol := signIn(t, ts, "c@example.com", "carol-password")
	subID := doList(t, carol, ts, "/api/v1/me/subscriptions")[0]["id"].(string)
	big := `{"subscriptionId":"` + subID + `","name":"big","template":"bun","memoryMb":1024,"cpuMillicores":250,"diskMb":512,"env":{"DISCORD_TOKEN":"t"}}`
	if code, b := do(t, carol, ts, "POST", "/api/v1/bots", big); code != 409 || errCode(b) != "quota" {
		t.Fatalf("1 GB bot on a 256 MB plan: %d %v", code, b)
	}

	// The admin gives Carol more memory and a higher process limit than the plan.
	mustDo(t, admin, ts, 200, "PATCH", "/api/v1/subscriptions/"+subID, `{"overrides":{"memoryMb":2048,"pidsMax":512},"note":"beta tester"}`)
	sub := doList(t, carol, ts, "/api/v1/me/subscriptions")[0]
	limits := sub["limits"].(map[string]any)
	if limits["memoryMb"] != float64(2048) || limits["maxBots"] != float64(1) || sub["note"] != "beta tester" {
		t.Fatalf("effective limits: %v note %v", limits, sub["note"])
	}
	mustDo(t, carol, ts, 201, "POST", "/api/v1/bots", big)
	if spec := specOf(t, agent.next(proto.TypeBotApply)); spec.Limits.Pids != 512 {
		t.Fatalf("pids override not in the spec: %d", spec.Limits.Pids)
	}

	// Bad overrides are refused, and clearing them goes back to the plan.
	if code, _ := do(t, admin, ts, "PATCH", "/api/v1/subscriptions/"+subID, `{"overrides":{"memoryMb":1}}`); code != 400 {
		t.Fatalf("1 MB override accepted: %d", code)
	}
	mustDo(t, admin, ts, 200, "PATCH", "/api/v1/subscriptions/"+subID, `{"overrides":{}}`)
	if l := doList(t, carol, ts, "/api/v1/me/subscriptions")[0]["limits"].(map[string]any); l["memoryMb"] != float64(256) {
		t.Fatalf("cleared override: %v", l)
	}
	// Users cannot change their own limits.
	if code, _ := do(t, carol, ts, "PATCH", "/api/v1/subscriptions/"+subID, `{"overrides":{"memoryMb":99999}}`); code != 403 {
		t.Fatalf("user changed their own limits: %d", code)
	}
}
