package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestManualCorrectionBecomesFeedback(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ps, err := e.st.GenerateDrafts(ctx, "main", "sujet", "", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	p := ps[0]
	if p.AIText != "Premier brouillon" {
		t.Fatalf("le texte de l'IA doit être gardé : %q", p.AIText)
	}
	edits := func() []Learning {
		var out []Learning
		for _, l := range e.st.store.Snapshot().Learnings {
			if l.Source == FeedbackEdit {
				out = append(out, l)
			}
		}
		return out
	}
	if _, err := e.st.EditPost(p.ID, "Premier brouillon, plus direct"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.EditPost(p.ID, "Brouillon direct"); err != nil {
		t.Fatal(err)
	}
	ls := edits()
	if len(ls) != 1 || ls[0].Before != "Premier brouillon" || ls[0].After != "Brouillon direct" || ls[0].PostID != p.ID || ls[0].Account != "main" {
		t.Fatalf("une seule correction par post, avec avant et après : %+v", ls)
	}

	// la correction guide la rédaction suivante
	if _, err := e.st.GenerateDrafts(ctx, "main", "autre", "", 1, ""); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "prompt", e.ai.lastSys, "[Correction] l'IA avait écrit « Premier brouillon » ; le propriétaire l'a corrigé en « Brouillon direct »")

	// catégorie, puis « ne pas apprendre »
	cat := "ton"
	if _, err := e.st.UpdateLearning(ls[0].ID, &cat, nil); err != nil {
		t.Fatal(err)
	}
	_, _ = e.st.GenerateDrafts(ctx, "main", "autre", "", 1, "")
	mustContain(t, "catégorie", e.ai.lastSys, "[Correction · Ton / voix]")
	yes := true
	if _, err := e.st.UpdateLearning(ls[0].ID, nil, &yes); err != nil {
		t.Fatal(err)
	}
	_, _ = e.st.GenerateDrafts(ctx, "main", "autre", "", 1, "")
	if strings.Contains(e.ai.lastSys, "Brouillon direct") {
		t.Fatal("un retour « ne pas apprendre » ne doit plus guider l'IA")
	}
	bad := "inconnue"
	if _, err := e.st.UpdateLearning(ls[0].ID, &bad, nil); err == nil {
		t.Fatal("catégorie inconnue acceptée")
	}

	// revenir au texte de l'IA retire la correction ; un brouillon écrit à la main n'en crée pas
	p2 := ps[0]
	_, _ = e.st.GenerateDrafts(ctx, "main", "x", "", 1, "")
	all := e.st.store.Snapshot().Posts
	p2 = all[len(all)-1]
	_, _ = e.st.EditPost(p2.ID, "modifié")
	_, _ = e.st.EditPost(p2.ID, p2.AIText)
	if n := len(edits()); n != 1 {
		t.Fatalf("revenir au texte de l'IA doit retirer la correction (%d)", n)
	}
	m := e.draft(t, "écrit à la main")
	_, _ = e.st.EditPost(m.ID, "écrit à la main, retouché")
	if n := len(edits()); n != 1 {
		t.Fatal("un texte qui ne vient pas de l'IA n'apprend rien")
	}

	// la synthèse lit les corrections et ignore les retours « ne pas apprendre »
	_, _ = e.st.EditPost(p2.ID, "corrigé à la main")
	_ = e.st.store.Update(func(d *Data) error {
		d.Learnings = append(d.Learnings, Learning{ID: "l_x", Account: "main", Text: "plus court", Source: FeedbackManual, CreatedAt: e.now})
		return nil
	})
	if _, err := e.st.Synthesize(ctx, ""); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "synthèse", e.ai.lastMsg, "le propriétaire l'a corrigé en « corrigé à la main »", "[Leçon] plus court")
	if strings.Contains(e.ai.lastMsg, "Brouillon direct") {
		t.Fatal("un retour « ne pas apprendre » ne doit pas nourrir la synthèse")
	}
	// les sources des retours sont enregistrées
	p3 := e.draft(t, "à rejeter")
	_, _ = e.st.Reject(p3.ID, "hors sujet")
	_, _ = e.st.RewritePost(ctx, all[0].ID, "plus court")
	var reject, rewrite bool
	for _, l := range e.st.store.Snapshot().Learnings {
		reject = reject || (l.Source == FeedbackReject && l.PostID == p3.ID)
		rewrite = rewrite || (l.Source == FeedbackRewrite && l.PostID == all[0].ID)
	}
	if !reject || !rewrite {
		t.Fatal("rejets et réécritures doivent garder leur source et leur post")
	}
}

