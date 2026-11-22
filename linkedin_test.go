package main

// Lot 1 LinkedIn : plateforme, limites, prompts, backend manuel. Aucun test ne
// touche le réseau : backends manuel, simulation ou doublures qui comptent leurs appels.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liEnv : un studio avec un compte X (« main ») et un compte LinkedIn (« pro »).
// backend choisit le backend du compte LinkedIn ; real = vrais backends
// (manual/dryrun) au lieu de la doublure X comptée.
func liEnv(t *testing.T, backend string, real bool) (*env, *int) {
	t.Helper()
	e := newEnv(t)
	calls := 0
	if real {
		e.st.backendFor = BackendFor
	} else {
		e.st.backendFor = func(*Account) (XBackend, error) { return countX{e.x, &calls}, nil }
	}
	if err := e.st.SaveAccount(&Account{Name: "pro", Platform: "linkedin", Line: "perso", Backend: backend,
		Timezone: "UTC", Slots: []string{"09:00"}}); err != nil {
		t.Fatal(err)
	}
	return e, &calls
}

func (e *env) liDraft(t *testing.T, text string) *Post {
	t.Helper()
	p, err := e.st.AddDraft("pro", text, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A1 : SaveAccount, plateforme × backend × ligne.
func TestLinkedInSaveAccountTable(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name, platform, line, backend string
		ok                            bool
	}{
		{"li-manual", "linkedin", "perso", "manual", true},
		{"li-dry", "linkedin", "kinflux", "dryrun", true},
		{"li-defaut", "linkedin", "allan-drdrt", "", true},
		{"li-sans-ligne", "linkedin", "", "manual", false},
		{"li-ligne-inconnue", "linkedin", "pro", "manual", false},
		{"li-api-pas-branche", "linkedin", "perso", "linkedin", false},
		{"li-backend-x", "linkedin", "perso", "agentx", false},
		{"x-ok", "x", "", "dryrun", true},
		{"x-vide", "", "", "xapi", true},
		{"x-avec-ligne", "x", "perso", "dryrun", false},
		{"x-avec-ligne-vide-platform", "", "perso", "dryrun", false},
		{"x-manual", "x", "", "manual", false},
		{"x-linkedin", "", "", "linkedin", false},
		{"plateforme-inconnue", "facebook", "", "dryrun", false},
	}
	for _, c := range cases {
		err := e.st.SaveAccount(&Account{Name: c.name, Platform: c.platform, Line: c.line, Backend: c.backend})
		if (err == nil) != c.ok {
			t.Errorf("%s : err=%v, attendu ok=%v", c.name, err, c.ok)
		}
	}
	a := e.st.store.Snapshot().Account("li-defaut")
	if a == nil || a.Backend != "dryrun" || a.DailyLimit != 0 {
		t.Errorf("un compte LinkedIn n'a ni backend vide ni plafond quotidien imposé : %+v", a)
	}
	if err := e.st.SaveAccount(&Account{Name: "premium", Platform: "linkedin", Line: "perso", Premium: true}); err != nil {
		t.Fatal(err)
	}
	if e.st.store.Snapshot().Account("premium").Premium {
		t.Error("Premium n'a pas de sens sur LinkedIn")
	}
}

// A1 (CLI) : account add --platform linkedin --line perso --backend manual.
func TestLinkedInAccountFlags(t *testing.T) {
	e := newEnv(t)
	a := parseArgs([]string{"add", "pro", "--platform", "linkedin", "--line", "perso", "--backend", "manual"})
	acc := &Account{Name: a.arg(1)}
	applyAccountFlags(acc, a)
	if err := e.st.SaveAccount(acc); err != nil || acc.Platform != "linkedin" || acc.Line != "perso" || acc.Backend != "manual" {
		t.Fatalf("%+v %v", acc, err)
	}
	bad := &Account{Name: "x2"}
	applyAccountFlags(bad, parseArgs([]string{"add", "x2", "--platform", "x", "--line", "perso"}))
	if err := e.st.SaveAccount(bad); err == nil {
		t.Fatal("--platform x --line perso doit échouer")
	}
}

// A2 : 3000 passe, 3001 est refusé ; X reste à 280/4000.
func TestLinkedInGuardLength(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	d := e.st.store.Snapshot()
	li, x := d.Account("pro"), d.Account("main")
	if err := Guard(d, li, strings.Repeat("é", 3000)); err != nil {
		t.Errorf("3000 caractères doivent passer : %v", err)
	}
	if err := Guard(d, li, strings.Repeat("é", 3001)); err == nil {
		t.Error("3001 caractères doivent être refusés")
	}
	if maxLen(li) != linkedinMaxChars || linkedinMaxChars != 3000 {
		t.Errorf("maxLen LinkedIn = %d", maxLen(li))
	}
	if maxLen(x) != 280 {
		t.Errorf("X = %d", maxLen(x))
	}
	x.Premium = true
	if maxLen(x) != 4000 {
		t.Errorf("X Premium = %d", maxLen(x))
	}
	// Guard refuse un post LinkedIn sans ligne
	li.Line = ""
	if err := Guard(d, li, "bonjour"); err == nil {
		t.Error("un compte LinkedIn sans ligne ne peut rien valider")
	}
	// les garde-fous X s'appliquent aussi (concurrents)
	d.Settings.Competitors = []string{"Acme"}
	li.Line = "perso"
	if err := Guard(d, li, "Mieux qu'Acme"); err == nil {
		t.Error("un concurrent reste interdit sur LinkedIn")
	}
}

// A3 : prompt système LinkedIn.
func TestLinkedInSystemPrompt(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	d := e.st.store.Snapshot()
	li, x := d.Account("pro"), d.Account("main")
	li.Handle = "@handle_x_d_allan"
	for _, length := range []string{"", "court", "moyen", "long"} {
		li.Length = length
		sp := systemPrompt(li, d)
		if !strings.Contains(sp, "LinkedIn") || !strings.Contains(sp, "ligne perso") || !strings.Contains(sp, "3000") {
			t.Errorf("longueur %q : le prompt LinkedIn doit citer LinkedIn, la ligne et 3000 :\n%s", length, sp)
		}
		for _, bad := range []string{"280", "X Premium", "compte X", "@handle", "handle_x_d_allan", "viennent de X"} {
			if strings.Contains(sp, bad) {
				t.Errorf("longueur %q : le prompt LinkedIn ne doit pas contenir %q", length, bad)
			}
		}
		if !strings.Contains(sp, "À COMPLÉTER") {
			t.Error("la règle « jamais de vécu inventé » doit rester active")
		}
	}
	li.Length = "moyen"
	if !strings.Contains(systemPrompt(li, d), "600 à 1 200") {
		t.Error("longueur moyenne LinkedIn : 600 à 1 200 caractères")
	}
	x.Handle = "@moi"
	sp := systemPrompt(x, d)
	if !strings.Contains(sp, "compte X @moi") || !strings.Contains(sp, "280") || strings.Contains(sp, "LinkedIn") {
		t.Errorf("le prompt X doit rester inchangé :\n%s", sp)
	}
}

// La ligne et le brouillon : générés par l'IA, ils restent des brouillons.
func TestLinkedInDraftsCarryLineAndStayDrafts(t *testing.T) {
	e, calls := liEnv(t, "manual", false)
	ps, err := e.st.GenerateDrafts(context.Background(), "pro", "un sujet", "", 2, "")
	if err != nil || len(ps) != 2 {
		t.Fatal(err, ps)
	}
	for _, p := range ps {
		if p.Line != "perso" || p.Status != StatusDraft || p.ValidatedBy != "" {
			t.Errorf("brouillon IA : %+v", p)
		}
	}
	// le compte change de ligne : l'historique garde l'étiquette
	a := e.st.store.Snapshot().Account("pro")
	a.Line = "kinflux"
	if err := e.st.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	if got := e.st.store.Snapshot().Post(ps[0].ID).Line; got != "perso" {
		t.Errorf("l'étiquette du post doit rester perso, obtenu %q", got)
	}
	// un brouillon X n'a pas d'étiquette
	if p := e.draft(t, "x"); p.Line != "" {
		t.Errorf("post X : ligne %q", p.Line)
	}
	if *calls != 0 {
		t.Error("aucun appel backend pour rédiger")
	}
}

func TestLinkedInRepliesAndQuotesRefused(t *testing.T) {
	e, calls := liEnv(t, "manual", false)
	if _, err := e.st.AddDraft("pro", "salut", "123", "", ""); err == nil {
		t.Error("une réponse LinkedIn n'existe pas dans ce lot")
	}
	if _, err := e.st.DraftReply(context.Background(), "pro", "123", false); err == nil {
		t.Error("DraftReply doit être refusé pour LinkedIn")
	}
	if *calls != 0 || e.ai.count() != 0 {
		t.Error("aucun appel attendu")
	}
}

// A4 : manual -> statut manual (ni published ni failed), puis mark-published.
func TestLinkedInManualBackendPublishDue(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	p := e.liDraft(t, "Un post pour LinkedIn")
	v, err := e.st.Validate(p.ID, "Allan", nil, false)
	if err != nil || v.Status != StatusScheduled {
		t.Fatal(err, v)
	}
	sched := *v.ScheduledAt
	e.now = sched.Add(time.Minute)
	res := e.st.PublishDue(context.Background())
	if len(res) != 1 || res[0].Status != StatusManual {
		t.Fatalf("résultat : %+v", res)
	}
	got := e.st.store.Snapshot().Post(p.ID)
	if got.Status != StatusManual || got.Error != "à publier à la main sur LinkedIn" || got.ScheduledAt == nil || !got.ScheduledAt.Equal(sched) {
		t.Fatalf("post : %+v", got)
	}
	if got.PublishedAt != nil || got.PostURN != "" {
		t.Error("rien n'est publié")
	}
	// un nouveau passage ne le touche pas
	if r := e.st.PublishDue(context.Background()); len(r) != 0 {
		t.Fatalf("un post manual n'est pas dû : %+v", r)
	}
	// mark-published
	pub, err := e.st.MarkPublished(p.ID, " https://www.linkedin.com/feed/update/urn:li:share:1 ")
	if err != nil || pub.Status != StatusPublished || pub.PublishedAt == nil || pub.PostURN != "https://www.linkedin.com/feed/update/urn:li:share:1" || pub.Error != "" {
		t.Fatalf("%+v %v", pub, err)
	}
	if _, err := e.st.MarkPublished(p.ID, ""); err == nil {
		t.Error("un post déjà publié ne se marque pas deux fois")
	}
	logs := e.st.store.Snapshot().Log
	if last := logs[len(logs)-1]; !strings.Contains(last.Message, "publié à la main") {
		t.Errorf("journal : %+v", last)
	}
}

func TestMarkPublishedWithoutURLAndGuards(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	draft := e.liDraft(t, "brouillon")
	if _, err := e.st.MarkPublished(draft.ID, ""); err == nil {
		t.Error("seul un post manual peut être marqué publié")
	}
	// un post « manual » forgé sans signature ne peut pas être marqué publié
	_ = e.st.store.Update(func(d *Data) error { d.Post(draft.ID).Status = StatusManual; return nil })
	if _, err := e.st.MarkPublished(draft.ID, ""); err == nil {
		t.Error("un post sans validation ne peut pas être marqué publié")
	}
	// sans url : PostURN reste vide
	p := e.liDraft(t, "autre post")
	_, _ = e.st.Validate(p.ID, "Allan", nil, true)
	e.st.PublishDue(context.Background())
	pub, err := e.st.MarkPublished(p.ID, "")
	if err != nil || pub.Status != StatusPublished || pub.PostURN != "" {
		t.Fatalf("%+v %v", pub, err)
	}
	// éditer un post manual le renvoie en brouillon, sans validation
	q := e.liDraft(t, "encore un")
	_, _ = e.st.Validate(q.ID, "Allan", nil, true)
	e.st.PublishDue(context.Background())
	ed, err := e.st.EditPost(q.ID, "corrigé")
	if err != nil || ed.Status != StatusDraft || ed.ValidatedBy != "" {
		t.Fatalf("%+v %v", ed, err)
	}
}

// Dryrun LinkedIn : faux URN, rien d'envoyé.
func TestLinkedInDryrunPublishes(t *testing.T) {
	e, _ := liEnv(t, "dryrun", true)
	p := e.liDraft(t, "essai à blanc")
	_, _ = e.st.Validate(p.ID, "Allan", nil, true)
	res := e.st.PublishDue(context.Background())
	if len(res) != 1 || res[0].Status != StatusPublished || !strings.HasPrefix(res[0].PostURN, "urn:li:share:dryrun-") || res[0].TweetID != "" {
		t.Fatalf("%+v", res)
	}
	got := e.st.store.Snapshot().Post(p.ID)
	if got.Status != StatusPublished || got.PostURN != res[0].PostURN || got.TweetID != "" {
		t.Fatalf("%+v", got)
	}
	if len(e.x.posted) != 0 {
		t.Error("le simulateur n'envoie rien")
	}
}

// A5 : un post LinkedIn forgé « scheduled » sans validation repart en brouillon,
// le backend n'est pas appelé.
func TestLinkedInUnvalidatedNeverReachesBackend(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	for _, status := range []string{StatusScheduled, StatusValidated} {
		p := e.liDraft(t, "forgé "+status)
		past := e.now.Add(-time.Hour)
		_ = e.st.store.Update(func(d *Data) error {
			x := d.Post(p.ID)
			x.Status, x.ScheduledAt, x.ValidatedBy, x.ValidatedAt = status, &past, "", nil
			return nil
		})
		res := e.st.PublishDue(context.Background())
		got := e.st.store.Snapshot().Post(p.ID)
		if got.Status != StatusDraft || len(res) != 1 || res[0].Status != "skipped" {
			t.Errorf("%s : %+v %+v", status, got, res)
		}
	}
	// signé mais sans horodatage : même sort
	p := e.liDraft(t, "signé sans date")
	past := e.now.Add(-time.Hour)
	_ = e.st.store.Update(func(d *Data) error {
		x := d.Post(p.ID)
		x.Status, x.ScheduledAt, x.ValidatedBy = StatusScheduled, &past, "Allan"
		return nil
	})
	e.st.PublishDue(context.Background())
	if got := e.st.store.Snapshot().Post(p.ID); got.Status != StatusDraft {
		t.Errorf("sans ValidatedAt : %+v", got)
	}
	if *calls != 0 || len(e.x.posted) != 0 {
		t.Fatalf("le backend a été appelé %d fois", *calls)
	}
}

// Une étiquette de ligne invalide bloque la publication, même validé.
func TestLinkedInMissingLineBlocksValidateAndPublish(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	p := e.liDraft(t, "sans ligne")
	_ = e.st.store.Update(func(d *Data) error { d.Post(p.ID).Line = "inconnue"; return nil })
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err == nil {
		t.Fatal("une ligne inconnue doit bloquer la validation")
	}
	// ancien brouillon sans ligne : elle est reprise du compte à la validation
	q := e.liDraft(t, "ancien brouillon")
	_ = e.st.store.Update(func(d *Data) error { d.Post(q.ID).Line = ""; return nil })
	v, err := e.st.Validate(q.ID, "Allan", nil, true)
	if err != nil || v.Line != "perso" {
		t.Fatalf("%+v %v", v, err)
	}
	// ligne retirée après validation : bloqué avant publication
	_ = e.st.store.Update(func(d *Data) error { d.Post(q.ID).Line = ""; return nil })
	res := e.st.PublishDue(context.Background())
	if len(res) != 1 || res[0].Status != "skipped" || e.st.store.Snapshot().Post(q.ID).Status != StatusFailed {
		t.Fatalf("%+v", res)
	}
	if *calls != 0 {
		t.Fatal("backend appelé")
	}
}

// Validation signée obligatoire, comme pour X.
func TestLinkedInValidateRequiresSignature(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	p := e.liDraft(t, "à signer")
	if _, err := e.st.Validate(p.ID, "  ", nil, true); err == nil {
		t.Fatal("pas de validation sans signature")
	}
	if _, err := e.st.ScheduleDraft(p.ID, "", ""); err == nil {
		t.Fatal("ScheduleDraft sans signature doit échouer")
	}
	long := e.liDraft(t, strings.Repeat("a", 3001))
	if _, err := e.st.Validate(long.ID, "Allan", nil, true); err == nil {
		t.Fatal("3001 caractères : validation refusée")
	}
}

// A6 : Tick ne fait aucun appel veille/triage/actus/campagne/métriques/analyse
// pour un compte LinkedIn.
func TestLinkedInTickMakesNoXCalls(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	xCalls := 0
	e.st.backendFor = func(*Account) (XBackend, error) { return countX{e.x, &xCalls}, nil }
	e.st.logf = func(string, ...any) {}
	// on retire le compte X : seul LinkedIn reste, chargé de tout ce qui déclenche un appel
	_ = e.st.store.Update(func(d *Data) error {
		d.Accounts = nil
		return nil
	})
	acc := &Account{Name: "pro", Platform: "linkedin", Line: "perso", Backend: "manual", Timezone: "UTC", AutoTriage: true, AutoDraft: true,
		Slots: []string{"09:00"}, Sources: SourceSettings{News: true, Changelog: "http://127.0.0.1:1/changelog", GitHubUser: "personne"}}
	if err := e.st.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddWatch("pro", "user", "@source", 1); err != nil {
		t.Fatal(err)
	}
	_ = e.st.store.Update(func(d *Data) error { d.Account("pro").Watch[0].Primary = true; return nil })
	camp := testCampaign()
	camp.Account = "pro"
	if _, err := e.st.SaveCampaign(camp); err != nil {
		t.Fatal(err)
	}
	old := e.now.Add(-30 * 24 * time.Hour)
	_ = e.st.store.Update(func(d *Data) error {
		d.Items = append(d.Items, &Item{ID: "i1", Account: "pro", Source: "watch:user:source", TweetID: "1", Text: "agents IA", Status: ItemPending, FetchedAt: e.now})
		// un post publié mesurable côté X : LinkedIn n'est jamais relevé
		// éligibles à l'analyse (vues, âge du relevé, pas « dry- ») et assez nombreux pour la déclencher
		pa := e.now.Add(-10 * 24 * time.Hour)
		for i := 0; i < perfAutoMinNewPosts+3; i++ {
			d.Posts = append(d.Posts, &Post{ID: fmt.Sprintf("pp%d", i), Account: "pro", Kind: "post", Text: "ancien", Status: StatusPublished, PublishedAt: &pa, TweetID: fmt.Sprintf("li-%d", i),
				Metrics: &Metrics{Views: perfMinViews + 100, Likes: 5 + i, UpdatedAt: pa.Add(perfMinAge + time.Hour)}, CreatedAt: old, UpdatedAt: old})
		}
		d.Settings.AutoPerfAnalysis = true
		return nil
	})
	for m := 0; m < 6; m++ {
		e.now = e.now.Add(time.Hour)
		out := e.st.Tick(ctx)
		for _, k := range []string{"watch", "triage", "sources", "news", "metrics", "campaigns", "perf"} {
			if v, ok := out[k]; ok {
				if l, isList := v.([]SourcesResult); isList && len(l) == 0 {
					continue
				}
				t.Errorf("Tick a produit %q pour un compte LinkedIn : %+v", k, v)
			}
		}
	}
	if xCalls != 0 || e.ai.count() != 0 {
		t.Fatalf("appels backend=%d, IA=%d : attendu 0", xCalls, e.ai.count())
	}
	// le même Tick, avec un compte X en plus : ses appels ont toujours lieu,
	// ceux du compte LinkedIn jamais
	if err := e.st.SaveAccount(&Account{Name: "xa", Handle: "@xa", Backend: "dryrun", Timezone: "UTC", AutoTriage: true, Slots: []string{"09:00"},
		Sources: SourceSettings{News: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddWatch("xa", "user", "@src", 1); err != nil {
		t.Fatal(err)
	}
	_ = e.st.store.Update(func(d *Data) error {
		d.Account("xa").Watch[0].Primary = true
		d.Items = append(d.Items, &Item{ID: "ix", Account: "xa", Source: "watch:user:src", TweetID: "7", Text: "annonce", Status: ItemPending, FetchedAt: e.now})
		return nil
	})
	e.now = e.now.Add(time.Hour)
	e.st.Tick(ctx)
	if xCalls == 0 {
		t.Error("le compte X doit continuer à appeler X")
	}
	if got := e.st.store.Snapshot().Item("ix"); got != nil && got.Status == ItemPending {
		t.Error("l'actualité du compte X devrait avoir été traitée")
	}
	if n := e.ai.count(); n == 0 {
		t.Error("le compte X doit continuer à utiliser l'IA")
	}
	for _, p := range e.st.store.Snapshot().Posts {
		if p.Account == "pro" && strings.HasPrefix(p.ID, "p") && p.Status == StatusDraft {
			t.Errorf("brouillon LinkedIn issu de l'automatique : %+v", p)
		}
	}
	// analyse manuelle (hors Tick, sans le filtre « due ») : LinkedIn reste exclu
	if _, err := e.st.analyzePerformance(ctx, "pro", false); err != nil && !errors.Is(err, errPerfUnknownAccount) {
		t.Logf("analyse manuelle : %v", err)
	}
	if r, _ := e.st.analyzePerformance(ctx, "", false); r != nil {
		for _, ar := range r.Accounts {
			if ar.Account == "pro" {
				t.Errorf("un compte LinkedIn ne doit pas être analysé : %+v", ar)
			}
		}
	}
	xCalls, aiBefore := 0, e.ai.count()
	if _, err := e.st.ImportBookmarks(ctx, "pro", 5); err == nil {
		t.Error("les signets n'existent pas pour LinkedIn")
	}
	if _, err := e.st.AddItem(ctx, "pro", "123"); err == nil {
		t.Error("AddItem est réservé à X")
	}
	if r := e.st.RunCampaign(ctx, camp.ID, true); r.Skipped == "" {
		t.Errorf("une campagne LinkedIn doit être ignorée : %+v", r)
	}
	if xCalls != 0 || e.ai.count() != aiBefore {
		t.Fatalf("appels backend=%d, IA=%d : attendu 0", xCalls, e.ai.count()-aiBefore)
	}
}

// Les idées, brouillons et réécritures restent actifs pour LinkedIn.
func TestLinkedInIdeasAndRewriteStayActive(t *testing.T) {
	e, _ := liEnv(t, "manual", false)
	p := e.liDraft(t, "premier jet")
	if _, err := e.st.RewritePost(context.Background(), p.ID, "plus court"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.ai.lastSys, "LinkedIn") || strings.Contains(e.ai.lastSys, "280") {
		t.Errorf("la réécriture doit utiliser le prompt LinkedIn :\n%s", e.ai.lastSys)
	}
	a := e.st.store.Snapshot().Account("pro")
	a.Ideas.Enabled = true
	if err := e.st.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	ideas, err := e.st.SuggestIdeas(context.Background(), "pro")
	if err != nil || len(ideas) == 0 {
		t.Fatalf("%v %v", ideas, err)
	}
	dp, err := e.st.DraftIdea(context.Background(), ideas[0].ID)
	if err != nil || dp.Line != "perso" || dp.Status != StatusDraft {
		t.Fatalf("%+v %v", dp, err)
	}
}

// Les métriques ne sont jamais fabriquées pour LinkedIn.
func TestLinkedInNoMetrics(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	p := e.liDraft(t, "mesure")
	_, _ = e.st.Validate(p.ID, "Allan", nil, true)
	e.st.PublishDue(context.Background())
	*calls = 0
	e.now = e.now.Add(7 * time.Hour)
	if n, err := e.st.RefreshMetrics(context.Background(), 0); n != 0 || err != nil || *calls != 0 {
		t.Fatalf("n=%d err=%v calls=%d", n, err, *calls)
	}
	if e.st.store.Snapshot().Post(p.ID).Metrics != nil {
		t.Error("Metrics doit rester nil (« indisponible », jamais 0)")
	}
}

// A7 : un ancien data.json s'ouvre et se réécrit à l'identique pour les comptes X.
func TestLinkedInLegacyDataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := NewStudio(store)
	if err := st.SaveAccount(&Account{Name: "main", Handle: "@moi", Backend: "dryrun", Timezone: "UTC", Slots: []string{"09:00"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddDraft("main", "ancien post", "", "", ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "data.json")
	before, _ := os.ReadFile(path)
	for _, key := range []string{`"platform"`, `"line"`, `"weeklyCap"`, `"authorURN"`, `"postUrn"`} {
		if strings.Contains(string(before), key) {
			t.Errorf("un compte et un post X ne doivent pas écrire %s", key)
		}
	}
	s2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Update(func(*Data) error { return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("data.json relu et réécrit doit rester identique")
	}
	if a := s2.Snapshot().Account("main"); isLinkedIn(a) || a.Platform != "" {
		t.Errorf("un ancien compte est un compte X : %+v", a)
	}
}

// Handler HTTP mark-published.
func TestMarkPublishedHandler(t *testing.T) {
	t.Setenv("XGROWTH_HOME", t.TempDir())
	store, err := OpenStore(homeDir())
	if err != nil {
		t.Fatal(err)
	}
	st := NewStudio(store)
	st.backendFor = BackendFor
	srv := httptest.NewServer(buildHandler(st, ServeOptions{NoAuth: true}))
	t.Cleanup(srv.Close)
	c := srv.Client()
	if code, m := call(t, c, "POST", srv.URL+"/api/accounts", `{"name":"pro","platform":"linkedin","line":"perso","backend":"manual","timezone":"UTC"}`); code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	if code, _ := call(t, c, "POST", srv.URL+"/api/accounts", `{"name":"sans","platform":"linkedin","backend":"manual"}`); code == 200 {
		t.Fatal("un compte LinkedIn sans ligne doit être refusé")
	}
	p, _ := st.AddDraft("pro", "via http", "", "", "")
	if code, _ := call(t, c, "POST", srv.URL+"/api/posts/"+p.ID+"/mark-published", `{}`); code == 200 {
		t.Fatal("un brouillon ne peut pas être marqué publié")
	}
	if code, m := call(t, c, "POST", srv.URL+"/api/posts/"+p.ID+"/validate", `{"by":"Allan","now":true}`); code != http.StatusOK {
		t.Fatalf("%d %v", code, m)
	}
	st.PublishDue(context.Background())
	for _, bad := range []string{"javascript:alert(1)", "linkedin.com/posts/x", "https://", "ftp://h/x", `\"><script>`} {
		body, _ := json.Marshal(map[string]string{"url": bad})
		if code, _ := call(t, c, "POST", srv.URL+"/api/posts/"+p.ID+"/mark-published", string(body)); code == 200 {
			t.Fatalf("url invalide acceptée : %q", bad)
		}
	}
	if store.Snapshot().Post(p.ID).Status != StatusManual {
		t.Fatal("une url refusée ne doit rien changer")
	}
	code, m := call(t, c, "POST", srv.URL+"/api/posts/"+p.ID+"/mark-published", `{"url":"https://www.linkedin.com/posts/x"}`)
	if code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	got := store.Snapshot().Post(p.ID)
	if got.Status != StatusPublished || got.PostURN != "https://www.linkedin.com/posts/x" {
		t.Fatalf("%+v", got)
	}
}

// Aucun écrivain de Status hors Validate / PublishDue / MarkPublished ne
// peut atteindre scheduled|validated|publishing|published (revue du chemin
// sans validation).
func TestLinkedInStatusWritersAreConfined(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	allowed := map[string]bool{"studio.go": true, "store.go": true}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] {
			continue
		}
		b, _ := os.ReadFile(f)
		for i, line := range strings.Split(string(b), "\n") {
			for _, s := range []string{"StatusScheduled", "StatusValidated", "StatusPublishing", "StatusPublished"} {
				if strings.Contains(line, ".Status = "+s) || strings.Contains(line, "Status, ") && strings.Contains(line, "= "+s) {
					t.Errorf("%s:%d écrit un statut publiable hors du circuit validé : %s", f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}
