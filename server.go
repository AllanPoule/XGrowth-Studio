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
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
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
	registerLinkedIn(mux, st, gate)

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
		mix := map[string]TypeMix{}
		for _, a := range d.Accounts {
			if types := PostTypesFor(a, d.Settings); len(types) > 0 {
				mix[a.Name] = ComputeTypeMix(d, a.Name, types, time.Now())
			}
		}
		// résumé des performances par compte : calcul seul, sans appel IA
		perf := map[string]PerfDigest{}
		for _, a := range d.Accounts {
			perf[a.Name] = ComputePerfDigest(d, a.Name, st.now())
		}
		// Les listes vides partent en [] et jamais en null : l'interface les
		// parcourt sans test (un studio sans aucun post plantait à l'affichage).
		reply(w, map[string]any{
			"accounts": orEmpty(d.Accounts), "posts": orEmpty(d.Posts), "items": orEmpty(items), "settings": d.Settings,
			"learnings": orEmpty(d.Learnings), "rules": orEmpty(sortedRules(d.RuleProposals)), "typeMix": mix, "perf": perf, "ideas": orEmpty(d.Ideas),
			"feedbackCategories": FeedbackCategories, "mediaLimits": mediaLimitsByAccount(d),
			"campaigns": orEmpty(d.Campaigns), "candidates": orEmpty(sortedCandidates(d.Candidates, 300)), "campaignStats": campaignStats(d, time.Now()),
			"commentsDefault": DefaultCommentsPrompt,
			"defaults":        map[string]any{"prompts": DefaultPrompts, "postTypes": DefaultPostTypes, "linkedin": map[string]any{"maxChars": linkedinMaxChars, "lines": linkedinLines}},
			"linkedin":        map[string]any{"configured": st.liCfg().configured(), "accounts": st.linkedinStates(d)},
			"log":             orEmpty(logs), "calendar": orEmpty(calendar(d, 21, time.Now())),
			"ai": map[string]string{"provider": ai.AIProvider, "model": ai.Model}, "version": version,
		}, nil)
	})
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		d := st.store.Snapshot()
		f := StatsFilter{Account: r.URL.Query().Get("account")}
		if f.Account != "" && d.Account(f.Account) == nil {
			reply(w, nil, errors.New("compte inconnu"))
			return
		}
		if v := r.URL.Query().Get("days"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 3650 {
				reply(w, nil, errors.New("période invalide (nombre de jours, 0 = tout)"))
				return
			}
			f.Days = n
		}
		reply(w, ComputeStats(d, f, st.now()), nil)
	})
	mux.HandleFunc("GET /api/sources", func(w http.ResponseWriter, r *http.Request) {
		reply(w, st.sourcesStatus(), nil)
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
		return st.GenerateDrafts(r.Context(), s(b, "account"), s(b, "topic"), s(b, "item"), n(b, "n", 3), s(b, "type"))
	})
	post("/api/reply", func(r *http.Request, b map[string]any) (any, error) {
		return st.DraftReply(r.Context(), s(b, "account"), s(b, "ref"), bl(b, "quote"))
	})
	post("/api/reply-draft", func(r *http.Request, b map[string]any) (any, error) {
		// LinkedIn : brouillon de réponse ou de commentaire à partir du texte collé ; rien n'est envoyé
		return st.DraftLinkedInReply(r.Context(), s(b, "account"), s(b, "target"), s(b, "url"), bl(b, "comment"))
	})
	post("/api/posts/{id}/handled", func(r *http.Request, b map[string]any) (any, error) {
		return st.MarkHandled(r.PathValue("id"))
	})
	post("/api/write", func(r *http.Request, b map[string]any) (any, error) {
		return st.AddDraft(s(b, "account"), s(b, "text"), s(b, "replyTo"), s(b, "quoteOf"), s(b, "media"))
	})
	post("/api/posts/{id}/edit", func(r *http.Request, b map[string]any) (any, error) {
		o := EditOptions{Clear: bl(b, "clear")}
		if v, ok := b["version"].(float64); ok {
			n := int(v)
			o.ExpectedVersion = &n
		}
		return st.EditPostOpts(r.PathValue("id"), s(b, "text"), o)
	})
	post("/api/posts/{id}/restore", func(r *http.Request, b map[string]any) (any, error) {
		v, ok := b["index"].(float64)
		if !ok {
			return nil, errors.New("version à restaurer manquante")
		}
		return st.RestorePost(r.PathValue("id"), int(v))
	})
	post("/api/posts/{id}/media", func(r *http.Request, b map[string]any) (any, error) {
		id := r.PathValue("id")
		if _, ok := b["path"]; ok || b["alt"] == nil {
			if _, err := st.SetMedia(id, s(b, "path")); err != nil {
				return nil, err
			}
		}
		if _, ok := b["alt"]; ok {
			return st.SetMediaAlt(id, s(b, "alt"))
		}
		return st.store.Snapshot().Post(id), nil
	})
	// Image déposée depuis le navigateur : le corps de la requête EST l'image
	// (aucun nom ni chemin du client) ; le contenu est vérifié par le serveur.
	mux.HandleFunc("POST /api/posts/{id}/media-file", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-XGrowth") != "1" {
			http.Error(w, "en-tête X-XGrowth manquant", http.StatusForbidden)
			return
		}
		p, err := st.AttachMedia(r.PathValue("id"), http.MaxBytesReader(w, r.Body, mediaUploadCeiling+1))
		reply(w, p, err)
	})
	mux.HandleFunc("GET /api/posts/{id}/media-file", func(w http.ResponseWriter, r *http.Request) {
		path, ext, ok := st.MediaFileOf(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", map[string]string{".jpg": "image/jpeg", ".png": "image/png", ".gif": "image/gif"}[ext])
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("Cache-Control", "private, no-cache")
		_, _ = io.Copy(w, f)
	})
	// ---- LinkedIn : méthode (aides sur demande, aucune n'écrit seule) et contrôles
	post("/api/posts/{id}/tics", func(r *http.Request, b map[string]any) (any, error) {
		p, err := st.CheckTics(r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"post": p, "tics": orEmpty(p.Tics), "needsAck": len(highTics(p.Tics)) > 0}, nil
	})
	post("/api/posts/{id}/line", func(r *http.Request, b map[string]any) (any, error) {
		return st.SetLine(r.PathValue("id"), s(b, "line"))
	})
	post("/api/posts/{id}/hooks", func(r *http.Request, b map[string]any) (any, error) {
		hooks, err := st.SuggestHooks(r.Context(), r.PathValue("id"))
		return map[string]any{"hooks": orEmpty(hooks)}, err
	})
	post("/api/posts/{id}/hooks/apply", func(r *http.Request, b map[string]any) (any, error) {
		return st.ApplyHook(r.PathValue("id"), s(b, "hook"))
	})
	post("/api/posts/{id}/humanize", func(r *http.Request, b map[string]any) (any, error) {
		return st.Humanize(r.Context(), r.PathValue("id"))
	})
	post("/api/posts/{id}/humanize/apply", func(r *http.Request, b map[string]any) (any, error) {
		return st.ApplyHumanized(r.PathValue("id"), s(b, "text"))
	})
	post("/api/posts/{id}/audit", func(r *http.Request, b map[string]any) (any, error) {
		return st.Audit(r.Context(), r.PathValue("id"))
	})
	post("/api/accounts/{name}/plan-week", func(r *http.Request, b map[string]any) (any, error) {
		return st.PlanWeek(r.Context(), r.PathValue("name"))
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
		return st.ValidateWith(r.PathValue("id"), s(b, "by"), at, bl(b, "now"), ValidateOptions{AckTics: bl(b, "ackTics")})
	})
	post("/api/posts/{id}/mark-published", func(r *http.Request, b map[string]any) (any, error) {
		return st.MarkPublished(r.PathValue("id"), s(b, "url"))
	})
	post("/api/posts/{id}/manual", func(r *http.Request, b map[string]any) (any, error) {
		return st.SendToManual(r.PathValue("id"))
	})
	post("/api/posts/{id}/reject", func(r *http.Request, b map[string]any) (any, error) {
		return st.Reject(r.PathValue("id"), s(b, "reason"))
	})
	post("/api/posts/{id}/schedule", func(r *http.Request, b map[string]any) (any, error) {
		return st.ScheduleDraft(r.PathValue("id"), s(b, "by"), s(b, "before"))
	})
	post("/api/posts/{id}/move", func(r *http.Request, b map[string]any) (any, error) {
		return st.MoveToDay(r.PathValue("id"), s(b, "day"))
	})
	post("/api/schedule/reorder", func(r *http.Request, b map[string]any) (any, error) {
		var ids []string
		if list, ok := b["ids"].([]any); ok {
			for _, x := range list {
				if v, ok := x.(string); ok {
					ids = append(ids, v)
				}
			}
		}
		return st.ReorderQueue(ids)
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
	post("/api/watch/seen", func(r *http.Request, b map[string]any) (any, error) {
		now := time.Now().UTC()
		return map[string]any{"watchSeenAt": now}, st.store.Update(func(d *Data) error {
			d.Settings.WatchSeenAt = &now
			return nil
		})
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
		return nil, st.store.Update(func(d *Data) error { return d.RemoveAccount(name) })
	})
	post("/api/watch", func(r *http.Request, b map[string]any) (any, error) {
		return st.AddWatch(s(b, "account"), s(b, "kind"), s(b, "value"), n(b, "every", 60))
	})
	post("/api/accounts/{name}/sources", func(r *http.Request, b map[string]any) (any, error) {
		raw, _ := json.Marshal(b["sources"])
		var src SourceSettings
		if err := json.Unmarshal(raw, &src); err != nil {
			return nil, err
		}
		var primary []string
		if list, ok := b["primary"].([]any); ok {
			primary = []string{}
			for _, x := range list {
				if v, ok := x.(string); ok {
					primary = append(primary, v)
				}
			}
		}
		return st.SaveSources(r.PathValue("name"), src, primary)
	})
	post("/api/sources/poll", func(r *http.Request, b map[string]any) (any, error) {
		return st.PollSources(r.Context(), s(b, "account"), true), nil
	})
	post("/api/news/draft", func(r *http.Request, b map[string]any) (any, error) {
		return st.DraftNews(r.Context()), nil
	})
	post("/api/watch/{id}/remove", func(r *http.Request, b map[string]any) (any, error) {
		return nil, st.RemoveWatch(r.PathValue("id"))
	})
	post("/api/settings", func(r *http.Request, b map[string]any) (any, error) {
		var out Settings
		err := st.store.Update(func(d *Data) error {
			cur := d.Settings
			// une liste envoyée remplace l'ancienne (json fusionnerait ses éléments)
			for k, f := range map[string]func(){"rules": func() { cur.Rules = nil }, "postTypes": func() { cur.PostTypes = nil },
				"competitors": func() { cur.Competitors = nil }, "bannedPhrases": func() { cur.BannedPhrases = nil }} {
				if _, ok := b[k]; ok {
					f()
				}
			}
			raw, _ := json.Marshal(b)
			// les dates tenues par le serveur sont des pointeurs partagés avec
			// d.Settings : sans cette remise à zéro, Unmarshal écrirait la date
			// envoyée dans la valeur enregistrée elle-même
			// (même piège pour la table PerfState : Unmarshal remplirait la
			// table enregistrée elle-même)
			cur.LastSynthesisAt, cur.LastPerfAnalysisAt, cur.PerfState = nil, nil, nil
			// fusion : seuls les champs envoyés changent
			if err := json.Unmarshal(raw, &cur); err != nil {
				return err
			}
			cur.Onboarded, cur.LastSynthesisAt = d.Settings.Onboarded, d.Settings.LastSynthesisAt
			cur.LastPerfAnalysisAt, cur.PerfState = d.Settings.LastPerfAnalysisAt, d.Settings.PerfState
			if err := NormalizeSettings(&cur); err != nil {
				return err
			}
			d.Settings, out = cur, cur
			return nil
		})
		return out, err
	})
	post("/api/learnings", func(r *http.Request, b map[string]any) (any, error) {
		if s(b, "text") == "" {
			return nil, errors.New("leçon vide")
		}
		l := Learning{ID: newID("l"), Account: s(b, "account"), Text: s(b, "text"), Source: FeedbackManual, CreatedAt: time.Now().UTC()}
		return l, st.store.Update(func(d *Data) error { d.Learnings = append(d.Learnings, l); return nil })
	})
	// ---- campagnes de réponses
	post("/api/campaigns", func(r *http.Request, b map[string]any) (any, error) {
		raw, _ := json.Marshal(b)
		var c Campaign
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		return st.SaveCampaign(&c)
	})
	post("/api/campaigns/{id}/remove", func(r *http.Request, b map[string]any) (any, error) {
		return nil, st.RemoveCampaign(r.PathValue("id"))
	})
	post("/api/campaigns/{id}/run", func(r *http.Request, b map[string]any) (any, error) {
		res := st.RunCampaign(r.Context(), r.PathValue("id"), true)
		if res.Error != "" && res.Searches == 0 && res.Found == 0 && res.Qualified == 0 {
			return nil, errors.New(res.Error)
		}
		return res, nil
	})

	// ---- règles apprises (synthèse des retours)
	post("/api/rules/synthesize", func(r *http.Request, b map[string]any) (any, error) {
		return st.Synthesize(r.Context(), s(b, "account"))
	})
	// ---- règles apprises (analyse des performances)
	post("/api/rules/analyze", func(r *http.Request, b map[string]any) (any, error) {
		return st.AnalyzePerformance(r.Context(), s(b, "account"))
	})
	post("/api/rules/{id}/{decision}", func(r *http.Request, b map[string]any) (any, error) {
		return st.DecideRule(r.PathValue("id"), r.PathValue("decision"), s(b, "text"))
	})
	post("/api/learnings/{id}", func(r *http.Request, b map[string]any) (any, error) {
		var cat *string
		var ign *bool
		if v, ok := b["category"].(string); ok {
			cat = &v
		}
		if v, ok := b["ignored"].(bool); ok {
			ign = &v
		}
		return st.UpdateLearning(r.PathValue("id"), cat, ign)
	})

	// ---- banque d'idées
	post("/api/ideas/suggest", func(r *http.Request, b map[string]any) (any, error) {
		return st.SuggestIdeas(r.Context(), s(b, "account"))
	})
	post("/api/ideas/{id}/{action}", func(r *http.Request, b map[string]any) (any, error) {
		if a := r.PathValue("action"); a == "draft" {
			return st.DraftIdea(r.Context(), r.PathValue("id"))
		} else {
			return st.DecideIdea(r.PathValue("id"), a)
		}
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

// orEmpty remplace une liste nil par une liste vide (sérialisée en [] et non null).
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func mediaLimitsByAccount(d *Data) map[string]MediaLimits {
	out := map[string]MediaLimits{}
	for i := range d.Accounts {
		out[d.Accounts[i].Name] = mediaLimitsFor(d.Accounts[i])
	}
	return out
}
