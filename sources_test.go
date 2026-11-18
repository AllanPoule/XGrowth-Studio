package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func itemsFrom(d *Data, source string) []*Item {
	var out []*Item
	for _, it := range d.Items {
		if it.Source == source {
			out = append(out, it)
		}
	}
	return out
}

func TestChangelogBringsOnlyNewEntries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := filepath.Join(t.TempDir(), "CHANGELOG.md")
	_ = os.WriteFile(f, []byte("# Changelog\n- 1.0 : première version\n"), 0o600)
	if _, err := e.st.SaveSources("main", SourceSettings{Changelog: "CHANGELOG.md"}, nil); err == nil {
		t.Fatal("un chemin relatif doit être refusé")
	}
	if _, err := e.st.SaveSources("main", SourceSettings{Changelog: f}, nil); err != nil {
		t.Fatal(err)
	}
	r := e.st.PollSources(ctx, "", false)
	if len(r) != 1 || !r[0].Baseline || len(itemsFrom(e.st.store.Snapshot(), "changelog")) != 0 {
		t.Fatalf("le premier relevé sert de référence : %+v", r)
	}
	if _, err := os.Stat(e.st.sourcesPath()); err != nil {
		t.Fatal("l'état des sources doit être écrit dans sources.json")
	}
	if b, _ := os.ReadFile(e.st.store.path); strings.Contains(string(b), "changelogSeen") {
		t.Fatal("l'état des sources ne doit pas alourdir data.json")
	}
	_ = os.WriteFile(f, []byte("# Changelog\n- 1.1 : export CSV\n- 1.0 : première version\n"), 0o600)
	if r := e.st.PollSources(ctx, "", false); len(r) != 1 || r[0].Changelog != 0 {
		t.Fatalf("hors relevé forcé, pas avant l'intervalle : %+v", r)
	}
	e.now = e.now.Add(61 * time.Minute)
	r = e.st.PollSources(ctx, "", false)
	items := itemsFrom(e.st.store.Snapshot(), "changelog")
	if r[0].Changelog != 1 || len(items) != 1 || !strings.Contains(items[0].Text, "1.1 : export CSV") || strings.Contains(items[0].Text, "première version") {
		t.Fatalf("seules les nouveautés arrivent dans la veille : %+v %+v", r, items)
	}
	if items[0].Status != ItemPending || items[0].URL != "" {
		t.Fatalf("contenu de veille : %+v", items[0])
	}

	// changelog par URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "v2 : nouveautés\n") }))
	defer srv.Close()
	if _, err := e.st.SaveSources("main", SourceSettings{Changelog: srv.URL + "/changelog"}, nil); err != nil {
		t.Fatal(err)
	}
	if r := e.st.PollSources(ctx, "main", true); r[0].Error != "" || r[0].Changelog != 1 {
		t.Fatalf("changelog par URL : %+v", r)
	}
}

func TestGitHubStarsBecomeItems(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	repos := []map[string]any{{"id": 1, "full_name": "acme/ancien", "html_url": "https://github.com/acme/ancien", "description": "déjà là", "stargazers_count": 10, "language": "Go", "owner": map[string]string{"login": "acme"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/allan/starred" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(repos)
	}))
	defer srv.Close()
	old := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = old })
	if _, err := e.st.SaveSources("main", SourceSettings{GitHubUser: "pas un login !"}, nil); err == nil {
		t.Fatal("login GitHub invalide accepté")
	}
	if _, err := e.st.SaveSources("main", SourceSettings{GitHubUser: "@allan"}, nil); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if r := e.st.PollSources(ctx, "", true); !r[0].Baseline || r[0].Stars != 0 {
		t.Fatalf("référence : %+v", r)
	}
	mu.Lock()
	repos = append([]map[string]any{{"id": 2, "full_name": "acme/nouveau", "html_url": "https://github.com/acme/nouveau", "description": "agents IA", "stargazers_count": 1200, "language": "Rust", "owner": map[string]string{"login": "acme"}}}, repos...)
	mu.Unlock()
	r := e.st.PollSources(ctx, "", true)
	items := itemsFrom(e.st.store.Snapshot(), "github:allan")
	if r[0].Stars != 1 || len(items) != 1 || !strings.Contains(items[0].Text, "acme/nouveau — agents IA (Rust, 1200 étoiles)") || items[0].URL != "https://github.com/acme/nouveau" {
		t.Fatalf("nouveau dépôt étoilé : %+v %+v", r, items)
	}
}

