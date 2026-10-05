package panel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/config"
	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/secrets"
)

// Integration tests run against a real Postgres. Point MECHON_TEST_DATABASE_URL at a throwaway
// database; its public schema is wiped on every run.
func testServer(t *testing.T) (*httptest.Server, *db.Queries) {
	t.Helper()
	dsn := os.Getenv("MECHON_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MECHON_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	web := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Mechon</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	var ts *httptest.Server
	ts = httptest.NewUnstartedServer(nil)
	u, _ := url.Parse("http://" + ts.Listener.Addr().String())
	srv, err := New(config.Panel{PublicURL: u, SecretKey: secrets.NewKey(), DataDir: t.TempDir()}, pool, web)
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = srv.Handler()
	jobsCtx, stopJobs := context.WithCancel(context.Background())
	t.Cleanup(stopJobs)
	if err := srv.StartJobs(jobsCtx); err != nil {
		t.Fatal(err)
	}
	ts.Start()
	t.Cleanup(ts.Close)
	return ts, db.New(pool)
}

func createUser(t *testing.T, q *db.Queries, email, password string, role db.UserRole) db.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{Email: email, Name: "Test", PasswordHash: &hash, Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func newClient(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func do(t *testing.T, c *http.Client, ts *httptest.Server, method, path, body string, hdr ...string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", ts.URL)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestLoginFlow(t *testing.T) {
	ts, q := testServer(t)
	createUser(t, q, "admin@example.com", "a-long-password", db.UserRoleAdmin)
	c := newClient(t)

	if code, body := do(t, c, ts, "GET", "/api/v1/me", ""); code != 401 || errCode(body) != "unauthorized" {
		t.Fatalf("me before login: %d %v", code, body)
	}
	if code, body := do(t, c, ts, "POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"wrong-password"}`); code != 401 || errCode(body) != "bad_credentials" {
		t.Fatalf("wrong password: %d %v", code, body)
	}
	if code, body := do(t, c, ts, "POST", "/api/v1/auth/login", `{"email":"nobody@example.com","password":"whatever-pass"}`); code != 401 || errCode(body) != "bad_credentials" {
		t.Fatalf("unknown email must look like a wrong password: %d %v", code, body)
	}
	code, body := do(t, c, ts, "POST", "/api/v1/auth/login", `{"email":"  Admin@Example.com ","password":"a-long-password"}`)
	if code != 200 || body["role"] != "admin" || body["email"] != "admin@example.com" {
		t.Fatalf("login: %d %v", code, body)
	}
	if _, ok := body["passwordHash"]; ok {
		t.Fatal("login response leaks the password hash")
	}
	if code, body := do(t, c, ts, "GET", "/api/v1/me", ""); code != 200 || body["email"] != "admin@example.com" {
		t.Fatalf("me after login: %d %v", code, body)
	}
	if code, _ := do(t, c, ts, "POST", "/api/v1/auth/logout", ""); code != 204 {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := do(t, c, ts, "GET", "/api/v1/me", ""); code != 401 {
		t.Fatalf("me after logout: %d", code)
	}
}

func TestSessionCookieIsHardened(t *testing.T) {
	ts, q := testServer(t)
	createUser(t, q, "a@example.com", "a-long-password", db.UserRoleUser)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"a-long-password"}`))
	req.Header.Set("Origin", ts.URL)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var found bool
	for _, c := range res.Cookies() {
		if c.Name == sessionCookie {
			found = true
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
				t.Fatalf("cookie flags: %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("no session cookie set")
	}
}

func TestCrossSiteWritesAreRejected(t *testing.T) {
	ts, q := testServer(t)
	createUser(t, q, "a@example.com", "a-long-password", db.UserRoleUser)
	c := newClient(t)
	body := `{"email":"a@example.com","password":"a-long-password"}`
	if code, b := do(t, c, ts, "POST", "/api/v1/auth/login", body, "Origin", "https://evil.example"); code != 403 || errCode(b) != "bad_origin" {
		t.Fatalf("foreign origin: %d %v", code, b)
	}
	if code, b := do(t, c, ts, "POST", "/api/v1/auth/login", body, "Sec-Fetch-Site", "cross-site"); code != 403 || errCode(b) != "bad_origin" {
		t.Fatalf("cross-site fetch: %d %v", code, b)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	ts, q := testServer(t)
	createUser(t, q, "a@example.com", "a-long-password", db.UserRoleUser)
	c := newClient(t)
	var last int
	for range 11 {
		last, _ = do(t, c, ts, "POST", "/api/v1/auth/login", `{"email":"a@example.com","password":"nope-nope-nope"}`)
	}
	if last != 429 {
		t.Fatalf("11th bad attempt: got %d, want 429", last)
	}
	// The right password is refused too while the account is locked out.
	if code, _ := do(t, c, ts, "POST", "/api/v1/auth/login", `{"email":"a@example.com","password":"a-long-password"}`); code != 429 {
		t.Fatalf("locked account accepted a login: %d", code)
	}
}

func TestSuspendedUserIsSignedOut(t *testing.T) {
	ts, q := testServer(t)
	u := createUser(t, q, "a@example.com", "a-long-password", db.UserRoleUser)
	c := newClient(t)
	if code, _ := do(t, c, ts, "POST", "/api/v1/auth/login", `{"email":"a@example.com","password":"a-long-password"}`); code != 200 {
		t.Fatal("login failed")
	}
	pool, _ := pgxpool.New(context.Background(), os.Getenv("MECHON_TEST_DATABASE_URL"))
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), "UPDATE users SET suspended_at = now() WHERE id = $1", u.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := do(t, c, ts, "GET", "/api/v1/me", ""); code != 401 {
		t.Fatalf("suspended user still signed in: %d", code)
	}
	if code, b := do(t, c, ts, "POST", "/api/v1/auth/login", `{"email":"a@example.com","password":"a-long-password"}`); code != 403 || errCode(b) != "suspended" {
		t.Fatalf("suspended login: %d %v", code, b)
	}
}

func TestSPAFallbackAndAPI404(t *testing.T) {
	ts, _ := testServer(t)
	for path, want := range map[string]string{"/": "<!doctype html>", "/bots/123": "<!doctype html>", "/assets/app.js": "console.log"} {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.HasPrefix(string(b), want) {
			t.Errorf("%s: %d %q", path, res.StatusCode, b)
		}
		if res.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no CSP header", path)
		}
	}
	res, _ := http.Get(ts.URL + "/assets/app.js")
	res.Body.Close()
	if !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("hashed assets should be cached: %q", res.Header.Get("Cache-Control"))
	}
	if code, b := do(t, newClient(t), ts, "GET", "/api/v1/nope", ""); code != 404 || errCode(b) != "not_found" {
		t.Errorf("unknown API path: %d %v", code, b)
	}
}
