package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tune modifie le compte « main » et les réglages communs.
func (e *env) tune(t *testing.T, acc func(a *Account), st func(s *Settings)) {
	t.Helper()
	if err := e.st.store.Update(func(d *Data) error {
		if acc != nil {
			acc(d.Account("main"))
		}
		if st != nil {
			st(&d.Settings)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mustContain(t *testing.T, where, s string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("%s : %q absent de\n%s", where, w, s)
		}
	}
}

func TestPromptsCarryAccountTuning(t *testing.T) {
	e := newEnv(t)
	e.tune(t, func(a *Account) {
		a.Goals = "Trouver 50 clients pour l'outil d'audit"
		a.Length = "moyen"
		a.Examples = []string{"J'ai coupé notre onboarding de 9 à 3 écrans."}
		a.Prompts.Ideas = "Une observation tirée du support client."
		a.Prompts.Rewrite = "Garde toujours la première phrase."
	}, func(s *Settings) {
		s.Rules = []string{"Jamais de point d'exclamation."}
		s.Prompts.Reply = "Réponds en une phrase, avec un chiffre."
	})
	ctx := context.Background()
	if _, err := e.st.GenerateDrafts(ctx, "main", "", "", 2, ""); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "prompt système", e.ai.lastSys, "<objectifs>", "Trouver 50 clients", "Longueur visée : moyenne", "sans X Premium, reste sous 280",
		"<regles>", "Jamais de point d'exclamation.", "<posts_de_reference>", "J'ai coupé notre onboarding")
	mustContain(t, "demande sans sujet", e.ai.lastMsg, "Une observation tirée du support client.")
	if strings.Contains(e.ai.lastMsg, DefaultPrompts.Ideas) {
		t.Error("la consigne du compte doit remplacer la consigne par défaut")
	}

	// réponse : consigne commune ; citation : consigne par défaut
	if _, err := e.st.DraftReply(ctx, "main", "123", false); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "réponse", e.ai.lastMsg, "Réponds en une phrase, avec un chiffre.")
	if _, err := e.st.DraftReply(ctx, "main", "124", true); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "citation", e.ai.lastMsg, DefaultPrompts.Quote)

	// réécriture : consigne du compte
	p := e.draft(t, "brouillon à reprendre")
	if _, err := e.st.RewritePost(ctx, p.ID, "plus direct"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "réécriture", e.ai.lastMsg, "Garde toujours la première phrase.", "plus direct")

	// brouillon tiré de la veille : consigne par défaut des contenus de veille
	_ = e.st.store.Update(func(d *Data) error {
		d.Items = append(d.Items, &Item{ID: "i_1", Account: "main", Author: "src", Text: "Nouvelle version de l'API", Status: ItemKept})
		return nil
	})
	if _, err := e.st.GenerateDrafts(ctx, "main", "", "i_1", 1, ""); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "veille", e.ai.lastMsg, DefaultPrompts.Item)
}

func TestDefaultPromptsWithoutTuning(t *testing.T) {
	e := newEnv(t)
	if _, err := e.st.GenerateDrafts(context.Background(), "main", "", "", 1, ""); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "demande", e.ai.lastMsg, DefaultPrompts.Ideas)
	for _, absent := range []string{"<objectifs>", "<regles>", "<posts_de_reference>"} {
		if strings.Contains(e.ai.lastSys, absent) {
			t.Errorf("%s ne doit pas apparaître sans réglage", absent)
		}
	}
	mustContain(t, "longueur par défaut", e.ai.lastSys, "Longueur visée : courte")
	item := e.ai.lastSchema["properties"].(map[string]any)["drafts"].(map[string]any)["items"].(map[string]any)
	if props := item["properties"].(map[string]any); props["type"] != nil || props["visual"] != nil {
		t.Fatal("sans types ni guide de visuel, le schéma d'un brouillon ne change pas")
	}
}

