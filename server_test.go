package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------- CLI d'IA

// fakeCLI installe un faux binaire dans un dossier mis en tête du PATH.
func fakeCLI(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestClaudeCodeProviderRunsWithoutTools(t *testing.T) {
	dir := fakeCLI(t, "claude", `printf '%s\n' "$@" > "$(dirname "$0")/args"
env > "$(dirname "$0")/env"
cat > "$(dirname "$0")/stdin"
echo '{"type":"result","subtype":"success","is_error":false,"result":"{}","structured_output":{"text":"Bonjour X","rationale":"test"}}'
`)
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-leak")
	var out DraftOut
	err := (&claudeCodeAI{model: "sonnet"}).JSON(context.Background(), "SYSTEME", "DEMANDE", draftSchema, &out)
	if err != nil || out.Text != "Bonjour X" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	a := string(args)
	for _, want := range []string{"-p\n", "--tools\n\n", "--strict-mcp-config", "--json-schema", "--system-prompt\nSYSTEME", "--model\nsonnet"} {
		if !strings.Contains(a, want) {
			t.Errorf("argument %q absent :\n%s", want, a)
		}
	}
	if stdin, _ := os.ReadFile(filepath.Join(dir, "stdin")); string(stdin) != "DEMANDE" {
		t.Errorf("la demande doit passer par stdin, reçu %q", stdin)
	}
	if env, _ := os.ReadFile(filepath.Join(dir, "env")); strings.Contains(string(env), "sk-should-not-leak") {
		t.Error("Claude Code ne doit pas recevoir ANTHROPIC_API_KEY (il utiliserait l'API au lieu de l'abonnement)")
	}
}

func TestClaudeCodeProviderReportsErrors(t *testing.T) {
	fakeCLI(t, "claude", `echo '{"type":"result","subtype":"error","is_error":true,"result":"Not logged in · Please run /login"}'`)
	var out DraftOut
	err := (&claudeCodeAI{}).JSON(context.Background(), "s", "u", draftSchema, &out)
	if err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("erreur attendue, obtenu %v", err)
	}
}

func TestCodexProviderUsesSchemaAndReadOnlySandbox(t *testing.T) {
	dir := fakeCLI(t, "codex", `printf '%s\n' "$@" > "$(dirname "$0")/args"
out=""; prev=""
for a in "$@"; do [ "$prev" = "-o" ] && out="$a"; prev="$a"; done
cat > /dev/null
echo '{"text":"Salut","rationale":"r"}' > "$out"
`)
	var out DraftOut
	if err := (&codexAI{}).JSON(context.Background(), "s", "u", draftSchema, &out); err != nil || out.Text != "Salut" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	for _, want := range []string{"exec\n", "--sandbox\nread-only", "--output-schema", "--ephemeral", "--skip-git-repo-check"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("argument %q absent :\n%s", want, args)
		}
	}
}

func TestCodexProviderSurfacesAPIError(t *testing.T) {
	fakeCLI(t, "codex", `cat > /dev/null
echo 'ERROR: {"type":"error","status":400,"error":{"message":"The model is not supported when using Codex with a ChatGPT account."}}' >&2
exit 1`)
	var out DraftOut
	err := (&codexAI{}).JSON(context.Background(), "s", "u", draftSchema, &out)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("erreur lisible attendue, obtenu %v", err)
	}
}

func TestDetectProviders(t *testing.T) {
	fakeCLI(t, "codex", `echo "codex-cli 9.9.9"`)
	ps := DetectProviders(true)
	var codex *ProviderInfo
	for i := range ps {
		if ps[i].ID == "codex" {
			codex = &ps[i]
		}
	}
	if codex == nil || !codex.Available || codex.Version != "codex-cli 9.9.9" {
		t.Fatalf("%+v", codex)
	}
}

func TestExtractJSON(t *testing.T) {
	if got := extractJSON("Voici :\n```json\n{\"text\":\"a\"}\n```"); got != `{"text":"a"}` {
		t.Fatal(got)
	}
}

// ---------------------------------------------------------------- authentification HTTP

func testServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	t.Setenv("XGROWTH_HOME", t.TempDir())
	store, err := OpenStore(homeDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(buildHandler(NewStudio(store), ServeOptions{}))
	t.Cleanup(srv.Close)
	return srv, store
}

func call(t *testing.T, c *http.Client, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("X-XGrowth", "1")
	req.Header.Set("Content-Type", "application/json")
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(r.Body).Decode(&m)
	return r.StatusCode, m
}

