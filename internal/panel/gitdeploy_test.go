package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestGitURLValidation(t *testing.T) {
	bad := []string{
		"http://github.com/a/b.git", "git@github.com:a/b.git", "ssh://github.com/a/b.git", "file:///etc",
		"https://user:pass@github.com/a/b.git", "https://127.0.0.1/a.git", "https://10.0.0.5/a.git",
		"https://169.254.169.254/latest", "https://[::1]/a.git", "", "https://",
	}
	for _, u := range bad {
		if _, err := parseGitURL(u); err == nil {
			t.Errorf("accepted %q", u)
		}
	}
	if _, err := parseGitURL(" https://github.com/jub0t/mechon.git "); err != nil {
		t.Errorf("rejected a normal URL: %v", err)
	}
	for _, r := range []string{"main", "release/v1.2", "v0.1.0"} {
		if !validRef(r) {
			t.Errorf("rejected ref %q", r)
		}
	}
	for _, r := range []string{"-x", "a..b", "a b", "a;b", strings.Repeat("a", 201)} {
		if validRef(r) {
			t.Errorf("accepted ref %q", r)
		}
	}
}

// Even when a hostname resolves to an internal address, the dialer refuses to connect.
func TestGitCloneRefusesInternalAddresses(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the panel connected to an internal address")
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://127.0.0.1")
	_, _, err := cloneToArtifact(context.Background(), t.TempDir(), "https://localhost"+host+"/repo.git", "", "", 16<<20)
	if err == nil || !strings.Contains(err.Error(), "private network") {
		t.Fatalf("want a private-network refusal, got %v", err)
	}
}

func TestGitCloneRealRepository(t *testing.T) {
	if os.Getenv("MECHON_TEST_NETWORK") == "" {
		t.Skip("set MECHON_TEST_NETWORK=1 to clone from GitHub")
	}
	dir := t.TempDir()
	art, src, err := cloneToArtifact(context.Background(), dir, "https://github.com/octocat/Hello-World.git", "", "", 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Commit) != 40 || src.Ref == "" || art.Bytes == 0 {
		t.Fatalf("clone result: %+v %+v", src, art)
	}
	raw, _ := os.ReadFile(dir + "/" + art.SHA256 + ".tar.gz")
	if names := tarNames(t, raw); len(names) == 0 || names[0] != "README" {
		t.Fatalf("artifact contents: %v", names)
	}
	if _, _, err := cloneToArtifact(context.Background(), dir, "https://github.com/octocat/Hello-World.git", "no-such-branch", "", 16<<20); err == nil ||
		!strings.Contains(err.Error(), "no branch or tag") {
		t.Fatalf("missing branch: %v", err)
	}
}