func TestPostTypesGuideDraftsAndMix(t *testing.T) {
	e := newEnv(t)
	e.tune(t, nil, func(s *Settings) { s.PostTypes = DefaultPostTypes })
	ctx := context.Background()
	ps, err := e.st.GenerateDrafts(ctx, "main", "un sujet", "", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if ps[0].PostType != "utile" {
		t.Fatalf("le type choisi par l'IA doit être gardé, obtenu %q", ps[0].PostType)
	}
	mustContain(t, "types", e.ai.lastMsg, "<types_de_posts>", "probleme_solution", "part visée 20 %", "le plus en retard")
	enum := e.ai.lastSchema["properties"].(map[string]any)["drafts"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["type"].(map[string]any)["enum"].([]any)
	if len(enum) != 4 {
		t.Fatalf("le schéma doit limiter le type aux clés du compte : %v", enum)
	}

	// deux posts « utile » : « portee » devient le plus en retard
	mix := ComputeTypeMix(e.st.store.Snapshot(), "main", DefaultPostTypes, e.now)
	if mix.Total != 2 || mix.Next != "portee" || mix.Mix[0].Actual != 100 {
		t.Fatalf("répartition inattendue : %+v", mix)
	}

	// un type imposé est transmis et appliqué ; un type inconnu est refusé
	ps, err = e.st.GenerateDrafts(ctx, "main", "", "", 1, "coulisses")
	if err != nil || ps[0].PostType != "coulisses" {
		t.Fatalf("type imposé : %+v %v", ps, err)
	}
	mustContain(t, "type imposé", e.ai.lastMsg, "Type demandé pour tous les brouillons : coulisses")
	if _, err := e.st.GenerateDrafts(ctx, "main", "", "", 1, "inconnu"); err == nil {
		t.Fatal("un type inconnu doit être refusé")
	}

	// les types du compte remplacent les types communs
	e.tune(t, func(a *Account) {
		a.PostTypes = []PostType{{Key: "fil", Label: "Fil", Share: 100, Prompt: "Un fil."}}
	}, nil)
	ps, _ = e.st.GenerateDrafts(ctx, "main", "", "", 1, "")
	if ps[0].PostType != "fil" {
		t.Fatalf("types du compte ignorés : %q", ps[0].PostType)
	}
}

func TestVisualGuideAddsVisualBrief(t *testing.T) {
	e := newEnv(t)
	e.tune(t, func(a *Account) { a.Prompts.Visual = "Une carte de texte sobre, fond sombre." }, nil)
	ps, err := e.st.GenerateDrafts(context.Background(), "main", "sujet", "", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if ps[0].VisualBrief != "Un schéma simple" {
		t.Fatalf("idée de visuel non enregistrée : %+v", ps[0])
	}
	mustContain(t, "visuel", e.ai.lastMsg, "<guide_visuel>", "Une carte de texte sobre")
}

func TestNormalizeTuning(t *testing.T) {
	types, err := NormalizePostTypes([]PostType{{Label: " Problème → solution ", Share: 30, Prompt: " p "}})
	if err != nil || types[0].Key != "probleme_solution" || types[0].Prompt != "p" {
		t.Fatalf("%+v %v", types, err)
	}
	bad := [][]PostType{
		{{Label: "", Prompt: "p"}},
		{{Label: "A b", Prompt: ""}},
		{{Label: "A b", Prompt: "p", Share: 120}},
		{{Label: "A b", Prompt: "p"}, {Label: "a-b", Prompt: "q"}},
		{{Key: "Mauvaise Clé", Label: "x", Prompt: "p"}},
	}
	for _, b := range bad {
		if _, err := NormalizePostTypes(b); err == nil {
			t.Errorf("types invalides acceptés : %+v", b)
		}
	}
	e := newEnv(t)
	acc := e.st.store.Snapshot().Account("main")
	acc.Length = "immense"
	if err := e.st.SaveAccount(acc); err == nil {
		t.Fatal("une longueur inconnue doit être refusée")
	}
	if got := SplitExamples("Post un\nsur deux lignes\n---\n\n---\nPost deux\n"); len(got) != 2 || got[0] != "Post un\nsur deux lignes" {
		t.Fatalf("découpage des exemples : %q", got)
	}
	if got := SplitLines("- Règle un\n\n• Règle deux  "); len(got) != 2 || got[1] != "Règle deux" {
		t.Fatalf("découpage des règles : %q", got)
	}
}

// Un data.json écrit avant ces réglages doit se charger et se réécrire sans perte.
func TestLegacyDataJSONStillLoads(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"settings":{"aiProvider":"claude-code","effort":"medium","competitors":["Acme"],"bannedPhrases":[],"minGapMinutes":90,"triageMinScore":6,"maxItemsPerRun":40,"onboarded":true},
"accounts":[{"name":"main","handle":"moi","backend":"dryrun","premium":false,"language":"français","timezone":"UTC","slots":["09:00"],"voice":"Direct","instructions":"Tutoie","topics":["IA"],"triageRules":"","autoTriage":false,"autoDraft":false,"dailyLimit":3,"watch":[]}],
"posts":[{"id":"p_1","account":"main","kind":"post","text":"Ancien post","status":"draft","createdAt":"2026-11-01T08:00:00Z","updatedAt":"2026-11-01T08:00:00Z"}],
"items":[],"learnings":[{"id":"l_1","account":"main","text":"Plus court","createdAt":"2026-11-01T08:00:00Z"}],"log":[],"auth":{}}`
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := store.Snapshot()
	a := d.Account("main")
	if a == nil || a.Voice != "Direct" || d.Settings.MinGapMinutes != 90 || !d.Settings.Onboarded || len(d.Learnings) != 1 {
		t.Fatalf("ancien contenu perdu : %+v %+v", a, d.Settings)
	}
	if a.Goals != "" || a.Length != "" || len(a.PostTypes) != 0 || a.Prompts != (AIPrompts{}) || len(d.RuleProposals) != 0 ||
		d.Settings.RulesAutoAccept || d.Settings.AutoSynthesis || d.Learnings[0].SynthesizedAt != nil {
		t.Fatal("les nouveaux réglages doivent être vides sur un ancien fichier")
	}
	// une écriture garde l'ancien contenu et n'ajoute pas de champs vides inutiles
	if err := store.Update(func(d *Data) error { d.AddLog("info", "test"); return nil }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "data.json"))
	for _, absent := range []string{`"prompts"`, `"postTypes"`, `"ruleProposals"`, `"goals"`, `"synthesizedAt"`} {
		if strings.Contains(string(b), absent) {
			t.Errorf("champ vide %s écrit dans data.json", absent)
		}
	}
	mustContain(t, "data.json réécrit", string(b), `"voice": "Direct"`, `"Ancien post"`, `"Plus court"`)
}

func TestSynthesisTurnsFeedbackIntoRules(t *testing.T) {
	e := newEnv(t)
	if err := e.st.SaveAccount(&Account{Name: "autre", Handle: "autre", Backend: "dryrun"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := e.st.Synthesize(ctx, ""); err == nil {
		t.Fatal("sans retours, la synthèse doit refuser")
	}
	p1, p2 := e.draft(t, "un"), e.draft(t, "deux")
	if _, err := e.st.Reject(p1.ID, "trop de jargon"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.RewritePost(ctx, p2.ID, "plus court"); err != nil {
		t.Fatal(err)
	}
	_ = e.st.store.Update(func(d *Data) error {
		d.Learnings = append(d.Learnings, Learning{ID: "l_autre", Account: "autre", Text: "pas d'emoji", CreatedAt: e.now})
		return nil
	})

	res, err := e.st.Synthesize(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Examined != 3 || len(res.Proposed) != 4 {
		t.Fatalf("synthèse inattendue : %+v", res)
	}
	mustContain(t, "demande de synthèse", e.ai.lastMsg, "trop de jargon", "plus court", "@autre")
	d := e.st.store.Snapshot()
	for _, l := range d.Learnings {
		if l.SynthesizedAt == nil {
			t.Fatal("chaque retour examiné doit être marqué")
		}
	}
	var own, common *RuleProposal
	for _, r := range d.RuleProposals {
		if r.Status != RuleProposed {
			t.Fatal("sans application automatique, une règle attend la décision du propriétaire")
		}
		if r.Text == "Règle commune" {
			common = r
		} else if r.Account == "main" && own == nil {
			own = r
		}
	}
	if common == nil || common.Account != "" || len(common.Evidence) != 2 {
		t.Fatalf("règle appuyée par deux comptes : %+v", common)
	}
	if own == nil {
		t.Fatal("une règle tirée des retours d'un seul compte doit lui être réservée")
	}

	// une règle proposée n'entre pas dans les prompts ; acceptée, si
	if _, err := e.st.GenerateDrafts(ctx, "main", "x", "", 1, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.ai.lastSys, own.Text) {
		t.Fatal("une règle seulement proposée ne doit pas guider l'IA")
	}
	if _, err := e.st.DecideRule(own.ID, "accept", "Évite le jargon technique."); err != nil {
		t.Fatal(err)
	}
	_, _ = e.st.GenerateDrafts(ctx, "main", "x", "", 1, "")
	mustContain(t, "règle acceptée", e.ai.lastSys, "Évite le jargon technique.")
	if rules := e.st.store.Snapshot().RulesFor("autre"); len(rules) != 0 {
		t.Fatalf("la règle d'un compte ne vaut pas pour les autres : %v", rules)
	}
	if _, err := e.st.DecideRule(own.ID, "refuse", ""); err == nil {
		t.Fatal("une règle en place se retire, elle ne se refuse pas")
	}
	if _, err := e.st.DecideRule(own.ID, "revoke", ""); err != nil {
		t.Fatal(err)
	}
	_, _ = e.st.GenerateDrafts(ctx, "main", "x", "", 1, "")
	if strings.Contains(e.ai.lastSys, "Évite le jargon technique.") {
		t.Fatal("une règle retirée ne doit plus guider l'IA")
	}

	// application automatique : les nouvelles règles sont en place d'emblée
	e.tune(t, nil, func(s *Settings) { s.RulesAutoAccept = true })
	_ = e.st.store.Update(func(d *Data) error {
		d.Learnings = append(d.Learnings, Learning{ID: "l_a", Account: "main", Text: "a", CreatedAt: e.now},
			Learning{ID: "l_b", Account: "main", Text: "b", CreatedAt: e.now})
		return nil
	})
	res, err = e.st.Synthesize(ctx, "main")
	if err != nil || len(res.Proposed) == 0 || res.Proposed[0].Status != RuleAccepted {
		t.Fatalf("application automatique : %+v %v", res, err)
	}
	// une règle déjà connue n'est jamais reproposée
	_ = e.st.store.Update(func(d *Data) error {
		for i := range d.Learnings {
			if d.Learnings[i].ID == "l_a" || d.Learnings[i].ID == "l_b" {
				d.Learnings[i].SynthesizedAt = nil
			}
		}
		return nil
	})
	res, _ = e.st.Synthesize(ctx, "main")
	if len(res.Proposed) != 0 {
		t.Fatalf("règles en double proposées : %+v", res.Proposed)
	}
}

func TestWeeklySynthesisInTick(t *testing.T) {
	e := newEnv(t)
	add := func(n int) {
		_ = e.st.store.Update(func(d *Data) error {
			for i := 0; i < n; i++ {
				d.Learnings = append(d.Learnings, Learning{ID: newID("l"), Account: "main", Text: "retour", CreatedAt: e.now})
			}
			return nil
		})
	}
	add(3)
	if _, ok := e.st.Tick(context.Background())["synthesis"]; ok {
		t.Fatal("synthèse hebdomadaire désactivée par défaut")
	}
	e.tune(t, nil, func(s *Settings) { s.AutoSynthesis = true })
	if _, ok := e.st.Tick(context.Background())["synthesis"]; !ok {
		t.Fatal("synthèse attendue : activée, 3 retours en attente")
	}
	add(3)
	e.now = e.now.Add(6 * 24 * time.Hour)
	if _, ok := e.st.Tick(context.Background())["synthesis"]; ok {
		t.Fatal("une seule synthèse par semaine")
	}
	e.now = e.now.Add(24 * time.Hour)
	if _, ok := e.st.Tick(context.Background())["synthesis"]; !ok {
		t.Fatal("synthèse attendue après 7 jours")
	}
}

// ---------------------------------------------------------------- API

func tuningServer(t *testing.T) (*httptest.Server, *env) {
	t.Helper()
	e := newEnv(t)
	srv := httptest.NewServer(buildHandler(e.st, ServeOptions{NoAuth: true}))
	t.Cleanup(srv.Close)
	return srv, e
}

func TestAPITuningSettingsAndRules(t *testing.T) {
	srv, e := tuningServer(t)
	c := &http.Client{}

	// réglages communs : fusion partielle, normalisation, validation
	st, m := call(t, c, "POST", srv.URL+"/api/settings", `{"rules":["- Une règle\n- Une autre"],"prompts":{"reply":"  Court.  "},"postTypes":[{"label":"Coulisses","share":50,"prompt":"Du vrai."}],"rulesAutoAccept":true}`)
	if st != 200 {
		t.Fatalf("réglages refusés : %v", m)
	}
	s := e.st.store.Snapshot().Settings
	if len(s.Rules) != 2 || s.Prompts.Reply != "Court." || s.PostTypes[0].Key != "coulisses" || !s.RulesAutoAccept || s.MinGapMinutes != 60 {
		t.Fatalf("réglages enregistrés : %+v", s)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/settings", `{"prompts":{"ideas":"Autre"}}`); st != 200 {
		t.Fatal("mise à jour partielle refusée")
	}
	if s := e.st.store.Snapshot().Settings; s.Prompts.Reply != "Court." || s.Prompts.Ideas != "Autre" {
		t.Fatalf("une consigne envoyée seule ne doit pas effacer les autres : %+v", s.Prompts)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/settings", `{"postTypes":[{"label":"Sans consigne","share":10}]}`); st != 400 {
		t.Fatal("un type sans consigne doit être refusé")
	}

	// compte : nouveaux champs acceptés et validés
	acc := e.st.store.Snapshot().Account("main")
	acc.Goals, acc.Length, acc.Examples = "Des clients", "long", []string{" Exemple ", ""}
	acc.Prompts.Visual = "Un schéma."
	body, _ := json.Marshal(acc)
	if st, m := call(t, c, "POST", srv.URL+"/api/accounts", string(body)); st != 200 {
		t.Fatalf("compte refusé : %v", m)
	}
	if a := e.st.store.Snapshot().Account("main"); a.Goals != "Des clients" || len(a.Examples) != 1 || a.Examples[0] != "Exemple" || a.Prompts.Visual != "Un schéma." {
		t.Fatalf("compte enregistré : %+v", a)
	}
	acc.Length = "infini"
	body, _ = json.Marshal(acc)
	if st, _ := call(t, c, "POST", srv.URL+"/api/accounts", string(body)); st != 400 {
		t.Fatal("une longueur inconnue doit être refusée")
	}

	// brouillon d'un type imposé
	if st, m := call(t, c, "POST", srv.URL+"/api/draft", `{"account":"main","topic":"x","n":1,"type":"coulisses"}`); st != 200 {
		t.Fatalf("brouillon typé refusé : %v", m)
	}

	// synthèse puis décisions
	if st, _ := call(t, c, "POST", srv.URL+"/api/rules/synthesize", `{}`); st != 400 {
		t.Fatal("synthèse sans retours : erreur attendue")
	}
	call(t, c, "POST", srv.URL+"/api/learnings", `{"account":"main","text":"trop long"}`)
	call(t, c, "POST", srv.URL+"/api/learnings", `{"account":"main","text":"trop formel"}`)
	st, m = call(t, c, "POST", srv.URL+"/api/rules/synthesize", `{"account":"main"}`)
	if st != 200 {
		t.Fatalf("synthèse refusée : %v", m)
	}
	proposed := m["data"].(map[string]any)["proposed"].([]any)
	id := proposed[0].(map[string]any)["id"].(string)
	if proposed[0].(map[string]any)["status"] != RuleAccepted {
		t.Fatal("rulesAutoAccept : la règle doit être appliquée d'office")
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/rules/"+id+"/revoke", `{}`); st != 200 {
		t.Fatal("retrait refusé")
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/rules/"+id+"/supprimer", `{}`); st != 400 {
		t.Fatal("décision inconnue acceptée")
	}

	// l'état expose règles, valeurs par défaut et répartition des types
	_, m = call(t, c, "GET", srv.URL+"/api/state", "")
	data := m["data"].(map[string]any)
	if len(data["rules"].([]any)) == 0 || data["defaults"].(map[string]any)["prompts"].(map[string]any)["reply"] != DefaultPrompts.Reply {
		t.Fatalf("état incomplet : rules=%v", data["rules"])
	}
	if mix := data["typeMix"].(map[string]any)["main"].(map[string]any); mix["total"].(float64) != 2 { // la fausse IA renvoie 2 brouillons
		t.Fatalf("répartition des types : %v", mix)
	}
}

func TestCLIPostTypesFlag(t *testing.T) {
	if got := postTypesFlag("defaut"); len(got) != len(DefaultPostTypes) {
		t.Fatalf("defaut : %v", got)
	}
	if got := postTypesFlag(""); got != nil {
		t.Fatal("vide = aucun type")
	}
	f := filepath.Join(t.TempDir(), "types.json")
	_ = os.WriteFile(f, []byte(`[{"label":"Fil","share":100,"prompt":"Un fil."}]`), 0o600)
	if got := postTypesFlag("@" + f); len(got) != 1 || got[0].Label != "Fil" {
		t.Fatalf("@fichier : %v", got)
	}
	a := parseArgs([]string{"settings", "--rules-auto-accept", "--auto-synthesis=false", "--reply-prompt", "Court."})
	var p AIPrompts
	applyPromptFlags(&p, a)
	if !a.boolean("rules-auto-accept") || a.boolean("auto-synthesis") || p.Reply != "Court." {
		t.Fatalf("options : %+v %+v", a.flags, p)
	}
}