func TestPlaceholdersBlockValidation(t *testing.T) {
	e := newEnv(t)
	for _, text := range []string{
		"Le jour où [À COMPLÉTER : situation réelle vécue côté créateur], j'ai compris.",
		"On a gagné [a completer: chiffre réel] clients.",
	} {
		p := e.draft(t, text)
		if _, err := e.st.Validate(p.ID, "Allan", nil, true); err == nil || !strings.Contains(err.Error(), "repère") {
			t.Fatalf("un repère doit bloquer la validation : %v", err)
		}
	}
	if got := Placeholders("a [À COMPLÉTER : x] b [A COMPLETER] c"); len(got) != 2 {
		t.Fatalf("repères trouvés : %q", got)
	}
	if _, err := e.st.GenerateDrafts(context.Background(), "main", "x", "", 1, ""); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "prompt", e.ai.lastSys, "[À COMPLÉTER : situation réelle vécue côté créateur]")
}

func TestProfileShapesTheVoice(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, _ = e.st.GenerateDrafts(ctx, "main", "x", "", 1, "")
	mustContain(t, "défaut", e.ai.lastSys, "première personne du singulier (je)")

	acc := e.st.store.Snapshot().Account("main")
	acc.Role, acc.DisplayName = "marque", "Acme Studio"
	if err := e.st.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	_, _ = e.st.GenerateDrafts(ctx, "main", "x", "", 1, "")
	mustContain(t, "marque", e.ai.lastSys, "« nous »", "Nom affiché du compte : Acme Studio.")

	acc.Role = "automatise"
	if err := e.st.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	if !e.st.store.Snapshot().Account("main").Automated {
		t.Fatal("un compte au rôle automatisé est ouvertement automatisé")
	}
	_, _ = e.st.GenerateDrafts(ctx, "main", "x", "", 1, "")
	mustContain(t, "automatisé", e.ai.lastSys, "ne prétends jamais être un humain")

	acc.Role = "robot"
	if err := e.st.SaveAccount(acc); err == nil {
		t.Fatal("rôle inconnu accepté")
	}
}

