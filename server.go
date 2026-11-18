package main

// Studio web : une application d'une page (web/, embarquée dans le binaire) et
// une API JSON. Protections : mot de passe et sessions (auth.go), liste des noms
// d'hôte autorisés (contre le DNS rebinding), en-tête X-XGrowth exigé sur
// chaque POST (contre les formulaires d'autres sites).

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

type ServeOptions struct {
	Addr   string
	Hosts  []string // noms d'hôte autorisés en plus de localhost
	NoAuth bool
}

func serve(ctx context.Context, st *Studio, opt ServeOptions) error {
	srv := &http.Server{Addr: opt.Addr, Handler: buildHandler(st, opt), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", opt.Addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "xgrowth %s : studio sur http://%s\n", version, ln.Addr())
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func buildHandler(st *Studio, opt ServeOptions) http.Handler {
	gate := &authGate{store: st.store, disable: opt.NoAuth}
	mux := http.NewServeMux()

	static, _ := fs.Sub(webFS, "web")
	files := http.FileServerFS(static)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://unavatar.io; connect-src 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.URL.Path == "/" || !strings.Contains(r.URL.Path, ".") {
			b, _ := fs.ReadFile(static, "index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(b)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})

	decode := func(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
		if r.Header.Get("X-XGrowth") != "1" {
			http.Error(w, "en-tête X-XGrowth manquant", http.StatusForbidden)
			return nil, false
		}
		body := map[string]any{}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
				reply(w, nil, fmt.Errorf("JSON invalide : %w", err))
				return nil, false
			}
		}
		return body, true
	}
	gate.routes(mux, decode)

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		d := st.store.Snapshot()
		items := d.Items
		if len(items) > 300 {
			items = items[len(items)-300:]
		}
		logs := d.Log
		if len(logs) > 80 {
			logs = logs[len(logs)-80:]
		}
		ai := st.aiSettings()
		reply(w, map[string]any{
			"accounts": d.Accounts, "posts": d.Posts, "items": items, "settings": d.Settings,
			"learnings": d.Learnings, "log": logs, "calendar": calendar(d, 21, time.Now()),
			"ai": map[string]string{"provider": ai.AIProvider, "model": ai.Model}, "version": version,
		}, nil)
	})
	mux.HandleFunc("GET /api/providers", func(w http.ResponseWriter, r *http.Request) {
		reply(w, DetectProviders(r.URL.Query().Get("refresh") == "1"), nil)
	})

	post := func(path string, h func(r *http.Request, body map[string]any) (any, error)) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			body, ok := decode(w, r)
			if !ok {
				return
			}
			out, err := h(r, body)
			reply(w, out, err)
		})
	}
	s := func(b map[string]any, k string) string { v, _ := b[k].(string); return strings.TrimSpace(v) }
	n := func(b map[string]any, k string, def int) int {
		if v, ok := b[k].(float64); ok {
			return int(v)
		}
		return def
	}
	bl := func(b map[string]any, k string) bool { v, _ := b[k].(bool); return v }

	// ---- IA
	providerOK := func(id string) error {
		for _, p := range providerCatalog {
			if p.ID == id {
				return nil
			}
		}
		return fmt.Errorf("fournisseur d'IA %q inconnu", id)
	}
	post("/api/ai/test", func(r *http.Request, b map[string]any) (any, error) {
		cfg := st.aiSettings()
		if p := s(b, "provider"); p != "" {
			if err := providerOK(p); err != nil {
				return nil, err
			}
			cfg.AIProvider, cfg.Model = p, s(b, "model")
		}
		return TestAI(r.Context(), cfg), nil
	})
	post("/api/ai/select", func(r *http.Request, b map[string]any) (any, error) {
		p := s(b, "provider")
		if err := providerOK(p); err != nil {
			return nil, err
		}
		return nil, st.store.Update(func(d *Data) error {
			d.Settings.AIProvider, d.Settings.Model = p, s(b, "model")
			if e := s(b, "effort"); e != "" {
				d.Settings.Effort = e
			}
			return nil
		})
	})

	// ---- connexion à X
	post("/api/x/agentx", func(r *http.Request, b map[string]any) (any, error) {
		return AgentxConnect(r.Context(), s(b, "name"), s(b, "authToken"), s(b, "ct0"))
	})
	post("/api/onboarding/complete", func(r *http.Request, b map[string]any) (any, error) {
		return nil, st.store.Update(func(d *Data) error { d.Settings.Onboarded = true; return nil })
	})

	// ---- rédaction et validation
	post("/api/draft", func(r *http.Request, b map[string]any) (any, error) {
		return st.GenerateDrafts(r.Context(), s(b, "account"), s(b, "topic"), s(b, "item"), n(b, "n", 3))
	})
	post("/api/reply", func(r *http.Request, b map[string]any) (any, error) {
		return st.DraftReply(r.Context(), s(b, "account"), s(b, "ref"), bl(b, "quote"))
	})
	post("/api/write", func(r *http.Request, b map[string]any) (any, error) {
		return st.AddDraft(s(b, "account"), s(b, "text"), s(b, "replyTo"), s(b, "quoteOf"), s(b, "media"))
	})
	post("/api/posts/{id}/edit", func(r *http.Request, b map[string]any) (any, error) {
		return st.EditPost(r.PathValue("id"), s(b, "text"))
	})
	post("/api/posts/{id}/media", func(r *http.Request, b map[string]any) (any, error) {
		return st.SetMedia(r.PathValue("id"), s(b, "path"))
	})
	post("/api/posts/{id}/rewrite", func(r *http.Request, b map[string]any) (any, error) {
		if s(b, "feedback") == "" {
			return nil, errors.New("indiquez ce qu'il faut changer")
		}
		return st.RewritePost(r.Context(), r.PathValue("id"), s(b, "feedback"))
	})
	post("/api/posts/{id}/validate", func(r *http.Request, b map[string]any) (any, error) {
		var at *time.Time
		if v := s(b, "at"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, fmt.Errorf("date invalide : %w", err)
			}
			at = &t
		}
		return st.Validate(r.PathValue("id"), s(b, "by"), at, bl(b, "now"))
	})
	post("/api/posts/{id}/reject", func(r *http.Request, b map[string]any) (any, error) {
		return st.Reject(r.PathValue("id"), s(b, "reason"))
	})
	post("/api/posts/{id}/unschedule", func(r *http.Request, b map[string]any) (any, error) {
		return st.Unschedule(r.PathValue("id"))
	})

	// ---- veille
	post("/api/items/{id}/status", func(r *http.Request, b map[string]any) (any, error) {
		switch v := s(b, "status"); v {
		case ItemKept, ItemIgnored, ItemPending:
		default:
			return nil, fmt.Errorf("statut %q invalide", v)
		}
		return st.SetItemStatus(r.PathValue("id"), s(b, "status"))
	})
	post("/api/items", func(r *http.Request, b map[string]any) (any, error) {
		return st.AddItem(r.Context(), s(b, "account"), s(b, "ref"))
	})
	post("/api/fetch", func(r *http.Request, b map[string]any) (any, error) {
		return st.PollWatch(r.Context(), s(b, "account"), true), nil
	})
	post("/api/bookmarks", func(r *http.Request, b map[string]any) (any, error) {
		return st.ImportBookmarks(r.Context(), s(b, "account"), n(b, "n", 20))
	})
	post("/api/triage", func(r *http.Request, b map[string]any) (any, error) {
		return st.TriagePending(r.Context(), s(b, "account"), false, bl(b, "draft")), nil
	})
	post("/api/publish-due", func(r *http.Request, b map[string]any) (any, error) {
		return st.PublishDue(r.Context()), nil
	})
	post("/api/metrics", func(r *http.Request, b map[string]any) (any, error) {
		n, err := st.RefreshMetrics(r.Context(), 0)
		return map[string]any{"updated": n}, err
	})

	// ---- comptes et réglages
	post("/api/accounts", func(r *http.Request, b map[string]any) (any, error) {
		raw, _ := json.Marshal(b)
		var a Account
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		a.Watch = nil // la veille se gère par /api/watch
		return &a, st.SaveAccount(&a)
	})
	post("/api/accounts/{name}/check", func(r *http.Request, b map[string]any) (any, error) {
		return st.CheckAccount(r.Context(), r.PathValue("name"))
	})
	post("/api/accounts/{name}/remove", func(r *http.Request, b map[string]any) (any, error) {
		name := r.PathValue("name")
		return nil, st.store.Update(func(d *Data) error {
			for i, a := range d.Accounts {
				if strings.EqualFold(a.Name, name) {
					d.Accounts = append(d.Accounts[:i], d.Accounts[i+1:]...)
					return nil
				}
			}
			return fmt.Errorf("compte %q inconnu", name)
		})
	})
	post("/api/watch", func(r *http.Request, b map[string]any) (any, error) {
		return st.AddWatch(s(b, "account"), s(b, "kind"), s(b, "value"), n(b, "every", 60))
	})
	post("/api/watch/{id}/remove", func(r *http.Request, b map[string]any) (any, error) {
		return nil, st.RemoveWatch(r.PathValue("id"))
	})
	post("/api/settings", func(r *http.Request, b map[string]any) (any, error) {
		var out Settings
		err := st.store.Update(func(d *Data) error {
			cur := d.Settings
			raw, _ := json.Marshal(b)
			// fusion : seuls les champs envoyés changent
			if err := json.Unmarshal(raw, &cur); err != nil {
				return err
			}
			cur.Onboarded = d.Settings.Onboarded
			d.Settings, out = cur, cur
			return nil
		})
		return out, err
	})
	post("/api/learnings", func(r *http.Request, b map[string]any) (any, error) {
		if s(b, "text") == "" {
			return nil, errors.New("leçon vide")
		}
		l := Learning{ID: newID("l"), Account: s(b, "account"), Text: s(b, "text"), CreatedAt: time.Now().UTC()}
		return l, st.store.Update(func(d *Data) error { d.Learnings = append(d.Learnings, l); return nil })
	})
	post("/api/learnings/{id}/remove", func(r *http.Request, b map[string]any) (any, error) {
		id := r.PathValue("id")
		return nil, st.store.Update(func(d *Data) error {
			for i, l := range d.Learnings {
				if l.ID == id {
					d.Learnings = append(d.Learnings[:i], d.Learnings[i+1:]...)
					return nil
				}
			}
			return errors.New("leçon inconnue")
		})
	})

	return hostGuard(opt.Hosts, gate.wrap(mux))
}

