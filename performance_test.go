package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// measured décrit un post publié et mesuré (données fictives).
type measured struct {
	id, kind, postType, text, sourceItem string
	account                              string // vide = « main »
	rate                                 float64
	views                                int
	measuredAfter                        time.Duration // délai entre la publication et le relevé
	tweetID                              string        // vide = identifiant réel fictif
	noMetrics                            bool
}

// publish ajoute des posts publiés et mesurés (au compte « main » par défaut).
func (e *env) publish(t *testing.T, ms ...measured) {
	t.Helper()
	if err := e.st.store.Update(func(d *Data) error {
		for _, m := range ms {
			pub := e.now.Add(-10 * 24 * time.Hour)
			p := &Post{ID: m.id, Account: "main", Kind: m.kind, Text: m.text, Status: StatusPublished, PublishedAt: &pub,
				TweetID: m.tweetID, SourceItem: m.sourceItem, PostType: m.postType, CreatedAt: pub, UpdatedAt: pub}
			if m.account != "" {
				p.Account = m.account
			}
			if p.Kind == "" {
				p.Kind = "post"
			}
			if p.Text == "" {
				p.Text = "Texte fictif"
			}
			if p.TweetID == "" {
				p.TweetID = "9" + m.id
			}
			if !m.noMetrics {
				views, after := m.views, m.measuredAfter
				if views == 0 {
					views = 1000
				}
				if after == 0 {
					after = 72 * time.Hour
				}
				p.Metrics = &Metrics{Views: views, EngagementRate: m.rate, UpdatedAt: pub.Add(after)}
			}
			d.Posts = append(d.Posts, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (e *env) digest() PerfDigest {
	return ComputePerfDigest(e.st.store.Snapshot(), "main", e.now)
}

func signalOf(dg PerfDigest, key string) *PerfSignal {
	for i := range dg.Signals {
		if dg.Signals[i].Key == key {
			return &dg.Signals[i]
		}
	}
	return nil
}

func TestPerfDigestIgnoresUnmeasuredPosts(t *testing.T) {
	e := newEnv(t)
	e.publish(t,
		measured{id: "p_ok1", rate: 2},
		measured{id: "p_ok2", rate: 3},
		// exclus
		measured{id: "p_dry", rate: 9, tweetID: "dry-123"},
		measured{id: "p_nometrics", noMetrics: true},
		measured{id: "p_fewviews", rate: 9, views: 49},
		measured{id: "p_young", rate: 9, measuredAfter: 47 * time.Hour},
	)
	// ni un brouillon mesuré, ni un post publié sans date, ni le post d'un autre compte
	_ = e.st.store.Update(func(d *Data) error {
		pub := e.now.Add(-10 * 24 * time.Hour)
		m := &Metrics{Views: 1000, EngagementRate: 9, UpdatedAt: e.now}
		d.Posts = append(d.Posts,
			&Post{ID: "p_draft", Account: "main", Kind: "post", Status: StatusDraft, PublishedAt: &pub, TweetID: "91", Metrics: m},
			&Post{ID: "p_nodate", Account: "main", Kind: "post", Status: StatusPublished, TweetID: "92", Metrics: m},
			&Post{ID: "p_other", Account: "autre", Kind: "post", Status: StatusPublished, PublishedAt: &pub, TweetID: "93", Metrics: m})
		return nil
	})
	dg := e.digest()
	if dg.Eligible != 2 || dg.Baseline != 2.5 {
		t.Fatalf("seuls les deux posts mesurés comptent : %+v", dg)
	}
	for _, id := range append(dg.Top, dg.Bottom...) {
		if id != "p_ok1" && id != "p_ok2" {
			t.Fatalf("post non éligible %s dans le résumé : %+v", id, dg)
		}
	}
	// aux seuils exacts (50 vues, relevé 48 h après), le post compte : N → N+1
	e.publish(t, measured{id: "p_limit", rate: 4, views: perfMinViews, measuredAfter: perfMinAge})
	if dg := e.digest(); dg.Eligible != 3 {
		t.Fatalf("un post éligible de plus doit donner 3, pas %d", dg.Eligible)
	}
}

func TestPerfDigestMatchesAccountCaseInsensitively(t *testing.T) {
	e := newEnv(t)
	e.publish(t,
		measured{id: "p_lower", rate: 1},
		measured{id: "p_title", rate: 3, account: "Main"},
		measured{id: "p_upper", rate: 5, account: "MAIN"},
		measured{id: "p_other", rate: 9, account: "autre"},
	)
	for _, name := range []string{"main", "Main", "MAIN"} {
		dg := ComputePerfDigest(e.st.store.Snapshot(), name, e.now)
		if dg.Eligible != 3 || dg.Baseline != 3 {
			t.Fatalf("%q : les trois graphies de « main » doivent compter : %+v", name, dg)
		}
		if dg.Account != "main" {
			t.Fatalf("%q : le résumé doit porter le nom enregistré du compte, pas %q", name, dg.Account)
		}
		if len(dg.Top) != 3 || dg.Top[0] != "p_upper" || dg.Bottom[0] != "p_lower" {
			t.Fatalf("%q : classement inattendu : %+v", name, dg)
		}
	}
}

func TestPerfDigestSignalsNeedSampleAndEffect(t *testing.T) {
	e := newEnv(t)
	e.tune(t, func(a *Account) {
		a.PostTypes = []PostType{{Key: "astuce", Label: "Astuce", Share: 50, Prompt: "p"}}
	}, nil)
	var ms []measured
	add := func(prefix, typ string, rates ...float64) {
		for i, r := range rates {
			ms = append(ms, measured{id: prefix + string(rune('a'+i)), postType: typ, rate: r})
		}
	}
	add("p_paire_", "paire", 10, 10)          // groupe de 2, rapport 5 : trop petit
	add("p_tiede_", "tiede", 2.4, 2.4, 2.4)   // groupe de 3, rapport 1,2 : effet trop faible
	add("p_astuce_", "astuce", 3, 4, 6)       // médiane 4 (moyenne 4,33) : rapport 2
	add("p_retrait_", "retrait", 0.5, 1, 1.5) // médiane 1 : rapport 0,5
	add("p_neutre_", "", 2, 2, 2, 2, 2, 2, 2, 2, 2)
	e.publish(t, ms...)

	// à la main : 20 taux triés 0,5 1 1,5 2×9 2,4×3 3 4 6 10 10 ; 10e et 11e valeurs = 2 et 2
	const baseline = 2.0
	dg := e.digest()
	if dg.Eligible != 20 || dg.Baseline != baseline {
		t.Fatalf("médiane du compte : %+v", dg)
	}
	if s := signalOf(dg, "type:paire"); s != nil {
		t.Fatalf("un groupe de 2 posts ne donne pas de signal : %+v", s)
	}
	if s := signalOf(dg, "type:tiede"); s != nil {
		t.Fatalf("un rapport de 1,2 ne donne pas de signal : %+v", s)
	}
	up := signalOf(dg, "type:astuce")
	if up == nil || up.N != 3 || up.Median != 4 || up.Baseline != baseline || up.Ratio != 2 {
		t.Fatalf("signal « mieux » attendu (médiane 4, compte 2, rapport 2) : %+v", up)
	}
	if up.Label != "type Astuce" || strings.Join(up.PostIDs, ",") != "p_astuce_c,p_astuce_b,p_astuce_a" {
		t.Fatalf("libellé ou posts du signal : %+v", up)
	}
	down := signalOf(dg, "type:retrait")
	if down == nil || down.Median != 1 || down.Ratio != 0.5 || down.Ratio > perfRatioLow {
		t.Fatalf("signal « en retrait » attendu (médiane 1, rapport 0,5) : %+v", down)
	}
	// genre, longueur et origine sont communs à tous les posts : rapport 1, aucun signal
	if len(dg.Signals) != 2 {
		t.Fatalf("seuls les deux signaux nets doivent rester : %+v", dg.Signals)
	}
	if strings.Join(dg.Top, ",") != "p_paire_a,p_paire_b,p_astuce_c" || strings.Join(dg.Bottom, ",") != "p_retrait_a,p_retrait_b,p_retrait_c" {
		t.Fatalf("meilleurs et pires posts : %v / %v", dg.Top, dg.Bottom)
	}

	// médiane du compte nulle : aucun signal
	z := newEnv(t)
	var zs []measured
	for i := 0; i < 6; i++ {
		zs = append(zs, measured{id: "p_zero_" + string(rune('a'+i)), postType: "astuce"})
	}
	z.publish(t, zs...)
	if dg := z.digest(); dg.Eligible != 6 || dg.Baseline != 0 || len(dg.Signals) != 0 {
		t.Fatalf("médiane nulle : aucun signal attendu : %+v", dg)
	}
}

func TestPerfDigestDimensions(t *testing.T) {
	long := strings.Repeat("é", 281)
	for text, want := range map[string]string{strings.Repeat("é", 140): "court", strings.Repeat("é", 141): "moyen",
		strings.Repeat("é", 280): "moyen", long: "long"} {
		if got := perfLength(text); got != want {
			t.Errorf("%d runes : %s, attendu %s", len([]rune(text)), got, want)
		}
	}
	e := newEnv(t)
	var ms []measured
	for i := 0; i < 5; i++ {
		ms = append(ms, measured{id: "p_base_" + string(rune('a'+i)), rate: 2})
	}
	for i := 0; i < 3; i++ {
		ms = append(ms, measured{id: "p_rep_" + string(rune('a'+i)), kind: "reply", text: long, sourceItem: "i_1", rate: 6})
	}
	e.publish(t, ms...)
	dg := e.digest()
	var keys []string
	for _, s := range dg.Signals {
		keys = append(keys, s.Key)
		if s.N != 3 || s.Median != 6 || s.Baseline != 2 || s.Ratio != 3 {
			t.Errorf("signal %s : %+v", s.Key, s)
		}
	}
	if strings.Join(keys, ",") != "kind:reply,len:long,source:veille" {
		t.Fatalf("dimensions genre, longueur et origine attendues : %v", keys)
	}
}

func TestLegacyRuleStillWorks(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"settings":{"aiProvider":"claude-code","rulesAutoAccept":false,"autoSynthesis":true},
"accounts":[{"name":"main","handle":"moi","backend":"dryrun","timezone":"UTC","slots":["09:00"],"dailyLimit":3,"watch":[]}],
"posts":[],"items":[],"log":[],"auth":{},
"ruleProposals":[{"id":"r_old","account":"main","text":"Phrases courtes.","rationale":"retours","evidence":["l_1"],"status":"accepted","createdAt":"2026-11-01T08:00:00Z"},
{"id":"r_wait","text":"Pas de jargon.","status":"proposed","createdAt":"2026-11-01T08:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := store.Snapshot()
	old := d.RuleProposal("r_old")
	if old == nil || old.Source != "" || old.Basis != "" || len(old.Signals) != 0 || len(old.Evidence) != 1 {
		t.Fatalf("ancienne règle mal chargée : %+v", old)
	}
	if d.Settings.AutoPerfAnalysis || d.Settings.LastPerfAnalysisAt != nil {
		t.Fatal("l'analyse des performances doit être désactivée sur un ancien fichier")
	}
	if rules := d.RulesFor("main"); strings.Join(rules, "|") != "Phrases courtes." {
		t.Fatalf("seule la règle acceptée vaut pour le compte : %v", rules)
	}
	// une ancienne règle proposée s'accepte toujours
	st := NewStudio(store)
	if _, err := st.DecideRule("r_wait", "accept", ""); err != nil {
		t.Fatal(err)
	}
	if rules := store.Snapshot().RulesFor("main"); strings.Join(rules, "|") != "Phrases courtes.|Pas de jargon." {
		t.Fatalf("règle acceptée absente : %v", rules)
	}
	// la réécriture n'ajoute pas les nouveaux champs vides
	b, _ := os.ReadFile(filepath.Join(dir, "data.json"))
	for _, absent := range []string{`"source"`, `"basis"`, `"signals"`, `"lastPerfAnalysisAt"`} {
		if strings.Contains(string(b), absent) {
			t.Errorf("champ vide %s écrit dans data.json", absent)
		}
	}
}

func TestSynthesisRulesKeepFeedbackSource(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error {
		d.Learnings = append(d.Learnings, Learning{ID: "l_a", Account: "main", Text: "trop long", CreatedAt: e.now},
			Learning{ID: "l_b", Account: "main", Text: "trop de jargon", CreatedAt: e.now})
		return nil
	})
	res, err := e.st.Synthesize(context.Background(), "main")
	if err != nil || len(res.Proposed) == 0 {
		t.Fatalf("synthèse : %+v %v", res, err)
	}
	for _, r := range e.st.store.Snapshot().RuleProposals {
		if (r.Source != "" && r.Source != RuleSourceFeedback) || r.Basis != "" || len(r.Signals) != 0 || r.Status != RuleProposed {
			t.Fatalf("une règle de retours garde le comportement de #4 : %+v", r)
		}
	}
}

// ---------------------------------------------------------------- analyse IA

const (
	perfTargetText = "Texte du post cité, à ne jamais envoyer à l'analyse"
	perfItemText   = "Contenu de veille fictif, à ne jamais envoyer à l'analyse"
)

// perfFixture : 10 posts mesurés de « main » (données fictives). Les 4 posts
// « utile » tirés de la veille ont une médiane de 6,5 % contre 3,0 % pour le
// compte : deux signaux, type:utile et source:veille. Le post le plus faible
// est une réponse dont le post cible ne doit pas partir à l'IA.
func perfFixture(t *testing.T, e *env, account string) {
	t.Helper()
	if err := e.st.store.Update(func(d *Data) error {
		d.Settings.PostTypes = DefaultPostTypes
		d.Items = append(d.Items, &Item{ID: "it_" + account, Account: account, Text: perfItemText})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var ms []measured
	for i, r := range []float64{5, 6, 7, 8} {
		ms = append(ms, measured{id: fmt.Sprintf("%s_u%d", account, i), account: account, postType: "utile", sourceItem: "it_" + account,
			rate: r, text: fmt.Sprintf("Astuce fictive numéro %d", i)})
	}
	for i, r := range []float64{2, 2, 3, 3, 3} {
		ms = append(ms, measured{id: fmt.Sprintf("%s_o%d", account, i), account: account, rate: r, text: fmt.Sprintf("Note fictive %d", i)})
	}
	ms = append(ms, measured{id: account + "_reply", account: account, kind: "reply", rate: 1.5, text: "Ma réponse fictive"})
	e.publish(t, ms...)
	if err := e.st.store.Update(func(d *Data) error {
		d.Post(account + "_reply").TargetText = perfTargetText
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func perfRules(d *Data) []*RuleProposal {
	var out []*RuleProposal
	for _, r := range d.RuleProposals {
		if r.Source == RuleSourcePerformance {
			out = append(out, r)
		}
	}
	return out
}

func TestRuleTextOK(t *testing.T) {
	cases := map[string]bool{
		"Privilégie une accroche concrète dès la première phrase.": true,
		strings.Repeat("é", 240):                                   true,
		strings.Repeat("é", 241):                                   false,
		"   ":                                                      false,
		"Deux lignes\nau lieu d'une":                               false,
		"Termine par <b>":                                          false,
		"Cite la source a > b":                                     false,
		"Ajoute un lien https://exemple.test":                      false,
		"Ajoute un lien HTTP://exemple.test":                       false,
		"Mentionne @nom":                                           false,
		"Invisible\u200bici":                                       false,
		"Invisible\u200fici":                                       false,
		"Sens\u202aforcé":                                          false,
		"Sens\u202eforcé":                                          false,
		"Joint\u2060ici":                                           false,
		"Marque\ufeffici":                                          false,
	}
	for text, ok := range cases {
		if err := ruleTextOK(text); (err == nil) != ok {
			t.Errorf("ruleTextOK(%q) = %v, attendu ok=%v", text, err, ok)
		}
	}
}

func TestAnalyzeSkipsWithoutDataAndCallsNoLLM(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.publish(t, measured{id: "p1", rate: 1}, measured{id: "p2", rate: 5}, measured{id: "p3", rate: 9})
	res, err := e.st.AnalyzePerformance(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if e.ai.calls != 0 {
		t.Fatalf("données insuffisantes : %d appel(s) IA, attendu 0", e.ai.calls)
	}
	if len(res.Accounts) != 1 || res.Accounts[0].Eligible != 3 || !strings.Contains(res.Accounts[0].Skipped, "3 sur 8") ||
		len(res.Accounts[0].Proposed) != 0 {
		t.Fatalf("résultat inattendu : %+v", res.Accounts[0])
	}
	if d := e.st.store.Snapshot(); len(d.RuleProposals) != 0 {
		t.Fatalf("aucune règle ne doit être créée : %+v", d.RuleProposals)
	}

	// assez de posts, mais aucun groupe ne s'écarte : pas d'appel non plus
	var flat []measured
	for i := 0; i < 6; i++ {
		flat = append(flat, measured{id: fmt.Sprintf("f%d", i), rate: 3})
	}
	e.publish(t, flat...)
	res, err = e.st.AnalyzePerformance(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if e.ai.calls != 0 || res.Accounts[0].Eligible != 9 || res.Accounts[0].Skipped == "" {
		t.Fatalf("sans signal : %d appel(s), résultat %+v", e.ai.calls, res.Accounts[0])
	}
	if _, err := e.st.AnalyzePerformance(ctx, "inconnu"); err == nil {
		t.Fatal("un compte inconnu doit renvoyer une erreur")
	}
}

func TestAnalyzeSkipsAccountAtRuleCap(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	perfFixture(t, e, "main")
	if err := e.st.store.Update(func(d *Data) error {
		for i := 0; i < perfMaxAccepted-1; i++ {
			acc := "main"
			if i%2 == 0 {
				acc = "" // les règles sans compte comptent aussi
			}
			d.RuleProposals = append(d.RuleProposals, &RuleProposal{ID: fmt.Sprintf("rp%d", i), Account: acc, Text: fmt.Sprintf("Règle de performance %d", i),
				Source: RuleSourcePerformance, Status: RuleAccepted})
		}
		// ne comptent pas : règles de retours, règles d'un autre compte, règles non acceptées
		for i := 0; i < 5; i++ {
			d.RuleProposals = append(d.RuleProposals,
				&RuleProposal{ID: fmt.Sprintf("rf%d", i), Text: fmt.Sprintf("Règle de retours %d", i), Status: RuleAccepted},
				&RuleProposal{ID: fmt.Sprintf("ro%d", i), Account: "autre", Text: fmt.Sprintf("Règle d'un autre compte %d", i), Source: RuleSourcePerformance, Status: RuleAccepted},
				&RuleProposal{ID: fmt.Sprintf("rr%d", i), Text: fmt.Sprintf("Règle retirée %d", i), Source: RuleSourcePerformance, Status: RuleRevoked})
		}
		d.Settings.Rules = []string{"Règle écrite à la main"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := perfAcceptedCount(e.st.store.Snapshot(), "MAIN"); n != perfMaxAccepted-1 {
		t.Fatalf("perfAcceptedCount = %d, attendu %d", n, perfMaxAccepted-1)
	}
	if _, err := e.st.AnalyzePerformance(ctx, "main"); err != nil || e.ai.calls != 1 {
		t.Fatalf("sous le plafond : err=%v, %d appel(s), attendu 1", err, e.ai.calls)
	}
	if err := e.st.store.Update(func(d *Data) error {
		d.RuleProposals = append(d.RuleProposals, &RuleProposal{ID: "rp_last", Account: "main", Text: "Règle de performance de trop",
			Source: RuleSourcePerformance, Status: RuleAccepted})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := len(e.st.store.Snapshot().RuleProposals)
	res, err := e.st.AnalyzePerformance(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if e.ai.calls != 1 || !strings.Contains(res.Accounts[0].Skipped, "plafond") || len(e.st.store.Snapshot().RuleProposals) != before {
		t.Fatalf("au plafond : %d appel(s), résultat %+v", e.ai.calls, res.Accounts[0])
	}
}

func TestAnalyzeProposesRulesWithComputedBasis(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	perfFixture(t, e, "main")
	if err := e.st.store.Update(func(d *Data) error { d.Settings.RulesAutoAccept = true; return nil }); err != nil {
		t.Fatal(err)
	}
	// la justification de l'IA avance ses propres chiffres : la base ne doit pas les reprendre
	e.ai.perf = func(string) []map[string]any {
		return []map[string]any{{"text": "Favorise les astuces concrètes tirées de la veille", "rationale": "99,9 % selon l'IA",
			"signals": []string{"type:utile", "source:veille", "type:utile"}}}
	}
	res, err := e.st.AnalyzePerformance(ctx, "MAIN")
	if err != nil {
		t.Fatal(err)
	}
	if e.ai.calls != 1 {
		t.Fatalf("%d appel(s) IA, attendu 1", e.ai.calls)
	}
	d := e.st.store.Snapshot()
	rules := perfRules(d)
	if len(res.Accounts) != 1 || res.Accounts[0].Account != "main" || res.Accounts[0].Eligible != 10 ||
		res.Accounts[0].Skipped != "" || len(res.Accounts[0].Proposed) != 3 || len(rules) != 3 {
		t.Fatalf("résultat inattendu : %+v ; règles %+v", res.Accounts[0], rules)
	}
	dg := ComputePerfDigest(d, "main", e.now)
	for _, r := range rules {
		if r.Status != RuleProposed || r.DecidedAt != nil {
			t.Errorf("règle %q : statut %s, attendu proposed malgré RulesAutoAccept", r.Text, r.Status)
		}
		if r.Account != "main" || r.ID == "" || r.CreatedAt.IsZero() || len(r.Signals) == 0 {
			t.Errorf("règle incomplète : %+v", r)
		}
		mustContain(t, "base de "+r.Text, r.Basis, "6,5 % d'engagement médian sur 4 posts", "contre 3,0 % pour l'ensemble du compte")
		if strings.Contains(r.Basis, "99,9") || strings.Contains(r.Basis, r.Rationale) {
			t.Errorf("la base ne doit venir que du code : %q", r.Basis)
		}
		want := map[string]bool{}
		for _, k := range r.Signals {
			sg := signalOf(dg, k)
			if sg == nil {
				t.Fatalf("clé de signal %q absente du résumé", k)
			}
			mustContain(t, "base de "+r.Text, r.Basis, sg.Label)
			for _, id := range sg.PostIDs {
				want[id] = true
			}
		}
		if len(r.Evidence) == 0 || len(r.Evidence) > 10 {
			t.Errorf("preuves : %v", r.Evidence)
		}
		for _, id := range r.Evidence {
			if d.Post(id) == nil || !want[id] {
				t.Errorf("preuve %q : pas un post des signaux cités", id)
			}
		}
	}
	last := rules[2]
	if len(last.Signals) != 2 || last.Signals[0] != "type:utile" || last.Signals[1] != "source:veille" ||
		!strings.Contains(last.Basis, " ; ") || len(last.Evidence) != 4 {
		t.Errorf("signaux dédoublonnés, une base par signal, preuves sans doublon : %+v", last)
	}
	if d.Settings.LastPerfAnalysisAt == nil || !d.Settings.LastPerfAnalysisAt.Equal(e.now) {
		t.Errorf("LastPerfAnalysisAt = %v, attendu %v", d.Settings.LastPerfAnalysisAt, e.now)
	}
	// journal : des nombres seulement, ni texte de post, ni identifiant, ni compte X
	msg := d.Log[len(d.Log)-1].Message
	mustContain(t, "journal", msg, "10 post(s)", "3 règle(s)")
	for _, bad := range []string{"@moi", "moi", "Astuce", "main_", "fictive"} {
		if strings.Contains(msg, bad) {
			t.Errorf("journal %q : contient %q", msg, bad)
		}
	}
}

func TestAnalyzeDropsInvalidProposals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	perfFixture(t, e, "main")
	if err := e.st.store.Update(func(d *Data) error {
		d.RuleProposals = append(d.RuleProposals, &RuleProposal{ID: "r_old", Account: "main", Text: "Privilégie les accroches courtes", Status: RuleRefused})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ok := []string{"type:utile"}
	e.ai.perf = func(string) []map[string]any {
		return []map[string]any{
			{"text": "Privilégie les formats inconnus", "rationale": "x", "signals": []string{"type:inconnu"}},
			{"text": "Privilégie les formats sans signal", "rationale": "x", "signals": []string{}},
			{"text": "PRIVILÉGIE LES ACCROCHES COURTES", "rationale": "x", "signals": ok},
			{"text": "Ajoute un lien https://exemple.test", "rationale": "x", "signals": ok},
			{"text": "Termine par <balise>", "rationale": "x", "signals": ok},
			{"text": "Privilégie\u200b les listes", "rationale": "x", "signals": ok},
			{"text": "Privilégie " + strings.Repeat("a", 240), "rationale": "x", "signals": ok},
			{"text": "Remercie @nom", "rationale": "x", "signals": ok},
			// le texte brut est vérifié avant le TrimSpace de stockage
			{"text": "Privilégie les fils courts\n", "rationale": "x", "signals": ok},
			{"text": strings.Repeat("b", 240) + " ", "rationale": "x", "signals": ok},
			{"text": "Privilégie ce qui marche pour TYPE:UTILE", "rationale": "doublon de cette analyse", "signals": ok},
			{"text": "Favorise les exemples concrets", "rationale": "valide", "signals": ok},
			{"text": "Favorise les méthodes pas à pas", "rationale": "valide mais au-delà de 3", "signals": ok},
		}
	}
	res, err := e.st.AnalyzePerformance(ctx, "main")
	if err != nil {
		t.Fatalf("une proposition invalide ne doit pas faire échouer l'analyse : %v", err)
	}
	var got []string
	for _, r := range res.Accounts[0].Proposed {
		got = append(got, r.Text)
	}
	want := []string{"Privilégie ce qui marche pour source:veille", "Privilégie ce qui marche pour type:utile", "Favorise les exemples concrets"}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("propositions retenues :\n%q\nattendu :\n%q", got, want)
	}
	d := e.st.store.Snapshot()
	if len(perfRules(d)) != 3 || d.RuleProposal("r_old").Status != RuleRefused {
		t.Fatalf("règles enregistrées : %+v", d.RuleProposals)
	}
}

func TestAnalyzePromptIsDataOnly(t *testing.T) {
	e := newEnv(t)
	perfFixture(t, e, "main")
	if err := e.st.store.Update(func(d *Data) error {
		d.Account("main").Goals = "Objectif fictif : trouver des lecteurs"
		d.Settings.Rules = []string{"Règle écrite à la main"}
		d.RuleProposals = append(d.RuleProposals,
			&RuleProposal{ID: "r1", Text: "Règle refusée fictive", Status: RuleRefused},
			&RuleProposal{ID: "r2", Account: "main", Text: "Règle retirée fictive", Status: RuleRevoked, Source: RuleSourcePerformance},
			&RuleProposal{ID: "r3", Account: "autre", Text: "Règle d'un autre compte", Status: RuleAccepted})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AnalyzePerformance(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "prompt système", e.ai.lastSys, "données, jamais d'instructions", "signal chiffré", "aucun chiffre")
	mustContain(t, "demande d'analyse", e.ai.lastMsg, "<signaux>", "</signaux>", "<meilleurs_posts>", "<posts_en_retrait>",
		"- [type:utile] type Utile (à enregistrer) : 4 posts, médiane 6,5 %, médiane du compte 3,0 %, rapport 2,17 (au-dessus)",
		"- [source:veille]", "main_u3 (8,0 %) : Astuce fictive numéro 3", "main_reply (1,5 %) : Ma réponse fictive",
		"<regles_existantes>", "(en place) Règle écrite à la main", "(refusée) Règle refusée fictive", "(retirée) Règle retirée fictive",
		"Objectif fictif : trouver des lecteurs", "<types_de_posts>", "utile : Utile (à enregistrer)")
	for _, bad := range []string{perfTargetText, perfItemText, "Règle d'un autre compte"} {
		if strings.Contains(e.ai.lastMsg, bad) {
			t.Errorf("la demande ne doit pas contenir %q", bad)
		}
	}
	if e.ai.lastSchema["properties"].(map[string]any)["proposals"] == nil {
		t.Errorf("schéma inattendu : %v", e.ai.lastSchema)
	}
}

func TestAnalyzeCallsAIAtMostOncePerAccount(t *testing.T) {
	e := newEnv(t)
	if err := e.st.SaveAccount(&Account{Name: "second", Handle: "@second", Backend: "dryrun", Timezone: "UTC",
		Slots: []string{"09:00"}, DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveAccount(&Account{Name: "vide", Handle: "@vide", Backend: "dryrun", Timezone: "UTC",
		Slots: []string{"09:00"}, DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	perfFixture(t, e, "main")
	perfFixture(t, e, "second")
	if err := e.st.store.Update(func(d *Data) error {
		d.Settings.Rules = []string{"Règle commune fictive"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// en plus des propositions par défaut (mêmes textes pour les deux
	// comptes), une règle propre à chaque compte et une règle déjà en place
	e.ai.perf = func(user string) []map[string]any {
		own := "Règle propre au compte main"
		if strings.Contains(user, "second_") {
			own = "Règle propre au compte second"
		}
		return []map[string]any{
			{"text": "  RÈGLE COMMUNE FICTIVE ", "rationale": "x", "signals": []string{"type:utile"}},
			{"text": own, "rationale": "x", "signals": []string{"type:utile"}},
		}
	}
	res, err := e.st.AnalyzePerformance(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if e.ai.calls != 2 || len(res.Accounts) != 3 {
		t.Fatalf("%d appel(s) IA pour %d compte(s), attendu 2 pour 3", e.ai.calls, len(res.Accounts))
	}
	// les textes proposés à « main » ne sont pas reproposés à « second » :
	// le dédoublonnage vaut pour tous les comptes (critère 5 du ticket)
	wantTexts := map[string][]string{
		"main":   {"Privilégie ce qui marche pour source:veille", "Privilégie ce qui marche pour type:utile", "Règle propre au compte main"},
		"second": {"Règle propre au compte second"},
	}
	for _, ar := range res.Accounts {
		if ar.Account == "vide" {
			if ar.Skipped == "" || len(ar.Proposed) != 0 {
				t.Errorf("compte sans posts : %+v", ar)
			}
			continue
		}
		for _, r := range ar.Proposed {
			if r.Account != ar.Account {
				t.Errorf("règle de %q rangée sous %q", ar.Account, r.Account)
			}
			for _, id := range r.Evidence {
				if !strings.HasPrefix(id, ar.Account+"_") {
					t.Errorf("preuve %q d'un autre compte que %q", id, ar.Account)
				}
			}
		}
		var got []string
		for _, r := range ar.Proposed {
			got = append(got, r.Text)
		}
		sort.Strings(got)
		if strings.Join(got, "|") != strings.Join(wantTexts[ar.Account], "|") {
			t.Errorf("%s : règles %q, attendu %q", ar.Account, got, wantTexts[ar.Account])
		}
	}
	// une règle refusée pour « second » n'est pas reproposée à « main »
	if err := e.st.store.Update(func(d *Data) error {
		d.RuleProposals = append(d.RuleProposals, &RuleProposal{ID: "r_second", Account: "second", Text: "Favorise les exemples concrets", Status: RuleRefused})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.ai.perf = func(string) []map[string]any {
		return []map[string]any{{"text": "FAVORISE les exemples concrets", "rationale": "x", "signals": []string{"type:utile"}}}
	}
	res, err = e.st.AnalyzePerformance(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ar := range res.Accounts {
		if len(ar.Proposed) != 0 {
			t.Errorf("seconde analyse, %s : %d règle(s), attendu 0", ar.Account, len(ar.Proposed))
		}
	}
	// une ligne de journal par compte et par analyse
	n := 0
	for _, l := range e.st.store.Snapshot().Log {
		if strings.HasPrefix(l.Message, "analyse des performances") {
			n++
		}
	}
	if n != 6 {
		t.Errorf("%d ligne(s) de journal, attendu 6", n)
	}
}

// Une analyse lancée plus tôt qui finit après une autre ne fait pas reculer
// LastPerfAnalysisAt (qui sert au déclenchement hebdomadaire).
func TestAnalyzeKeepsLatestAnalysisDate(t *testing.T) {
	e := newEnv(t)
	perfFixture(t, e, "main")
	early := e.now
	late := early.Add(time.Minute)
	nested := false
	e.ai.perf = func(string) []map[string]any {
		if !nested {
			// pendant l'appel IA de la première analyse, une seconde
			// analyse démarre plus tard et se termine avant elle
			nested = true
			e.now = late
			if _, err := e.st.AnalyzePerformance(context.Background(), "main"); err != nil {
				t.Error(err)
			}
		}
		return nil
	}
	if _, err := e.st.AnalyzePerformance(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	if e.ai.calls != 2 {
		t.Fatalf("%d appel(s) IA, attendu 2", e.ai.calls)
	}
	got := e.st.store.Snapshot().Settings.LastPerfAnalysisAt
	if got == nil || !got.Equal(late) {
		t.Errorf("LastPerfAnalysisAt = %v, attendu %v (pas %v)", got, late, early)
	}
}

// ---------------------------------------------------------------- planificateur, API et CLI (LOT1-T4)

// recentPerf : rend les 10 posts de perfFixture « récents » pour le
// planificateur. Les 5 premiers passent à 3 jours de la date courante, les
// autres restent à 10 jours ; tous les relevés sont du jour, pour que
// RefreshMetrics (appelé par Tick avant l'analyse) ne les remplace pas.
func recentPerf(t *testing.T, e *env, lastAnalysis time.Time) {
	t.Helper()
	if err := e.st.store.Update(func(d *Data) error {
		recent := e.now.Add(-3 * 24 * time.Hour)
		n := 0
		for _, p := range d.Posts {
			if n < 5 {
				p.PublishedAt = &recent
				n++
			}
			p.Metrics.UpdatedAt = e.now
		}
		d.Settings.LastPerfAnalysisAt = &lastAnalysis
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPerfAnalysisDue(t *testing.T) {
	e := newEnv(t)
	perfFixture(t, e, "main") // 10 posts publiés il y a 10 jours
	due := func() bool { return perfAnalysisDue(e.st.store.Snapshot(), e.now) }
	if due() {
		t.Fatal("désactivée par défaut")
	}
	e.tune(t, nil, func(s *Settings) { s.AutoPerfAnalysis = true })
	if !due() {
		t.Fatal("jamais faite, 10 posts publiés : due")
	}
	recentPerf(t, e, e.now.Add(-7*24*time.Hour)) // 5 posts publiés depuis, il y a 7 jours pile
	if !due() {
		t.Fatal("7 jours et 5 posts depuis : due")
	}
	recentPerf(t, e, e.now.Add(-7*24*time.Hour+time.Minute))
	if due() {
		t.Fatal("moins de 7 jours : pas due")
	}
	_ = e.st.store.Update(func(d *Data) error {
		d.Posts[0].Status = StatusFailed // plus que 4 posts publiés depuis
		return nil
	})
	recentPerf(t, e, e.now.Add(-8*24*time.Hour))
	if due() {
		t.Fatal("4 posts publiés depuis la dernière analyse : pas due")
	}
}

// failingAI : IA qui répond toujours une erreur (msg, ou un texte par défaut).
type failingAI struct {
	calls int
	msg   string
}

func (f *failingAI) JSON(context.Context, string, string, map[string]any, any) error {
	f.calls++
	if f.msg != "" {
		return errors.New(f.msg)
	}
	return errors.New("IA indisponible")
}

// Test 7 du §4.3.1.
func TestPerfAnalysisDueInTick(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	perfFixture(t, e, "main")
	recentPerf(t, e, e.now.Add(-7*24*time.Hour))

	if _, ok := e.st.Tick(ctx)["perf"]; ok || e.ai.calls != 0 {
		t.Fatalf("analyse désactivée : %d appel(s) IA dans Tick", e.ai.calls)
	}
	e.tune(t, nil, func(s *Settings) { s.AutoPerfAnalysis = true })
	out := e.st.Tick(ctx)
	res, ok := out["perf"].(*PerfResult)
	if !ok || e.ai.calls != 1 {
		t.Fatalf("activée, 5 posts récents, dernière analyse à 7 jours : %d appel(s) IA, sortie %v", e.ai.calls, out)
	}
	if len(res.Accounts) != 1 || res.Accounts[0].Eligible != 10 || len(res.Accounts[0].Proposed) == 0 {
		t.Fatalf("résultat : %+v", res.Accounts)
	}
	if last := e.st.store.Snapshot().Settings.LastPerfAnalysisAt; last == nil || !last.Equal(e.now) {
		t.Fatalf("LastPerfAnalysisAt = %v", last)
	}
	if _, ok := e.st.Tick(ctx)["perf"]; ok || e.ai.calls != 1 {
		t.Fatalf("second Tick immédiat : %d appel(s) IA, attendu 1", e.ai.calls)
	}

	// une erreur de l'IA est journalisée sans interrompre le Tick ; la date
	// ne bouge pas, l'analyse sera retentée. Le message du fournisseur (ici un
	// marqueur fictif imitant un corps de réponse HTTP) n'entre pas au journal.
	const marker = "MARQUEUR-FICTIF-7f3a"
	bad := &failingAI{msg: "HTTP 400 : {\"post\":\"" + marker + "\",\"compte\":\"@" + marker + "\"}"}
	e.st.ai = func(Settings) AI { return bad }
	recentPerf(t, e, e.now.Add(-8*24*time.Hour))
	before := *e.st.store.Snapshot().Settings.LastPerfAnalysisAt
	out = e.st.Tick(ctx)
	if _, ok := out["perf"]; ok || bad.calls != 1 || out["at"] == nil {
		t.Fatalf("erreur IA : sortie %v, %d appel(s)", out, bad.calls)
	}
	d := e.st.store.Snapshot()
	if l := d.Log[len(d.Log)-1]; l.Level != "error" || !strings.HasPrefix(l.Message, "analyse des performances : ") {
		t.Fatalf("journal : %+v", l)
	}
	for _, l := range d.Log {
		if strings.Contains(l.Message, marker) {
			t.Fatalf("message du fournisseur IA recopié au journal : %+v", l)
		}
	}
	if !d.Settings.LastPerfAnalysisAt.Equal(before) {
		t.Fatalf("LastPerfAnalysisAt modifiée malgré l'échec : %v", d.Settings.LastPerfAnalysisAt)
	}
}

// Le journal de l'analyse automatique ne reçoit qu'une catégorie fixe.
func TestPerfErrorKindIsFixed(t *testing.T) {
	const marker = "MARQUEUR-FICTIF-7f3a"
	for _, err := range []error{
		errors.New("sortie de commande : " + marker),
		fmt.Errorf("appel IA : %w : %s", context.DeadlineExceeded, marker),
		fmt.Errorf("appel IA : %w : %s", context.Canceled, marker),
	} {
		k := perfErrorKind(err)
		if k == "" || strings.Contains(k, marker) {
			t.Fatalf("catégorie %q pour %v", k, err)
		}
	}
	if perfErrorKind(fmt.Errorf("x : %w", context.DeadlineExceeded)) == perfErrorKind(errors.New("x")) {
		t.Fatal("un délai dépassé doit avoir sa propre catégorie")
	}
}

// Test 10 du §4.3.1.
func TestAPIAnalyzeAndState(t *testing.T) {
	srv, e := tuningServer(t)
	c := &http.Client{}
	perfFixture(t, e, "main")

	st, m := call(t, c, "POST", srv.URL+"/api/rules/analyze", `{"account":"main"}`)
	if st != 200 || e.ai.calls != 1 {
		t.Fatalf("analyse : %d %v (%d appel(s) IA)", st, m, e.ai.calls)
	}
	accounts := m["data"].(map[string]any)["accounts"].([]any)
	acc := accounts[0].(map[string]any)
	proposed := acc["proposed"].([]any)
	if len(accounts) != 1 || acc["account"] != "main" || acc["eligible"].(float64) != 10 || acc["skipped"] != nil || len(proposed) != 2 {
		t.Fatalf("résultat de l'analyse : %v", acc)
	}
	stored := perfRules(e.st.store.Snapshot())
	if len(stored) != 2 {
		t.Fatalf("règles enregistrées : %+v", stored)
	}
	for i, p := range proposed {
		r := p.(map[string]any)
		if r["id"] != stored[i].ID || r["source"] != RuleSourcePerformance || r["status"] != RuleProposed || r["basis"] != stored[i].Basis {
			t.Fatalf("proposition %d : %v, enregistrée %+v", i, r, stored[i])
		}
	}

	// sans corps : tous les comptes ; compte inconnu : erreur
	if st, m := call(t, c, "POST", srv.URL+"/api/rules/analyze", ""); st != 200 || len(m["data"].(map[string]any)["accounts"].([]any)) != 1 {
		t.Fatalf("analyse sans corps : %d %v", st, m)
	}
	calls := e.ai.calls
	if st, _ := call(t, c, "POST", srv.URL+"/api/rules/analyze", `{"account":"inconnu"}`); st != 400 || e.ai.calls != calls {
		t.Fatalf("compte inconnu : %d", st)
	}

	// l'état expose le résumé des performances, sans appel IA
	_, m = call(t, c, "GET", srv.URL+"/api/state", "")
	perf, ok := m["data"].(map[string]any)["perf"].(map[string]any)
	if !ok || e.ai.calls != calls {
		t.Fatalf("état : perf absent ou appel IA (%d)", e.ai.calls)
	}
	dg := e.digest()
	pm := perf["main"].(map[string]any)
	if pm["eligible"].(float64) != float64(dg.Eligible) || pm["baseline"].(float64) != dg.Baseline ||
		len(pm["signals"].([]any)) != len(dg.Signals) || len(dg.Signals) != 2 {
		t.Fatalf("perf.main = %v, attendu %+v", pm, dg)
	}

	// accepter une règle de performance par la route existante
	rule := stored[0]
	if st, m := call(t, c, "POST", srv.URL+"/api/rules/"+rule.ID+"/accept", `{}`); st != 200 || m["data"].(map[string]any)["status"] != RuleAccepted {
		t.Fatalf("acceptation : %d %v", st, m)
	}
	d := e.st.store.Snapshot()
	sys := systemPrompt(d.Account("main"), d)
	i, j := strings.Index(sys, "<regles>"), strings.Index(sys, "</regles>")
	if i < 0 || j < i || !strings.Contains(sys[i:j], "- "+rule.Text+"\n") {
		t.Fatalf("la règle acceptée doit figurer dans <regles> : %q", sys)
	}
	if strings.Contains(sys, stored[1].Text) {
		t.Fatal("une règle seulement proposée ne doit pas entrer dans le prompt")
	}
}

// Critère 4 du ticket : le client ne peut pas écraser LastPerfAnalysisAt.
func TestAPISettingsKeepLastPerfAnalysisAt(t *testing.T) {
	srv, e := tuningServer(t)
	c := &http.Client{}
	body := `{"autoPerfAnalysis":true,"lastPerfAnalysisAt":"2020-01-01T00:00:00Z"}`
	if st, m := call(t, c, "POST", srv.URL+"/api/settings", body); st != 200 {
		t.Fatalf("réglages refusés : %v", m)
	}
	if s := e.st.store.Snapshot().Settings; !s.AutoPerfAnalysis || s.LastPerfAnalysisAt != nil {
		t.Fatalf("jamais analysé : %v / %v", s.AutoPerfAnalysis, s.LastPerfAnalysisAt)
	}
	// une date déjà enregistrée n'est pas écrasée non plus (la fusion JSON
	// écrivait dans la valeur pointée) ; même garantie pour LastSynthesisAt
	last := e.now.Add(-time.Hour)
	_ = e.st.store.Update(func(d *Data) error {
		perf, synth := last, last
		d.Settings.LastPerfAnalysisAt, d.Settings.LastSynthesisAt = &perf, &synth
		return nil
	})
	body = `{"lastPerfAnalysisAt":"2020-01-01T00:00:00Z","lastSynthesisAt":"2020-01-01T00:00:00Z"}`
	if st, m := call(t, c, "POST", srv.URL+"/api/settings", body); st != 200 {
		t.Fatalf("réglages refusés : %v", m)
	}
	s := e.st.store.Snapshot().Settings
	if s.LastPerfAnalysisAt == nil || !s.LastPerfAnalysisAt.Equal(last) {
		t.Fatalf("LastPerfAnalysisAt = %v, attendu %v", s.LastPerfAnalysisAt, last)
	}
	if s.LastSynthesisAt == nil || !s.LastSynthesisAt.Equal(last) {
		t.Fatalf("LastSynthesisAt = %v, attendu %v", s.LastSynthesisAt, last)
	}
}

// cliExit : code de sortie d'une commande, remonté par panique dans les tests.
type cliExit int

// runCommand exécute une commande de la CLI et renvoie son code de sortie et
// son enveloppe JSON, sans quitter le processus de test.
func runCommand(t *testing.T, run func()) (code int, out map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	oldOut, oldExit := stdout, exit
	stdout, exit = &buf, func(c int) { panic(cliExit(c)) }
	defer func() { stdout, exit = oldOut, oldExit }()
	func() {
		defer func() {
			r := recover()
			c, ok := r.(cliExit)
			if !ok {
				panic(r)
			}
			code = int(c)
		}()
		run()
	}()
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("enveloppe illisible %q : %v", buf.String(), err)
	}
	return code, out
}

// sansID : résultat d'analyse sans les identifiants aléatoires des règles.
func sansID(t *testing.T, v any) string {
	t.Helper()
	raw, _ := json.Marshal(v)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, a := range m["accounts"].([]any) {
		for _, r := range a.(map[string]any)["proposed"].([]any) {
			delete(r.(map[string]any), "id")
		}
	}
	raw, _ = json.Marshal(m)
	return string(raw)
}

// Test 11 du §4.3.1.
func TestCLIRulesAnalyze(t *testing.T) {
	ctx := context.Background()
	// même studio fictif des deux côtés : l'API sert de référence
	srv, ref := tuningServer(t)
	perfFixture(t, ref, "main")
	_, m := call(t, &http.Client{}, "POST", srv.URL+"/api/rules/analyze", `{"account":"main"}`)
	want := sansID(t, m["data"])

	e := newEnv(t)
	perfFixture(t, e, "main")
	code, out := runCommand(t, func() { cmdRules(ctx, e.st, parseArgs([]string{"analyze", "--account", "main"})) })
	if code != 0 || out["ok"] != true || out["schema_version"] != "1" || out["error"] != nil {
		t.Fatalf("enveloppe : code %d, %v", code, out)
	}
	if got := sansID(t, out["data"]); got != want || e.ai.calls != 1 {
		t.Fatalf("contenu CLI :\n%s\nattendu (API) :\n%s", got, want)
	}
	stored := perfRules(e.st.store.Snapshot())
	proposed := out["data"].(map[string]any)["accounts"].([]any)[0].(map[string]any)["proposed"].([]any)
	if len(stored) != 2 || len(proposed) != 2 || proposed[0].(map[string]any)["id"] != stored[0].ID {
		t.Fatalf("règles enregistrées : %+v", stored)
	}

	// compte inconnu : enveloppe d'erreur, code 1
	code, out = runCommand(t, func() { cmdRules(ctx, e.st, parseArgs([]string{"analyze", "-a", "inconnu"})) })
	if code != 1 || out["ok"] != false || out["error"].(map[string]any)["code"] != "error" {
		t.Fatalf("compte inconnu : code %d, %v", code, out)
	}

	// réglage --auto-perf-analysis[=false]
	if code, out := runCommand(t, func() { cmdSettings(e.st.store, parseArgs([]string{"--auto-perf-analysis"})) }); code != 0 ||
		out["data"].(map[string]any)["autoPerfAnalysis"] != true || !e.st.store.Snapshot().Settings.AutoPerfAnalysis {
		t.Fatalf("--auto-perf-analysis : %v", out)
	}
	if code, _ := runCommand(t, func() { cmdSettings(e.st.store, parseArgs([]string{"--auto-perf-analysis=false"})) }); code != 0 ||
		e.st.store.Snapshot().Settings.AutoPerfAnalysis {
		t.Fatal("--auto-perf-analysis=false doit désactiver l'analyse")
	}
	if !strings.Contains(usageText, "rules analyze [--account c]") || !strings.Contains(usageText, "--auto-perf-analysis[=false]") {
		t.Fatal("aide non mise à jour")
	}
}