func TestAIPerAccount(t *testing.T) {
	e := newEnv(t)
	var used []Settings
	e.st.ai = func(st Settings) AI { used = append(used, st); return e.ai }
	_ = e.st.store.Update(func(d *Data) error { d.Settings.AIProvider, d.Settings.Model = "claude-code", "sonnet"; return nil })
	if err := e.st.SaveAccount(&Account{Name: "autre", Handle: "autre", Backend: "dryrun", AIProvider: "codex", AIModel: "gpt-x"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, _ = e.st.GenerateDrafts(ctx, "main", "x", "", 1, "")
	_, _ = e.st.GenerateDrafts(ctx, "autre", "x", "", 1, "")
	if len(used) != 2 || used[0].AIProvider != "claude-code" || used[0].Model != "sonnet" || used[1].AIProvider != "codex" || used[1].Model != "gpt-x" {
		t.Fatalf("IA utilisées : %+v", used)
	}
	if err := e.st.SaveAccount(&Account{Name: "faux", Backend: "dryrun", AIProvider: "inconnu"}); err == nil {
		t.Fatal("fournisseur inconnu accepté")
	}
}

func TestIdeaBank(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.st.SuggestIdeas(ctx, "main"); err == nil {
		t.Fatal("banque d'idées désactivée : erreur attendue")
	}
	acc := e.st.store.Snapshot().Account("main")
	acc.Ideas = IdeaSettings{Enabled: true, Themes: []string{"agents IA\naudit"}, PerRequest: 2}
	acc.Prompts.Ideas = "Une idée part d'un vrai problème client."
	if err := e.st.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	ideas, err := e.st.SuggestIdeas(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(ideas) != 2 || ideas[0].Status != IdeaProposed || ideas[0].Angle != "angle A" {
		t.Fatalf("idées : %+v", ideas)
	}
	mustContain(t, "suggestion", e.ai.lastMsg, "Propose 2 idées", "<sujets>\nagents IA\naudit", "Une idée part d'un vrai problème client.")

	if _, err := e.st.DecideIdea(ideas[1].ID, "dismiss"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.DecideIdea(ideas[0].ID, "keep"); err != nil {
		t.Fatal(err)
	}
	_, _ = e.st.SuggestIdeas(ctx, "main")
	mustContain(t, "seconde suggestion", e.ai.lastMsg, "Idées écartées par le propriétaire", "- Idée B", "Idées déjà dans la banque")

	p, err := e.st.DraftIdea(ctx, ideas[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, "rédaction", e.ai.lastMsg, "Angle : angle A", DefaultPrompts.IdeaWrite)
	d := e.st.store.Snapshot()
	if p.IdeaID != ideas[0].ID || p.AIText == "" || d.Idea(ideas[0].ID).Status != IdeaDrafted || d.Idea(ideas[0].ID).PostID != p.ID || d.Post(p.ID) == nil {
		t.Fatalf("idée rédigée : %+v %+v", p, d.Idea(ideas[0].ID))
	}
	if _, err := e.st.DraftIdea(ctx, ideas[1].ID); err == nil {
		t.Fatal("une idée écartée ne se rédige pas")
	}

	// rédaction directe
	acc = e.st.store.Snapshot().Account("main")
	acc.Ideas.AutoDraft = true
	_ = e.st.SaveAccount(acc)
	ideas, err = e.st.SuggestIdeas(ctx, "main")
	if err != nil || ideas[0].Status != IdeaDrafted || ideas[0].PostID == "" {
		t.Fatalf("rédaction directe : %+v %v", ideas, err)
	}
}

func TestWindowSchedulingIsHuman(t *testing.T) {
	e := newEnv(t)
	old := randIntn
	randIntn = func(int) int { return 0 } // tirage : toujours la borne basse
	t.Cleanup(func() { randIntn = old })
	acc := e.st.store.Snapshot().Account("main")
	acc.Timing = Timing{Mode: "window"}
	acc.DailyLimit = 2
	if err := e.st.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	if a := e.st.store.Snapshot().Account("main"); a.Timing.Start != "08:00" || a.Timing.End != "22:00" || a.Timing.GapMin != 30 || a.Timing.GapMax != 60 {
		t.Fatalf("valeurs par défaut de la plage : %+v", a.Timing)
	}
	day := func(h, m int) time.Time { return time.Date(2026, 11, 18, h, m, 0, 0, time.UTC) }
	validate := func(at time.Time) time.Time {
		t.Helper()
		e.now = at
		p := e.draft(t, "post "+at.Format("15:04:05.000")+newID(""))
		v, err := e.st.Validate(p.ID, "Allan", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		return *v.ScheduledAt
	}
	// aucun post aujourd'hui et on est dans la plage : tout de suite
	if got := validate(day(10, 0)); !got.Equal(day(10, 1)) {
		t.Fatalf("premier post : %v", got)
	}
	// ensuite : après le dernier post du jour, écart tiré entre 30 et 60 min
	if got := validate(day(10, 5)); !got.Equal(day(10, 31)) {
		t.Fatalf("second post : %v", got)
	}
	// limite de 2 posts : le lendemain, début de plage ± aléa (borne basse -15 → +15 min, la plage est respectée)
	if got := validate(day(10, 10)); !got.Equal(day(8, 15).AddDate(0, 0, 1)) {
		t.Fatalf("lendemain : %v", got)
	}

	// avant la plage : au début de la plage à ± l'aléa
	e2 := newEnv(t)
	acc = e2.st.store.Snapshot().Account("main")
	zero := 0
	acc.Timing = Timing{Mode: "window", Start: "09:00", End: "21:00", GapMin: 20, GapMax: 40, Jitter: &zero}
	_ = e2.st.SaveAccount(acc)
	e2.now = day(6, 0)
	p := e2.draft(t, "tôt")
	v, err := e2.st.Validate(p.ID, "Allan", nil, false)
	if err != nil || !v.ScheduledAt.Equal(day(9, 0)) {
		t.Fatalf("avant la plage : %v %v", v.ScheduledAt, err)
	}
	// après la plage : le lendemain
	e2.now = day(22, 30)
	p = e2.draft(t, "tard")
	v, _ = e2.st.Validate(p.ID, "Allan", nil, false)
	if !v.ScheduledAt.Equal(day(9, 20).AddDate(0, 0, 1)) && !v.ScheduledAt.Equal(day(9, 0).AddDate(0, 0, 1)) {
		t.Fatalf("après la plage : %v", v.ScheduledAt)
	}

	for _, bad := range []Timing{{Mode: "window", Start: "22:00", End: "08:00"}, {Mode: "window", GapMin: 50, GapMax: 20}, {Mode: "hasard"}} {
		x := *acc
		x.Timing = bad
		if err := e2.st.SaveAccount(&x); err == nil {
			t.Errorf("programmation invalide acceptée : %+v", bad)
		}
	}
}

func TestAPIWritingFeedbackAndIdeas(t *testing.T) {
	srv, e := tuningServer(t)
	c := &http.Client{}
	ps, _ := e.st.GenerateDrafts(context.Background(), "main", "x", "", 1, "")
	call(t, c, "POST", srv.URL+"/api/posts/"+ps[0].ID+"/edit", `{"text":"corrigé"}`)
	var id string
	for _, l := range e.st.store.Snapshot().Learnings {
		if l.Source == FeedbackEdit {
			id = l.ID
		}
	}
	if id == "" {
		t.Fatal("la correction faite dans le studio doit devenir un retour")
	}
	if st, m := call(t, c, "POST", srv.URL+"/api/learnings/"+id, `{"category":"longueur","ignored":true}`); st != 200 {
		t.Fatalf("mise à jour du retour : %v", m)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/learnings/"+id, `{"category":"nimporte"}`); st != 400 {
		t.Fatal("catégorie inconnue acceptée")
	}

	acc := e.st.store.Snapshot().Account("main")
	acc.Ideas.Enabled = true
	_ = e.st.SaveAccount(acc)
	st, m := call(t, c, "POST", srv.URL+"/api/ideas/suggest", `{"account":"main"}`)
	if st != 200 {
		t.Fatalf("suggestion : %v", m)
	}
	ideaID := m["data"].([]any)[0].(map[string]any)["id"].(string)
	if st, _ := call(t, c, "POST", srv.URL+"/api/ideas/"+ideaID+"/keep", `{}`); st != 200 {
		t.Fatal("retenir une idée refusé")
	}
	if st, m := call(t, c, "POST", srv.URL+"/api/ideas/"+ideaID+"/draft", `{}`); st != 200 {
		t.Fatalf("rédaction de l'idée refusée : %v", m)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/ideas/"+ideaID+"/jeter", `{}`); st != 400 {
		t.Fatal("action inconnue acceptée")
	}
	_, m = call(t, c, "GET", srv.URL+"/api/state", "")
	data := m["data"].(map[string]any)
	if len(data["ideas"].([]any)) != 3 || data["feedbackCategories"].(map[string]any)["fait"] != "Erreur factuelle" {
		t.Fatalf("état : ideas=%d", len(data["ideas"].([]any)))
	}
	learned := data["learnings"].([]any)
	last := learned[len(learned)-1].(map[string]any)
	if last["source"] != "edit" || last["ignored"] != true || last["category"] != "longueur" || last["before"] == nil {
		t.Fatalf("retour exposé : %v", last)
	}
}

func TestCLIWritingFlags(t *testing.T) {
	var acc Account
	applyWritingFlags(&acc, parseArgs([]string{"account", "set", "main", "--role", "marque", "--automated", "--ai-provider", "codex",
		"--ideas-enabled", "--idea-themes", "a\nb", "--ideas-per-request", "3", "--schedule", "window", "--window", "09:00-21:30", "--gap", "20-45", "--jitter", "10"}))
	tm := acc.Timing
	if acc.Role != "marque" || !acc.Automated || acc.AIProvider != "codex" || !acc.Ideas.Enabled || len(acc.Ideas.Themes) != 2 || acc.Ideas.PerRequest != 3 ||
		tm.Mode != "window" || tm.Start != "09:00" || tm.End != "21:30" || tm.GapMin != 20 || tm.GapMax != 45 || tm.Jitter == nil || *tm.Jitter != 10 {
		t.Fatalf("options : %+v", acc)
	}
}