func TestNewsWaitsForTheThreadToSettle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	w, err := e.st.AddWatch("main", "user", "editeur", 30)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := e.st.AddWatch("main", "user", "bavard", 30)
	if _, err := e.st.SaveSources("main", SourceSettings{News: true, NewsPrompt: "Commence par la version."}, []string{w.ID, other.ID}); err != nil {
		t.Fatal(err)
	}
	_, _ = e.st.SaveSources("main", SourceSettings{News: true, NewsPrompt: "Commence par la version."}, []string{w.ID})
	add := func(src, text string, age time.Duration) {
		_ = e.st.store.Update(func(d *Data) error {
			d.Items = append(d.Items, &Item{ID: newID("i"), Account: "main", Source: src, Author: strings.TrimPrefix(src, "watch:user:"), Text: text, Status: ItemPending, FetchedAt: e.now.Add(-age)})
			return nil
		})
	}
	add("watch:user:editeur", "Version 2.0 disponible", 10*time.Minute)
	add("watch:user:editeur", "Détail : export CSV inclus", 2*time.Minute)
	add("watch:user:bavard", "Une nouveauté qui n'est pas d'une source primaire", 30*time.Minute)
	add("watch:user:editeur", "Vieille annonce", 13*time.Hour)

	if r := e.st.DraftNews(ctx); len(r) != 0 {
		t.Fatalf("le fil bouge encore (dernier post il y a 2 min) : %+v", r)
	}
	e.now = e.now.Add(3 * time.Minute)
	r := e.st.DraftNews(ctx)
	if len(r) != 1 || r[0].Drafted != 1 {
		t.Fatalf("actualité attendue : %+v", r)
	}
	mustContain(t, "demande", e.ai.lastMsg, "Actualité d'une source primaire suivie", "Commence par la version.", "Suite du fil", "export CSV inclus", "Version 2.0 disponible")
	if strings.Contains(e.ai.lastMsg, "Vieille annonce") || strings.Contains(e.ai.lastMsg, "pas d'une source primaire") {
		t.Fatal("ni les annonces trop anciennes ni les comptes non primaires")
	}
	if r := e.st.DraftNews(ctx); len(r) != 0 {
		t.Fatalf("un fil n'est rédigé qu'une fois : %+v", r)
	}

	// limite du jour
	_ = e.st.store.Update(func(d *Data) error { d.Settings.NewsMaxPerDay = 1; return nil })
	add("watch:user:editeur", "Version 2.1", 10*time.Minute)
	if r := e.st.DraftNews(ctx); len(r) != 0 {
		t.Fatalf("limite d'une actualité par jour : %+v", r)
	}
	e.now = e.now.Add(24 * time.Hour)
	add("watch:user:editeur", "Version 2.2", 10*time.Minute)
	if r := e.st.DraftNews(ctx); len(r) != 1 {
		t.Fatalf("le compteur repart chaque jour : %+v", r)
	}
	// seul un compte suivi peut être une source primaire
	d := e.st.store.Snapshot()
	for _, x := range d.Account("main").Watch {
		if x.Value == "bavard" && x.Primary {
			t.Fatal("seules les cibles cochées sont primaires")
		}
	}
}

func TestMethodGuidesTriage(t *testing.T) {
	e := newEnv(t)
	_, _ = e.st.SaveSources("main", SourceSettings{Method: "Garde les annonces officielles, ignore les rumeurs."}, nil)
	_ = e.st.store.Update(func(d *Data) error {
		d.Items = append(d.Items, &Item{ID: "i_1", Account: "main", Author: "x", Text: "agent", Status: ItemPending, FetchedAt: e.now})
		return nil
	})
	e.st.TriagePending(context.Background(), "main", false, false)
	mustContain(t, "tri", e.ai.lastMsg, "<methode_de_recuperation>", "ignore les rumeurs")
}

func TestAPISources(t *testing.T) {
	srv, e := tuningServer(t)
	c := &http.Client{}
	w, _ := e.st.AddWatch("main", "user", "editeur", 30)
	f := filepath.Join(t.TempDir(), "CHANGELOG")
	_ = os.WriteFile(f, []byte("v1\n"), 0o600)
	body := fmt.Sprintf(`{"sources":{"changelog":%q,"news":true,"method":"m"},"primary":[%q]}`, f, w.ID)
	if st, m := call(t, c, "POST", srv.URL+"/api/accounts/main/sources", body); st != 200 {
		t.Fatalf("sources refusées : %v", m)
	}
	a := e.st.store.Snapshot().Account("main")
	if !a.Sources.News || a.Sources.Changelog != f || !a.Watch[0].Primary {
		t.Fatalf("sources enregistrées : %+v", a.Sources)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/accounts/main/sources", `{"sources":{"changelog":"relatif.md"}}`); st != 400 {
		t.Fatal("chemin relatif accepté")
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/sources/poll", `{"account":"main"}`); st != 200 {
		t.Fatal("relevé refusé")
	}
	if st, m := call(t, c, "POST", srv.URL+"/api/news/draft", `{}`); st != 200 || m["data"] == nil {
		t.Fatalf("sans actualité prête, une liste vide (pas null) : %v", m)
	}
	_, m := call(t, c, "GET", srv.URL+"/api/sources", "")
	data := m["data"].(map[string]any)
	if data["accounts"].(map[string]any)["main"] == nil || data["defaults"].(map[string]any)["newsMaxPerDay"].(float64) != 8 {
		t.Fatalf("état des sources : %v", data)
	}
	// l'enregistrement du compte complet garde les sources
	acc := e.st.store.Snapshot().Account("main")
	b, _ := json.Marshal(acc)
	call(t, c, "POST", srv.URL+"/api/accounts", string(b))
	if !e.st.store.Snapshot().Account("main").Sources.News {
		t.Fatal("les sources doivent survivre à l'enregistrement du compte")
	}
	if !onOff("oui") || onOff("off") {
		t.Fatal("onOff")
	}
}