func TestAuthFlow(t *testing.T) {
	srv, store := testServer(t)
	code, err := EnsureSetupCode(store)
	if err != nil || len(code) != 11 {
		t.Fatalf("code=%q err=%v", code, err)
	}
	jar := &cookieJar{}
	c := &http.Client{Jar: jar}

	if st, _ := call(t, c, "GET", srv.URL+"/api/state", ""); st != 401 {
		t.Fatalf("l'API doit exiger une session, statut %d", st)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/auth/setup", `{"code":"MAUVAIS-CODE","password":"motdepasse-solide"}`); st != 400 {
		t.Fatal("un mauvais code doit être refusé")
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/auth/setup", `{"code":"`+code+`","password":"court"}`); st != 400 {
		t.Fatal("un mot de passe trop court doit être refusé")
	}
	if st, m := call(t, c, "POST", srv.URL+"/api/auth/setup", `{"code":"`+strings.ToLower(code)+`","password":"motdepasse-solide"}`); st != 200 {
		t.Fatalf("installation refusée : %v", m)
	}
	if st, _ := call(t, c, "GET", srv.URL+"/api/state", ""); st != 200 {
		t.Fatal("la session créée à l'installation doit donner accès")
	}
	// le code ne sert qu'une fois
	if st, _ := call(t, &http.Client{}, "POST", srv.URL+"/api/auth/setup", `{"code":"`+code+`","password":"autre-mot-de-passe"}`); st != 400 {
		t.Fatal("le code d'installation ne doit plus servir")
	}
	// un autre navigateur doit se connecter
	other := &http.Client{Jar: &cookieJar{}}
	if st, _ := call(t, other, "POST", srv.URL+"/api/auth/login", `{"password":"faux-mot-de-passe"}`); st != 400 {
		t.Fatal("mauvais mot de passe accepté")
	}
	if st, _ := call(t, other, "POST", srv.URL+"/api/auth/login", `{"password":"motdepasse-solide"}`); st != 200 {
		t.Fatal("bon mot de passe refusé")
	}
	if st, _ := call(t, other, "GET", srv.URL+"/api/state", ""); st != 200 {
		t.Fatal("session de connexion refusée")
	}
	call(t, other, "POST", srv.URL+"/api/auth/logout", "")
	if st, _ := call(t, other, "GET", srv.URL+"/api/state", ""); st != 401 {
		t.Fatal("la déconnexion doit révoquer la session")
	}
	// /api/state ne doit jamais exposer les secrets d'authentification
	_, m := call(t, c, "GET", srv.URL+"/api/state", "")
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "passwordHash") || strings.Contains(string(b), "sessions") {
		t.Fatal("l'état renvoyé au navigateur contient des secrets")
	}
}

func TestHostGuardAndCSRFHeader(t *testing.T) {
	srv, _ := testServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/auth/status", nil)
	req.Host = "attaquant.example"
	r, _ := http.DefaultClient.Do(req)
	if r.StatusCode != 403 {
		t.Fatalf("hôte étranger accepté : %d", r.StatusCode)
	}
	r2, _ := http.Post(srv.URL+"/api/auth/login", "application/json", strings.NewReader(`{"password":"x"}`))
	if r2.StatusCode != 403 {
		t.Fatalf("POST sans en-tête X-XGrowth accepté : %d", r2.StatusCode)
	}
}

// cookieJar minimal (évite net/http/cookiejar et ses règles de domaine sur 127.0.0.1).
type cookieJar struct{ cookies []*http.Cookie }

func (j *cookieJar) SetCookies(_ *url.URL, cs []*http.Cookie) {
	for _, c := range cs {
		kept := j.cookies[:0]
		for _, o := range j.cookies {
			if o.Name != c.Name {
				kept = append(kept, o)
			}
		}
		j.cookies = kept
		if c.MaxAge >= 0 && c.Value != "" {
			j.cookies = append(j.cookies, c)
		}
	}
}
func (j *cookieJar) Cookies(*url.URL) []*http.Cookie { return j.cookies }

func TestWatchSeenIsStoredServerSide(t *testing.T) {
	t.Setenv("XGROWTH_HOME", t.TempDir())
	store, err := OpenStore(homeDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(buildHandler(NewStudio(store), ServeOptions{NoAuth: true}))
	defer srv.Close()
	if st, m := call(t, &http.Client{}, "POST", srv.URL+"/api/watch/seen", ""); st != 200 {
		t.Fatalf("%d %v", st, m)
	}
	if store.Snapshot().Settings.WatchSeenAt == nil {
		t.Fatal("la dernière visite de la veille doit être enregistrée côté serveur")
	}
	// un enregistrement des réglages ne doit pas l'effacer
	call(t, &http.Client{}, "POST", srv.URL+"/api/settings", `{"minGapMinutes":45}`)
	if store.Snapshot().Settings.WatchSeenAt == nil {
		t.Fatal("POST /api/settings ne doit pas effacer watchSeenAt")
	}
}
