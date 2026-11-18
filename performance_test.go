package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// measured décrit un post publié et mesuré (données fictives).
type measured struct {
	id, kind, postType, text, sourceItem string
	rate                                 float64
	views                                int
	measuredAfter                        time.Duration // délai entre la publication et le relevé
	tweetID                              string        // vide = identifiant réel fictif
	noMetrics                            bool
}

// publish ajoute des posts publiés et mesurés au compte « main ».
func (e *env) publish(t *testing.T, ms ...measured) {
	t.Helper()
	if err := e.st.store.Update(func(d *Data) error {
		for _, m := range ms {
			pub := e.now.Add(-10 * 24 * time.Hour)
			p := &Post{ID: m.id, Account: "main", Kind: m.kind, Text: m.text, Status: StatusPublished, PublishedAt: &pub,
				TweetID: m.tweetID, SourceItem: m.sourceItem, PostType: m.postType, CreatedAt: pub, UpdatedAt: pub}
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
