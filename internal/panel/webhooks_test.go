package panel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jub0t/mechon/internal/db"
)

type received struct {
	event, delivery, signature string
	body                       []byte
}

func TestWebhooksAuditAndSettings(t *testing.T) {
	ts, q := testServer(t)
	createUser(t, q, "admin@example.com", "a-long-password", db.UserRoleAdmin)
	admin := signIn(t, ts, "admin@example.com", "a-long-password")

	got := make(chan received, 10)
	failFirst := true
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{r.Header.Get("Mechon-Event"), r.Header.Get("Mechon-Delivery"), r.Header.Get("Mechon-Signature"), body}
		if failFirst && r.Header.Get("Mechon-Event") == "user.created" {
			failFirst = false
			http.Error(w, "try again", http.StatusServiceUnavailable)
		}
	}))
	defer sink.Close()

	hook := mustDo(t, admin, ts, 201, "POST", "/api/v1/webhooks", `{"url":"`+sink.URL+`","events":["user.created"]}`)
	secret := hook["secret"].(string)
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("secret: %q", secret)
	}

	mustDo(t, admin, ts, 201, "POST", "/api/v1/users", `{"email":"dana@example.com","name":"Dana","password":"dana-password"}`)
	// Not subscribed: plan events must not arrive.
	mustDo(t, admin, ts, 201, "POST", "/api/v1/plans", `{"slug":"p","name":"P","maxBots":1,"memoryMb":128,"cpuMillicores":100,"diskMb":256}`)

	wait := func() received {
		t.Helper()
		select {
		case r := <-got:
			return r
		case <-time.After(20 * time.Second):
			t.Fatal("no webhook arrived")
			return received{}
		}
	}
	first := wait()
	// The first attempt got a 503; River retries the same delivery.
	second := wait()
	if first.event != "user.created" || second.delivery != first.delivery {
		t.Fatalf("retry: first %+v second %+v", first, second)
	}
	var payload struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(second.body, &payload)
	if payload.Type != "user.created" || payload.Data["email"] != "dana@example.com" {
		t.Fatalf("payload: %s", second.body)
	}
	// Verify the signature exactly as a receiver would.
	parts := map[string]string{}
	for _, kv := range strings.Split(second.signature, ",") {
		k, v, _ := strings.Cut(kv, "=")
		parts[k] = v
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts["t"] + "." + string(second.body)))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(parts["v1"])) {
		t.Fatalf("signature does not verify: %s", second.signature)
	}
	waitFor(t, func() bool {
		d := doList(t, admin, ts, "/api/v1/webhooks/"+hook["id"].(string)+"/deliveries")
		return len(d) == 1 && d[0]["status"] == "delivered" && d[0]["attempts"] == float64(2)
	})
	select {
	case extra := <-got:
		t.Fatalf("unsubscribed event delivered: %s", extra.event)
	case <-time.After(300 * time.Millisecond):
	}

	// The audit log saw it all, newest first.
	audit := doList(t, admin, ts, "/api/v1/audit")
	var actions []string
	for _, a := range audit {
		actions = append(actions, a["action"].(string))
	}
	if strings.Join(actions, ",") != "plan.create,user.create,webhook.create" {
		t.Fatalf("audit: %v", actions)
	}

	// Settings: admins write, anyone reads the public part.
	mustDo(t, admin, ts, 200, "PUT", "/api/v1/settings", `{"brandName":"Botsy Hosting","supportUrl":"https://botsy.example/help"}`)
	if b := mustDo(t, newClient(t), ts, 200, "GET", "/api/v1/settings/public", ""); b["brandName"] != "Botsy Hosting" {
		t.Fatalf("public settings: %v", b)
	}
	if code, _ := do(t, admin, ts, "PUT", "/api/v1/settings", `{"brandName":"x","supportUrl":"javascript:alert(1)"}`); code != 400 {
		t.Fatalf("javascript: support link accepted: %d", code)
	}
}