// hostGuard n'accepte que localhost et les noms d'hôte déclarés (--host).
func hostGuard(hosts []string, h http.Handler) http.Handler {
	allowed := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	for _, x := range hosts {
		allowed[strings.ToLower(strings.TrimSpace(x))] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hh, _, err := net.SplitHostPort(host); err == nil {
			host = hh
		}
		if !allowed[strings.ToLower(host)] && !allowed["*"] {
			http.Error(w, "nom d'hôte non autorisé (ajoutez --host "+host+")", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, data any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	e := envelope{OK: err == nil, SchemaVersion: "1", Data: data}
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		e.Error = &apiError{Code: "error", Message: err.Error()}
		e.Data = nil
	}
	_ = json.NewEncoder(w).Encode(e)
}

var agentxNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

// AgentxConnect enregistre une session X dans agentx (cookies passés par
// l'environnement, jamais en argument) puis renvoie le compte connecté.
func AgentxConnect(ctx context.Context, name, authToken, ct0 string) (*Profile, error) {
	if !agentxNameRe.MatchString(name) {
		return nil, errors.New("nom de session invalide (lettres, chiffres, - et _)")
	}
	if len(authToken) < 20 || len(ct0) < 20 || strings.ContainsAny(authToken+ct0, " \n\t;") {
		return nil, errors.New("auth_token ou ct0 invalide : copiez la valeur exacte des cookies x.com")
	}
	be := &agentxBackend{bin: agentxBin()}
	env := []string{"TWITTER_AUTH_TOKEN=" + authToken, "TWITTER_CT0=" + ct0}
	if err := be.runEnv(ctx, nil, env, "account", "add", "--name", name); err != nil {
		return nil, err
	}
	be.account = name
	p, err := be.Me(ctx)
	if err != nil {
		return nil, fmt.Errorf("session enregistrée mais X la refuse : %w", err)
	}
	return p, nil
}
