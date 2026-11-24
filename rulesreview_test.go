package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func addRules(t *testing.T, e *env, rs ...*RuleProposal) {
	t.Helper()
	if err := e.st.store.Update(func(d *Data) error {
		d.RuleProposals = append(d.RuleProposals, rs...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func accepted(id, account, text string, at time.Time) *RuleProposal {
	return &RuleProposal{ID: id, Account: account, Text: text, Status: RuleAccepted, CreatedAt: at, DecidedAt: &at}
}

// Les règles d'un ancien data.json migrent sans perte : mêmes textes, mêmes
// statuts, aucune règle désactivée ; échéance calculée depuis l'acceptation.
func TestRulesMigrationKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":2,"settings":{"aiProvider":"claude-code","ruleReviewDays":0},
"accounts":[{"name":"main","handle":"moi","backend":"dryrun","timezone":"UTC","slots":["09:00"],"dailyLimit":3,"watch":[]}],
"posts":[],"items":[],"log":[],"auth":{},
"learnings":[{"id":"l_1","text":"trop long","createdAt":"2026-08-01T08:00:00Z","source":"manual"}],
"ruleProposals":[
{"id":"r_acc","account":"main","text":"Phrases courtes.","evidence":["l_1","l_gone"],"status":"accepted","createdAt":"2026-08-01T08:00:00Z","decidedAt":"2026-08-10T08:00:00Z"},
{"id":"r_nodate","text":"Sans date de décision.","status":"accepted","createdAt":"2026-08-05T08:00:00Z"},
{"id":"r_wait","text":"Pas de jargon.","status":"proposed","createdAt":"2026-11-01T08:00:00Z"},
{"id":"r_ref","text":"Refusée.","status":"refused","createdAt":"2026-11-01T08:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := store.Snapshot()
	if len(d.RuleProposals) != 4 {
		t.Fatalf("aucune règle ne doit disparaître : %d", len(d.RuleProposals))
	}
	acc := d.RuleProposal("r_acc")
	wantAt := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC).AddDate(0, 0, 90)
	if acc.Status != RuleAccepted || acc.Text != "Phrases courtes." || acc.ReviewAt == nil || !acc.ReviewAt.Equal(wantAt) {
		t.Fatalf("échéance depuis la date d'acceptation attendue (%v) : %+v", wantAt, acc)
	}
	if len(acc.Examples) != 1 || acc.Examples[0].Ref != "l_1" {
		t.Fatalf("exemple reconstitué depuis le retour encore présent : %+v", acc.Examples)
	}
	nd := d.RuleProposal("r_nodate")
	if nd.ReviewAt == nil || !nd.ReviewAt.Equal(nd.CreatedAt.AddDate(0, 0, 90)) {
		t.Fatalf("sans date d'acceptation, depuis la création : %+v", nd)
	}
	if d.RuleProposal("r_wait").ReviewAt != nil || d.RuleProposal("r_ref").Status != RuleRefused || d.RuleProposal("r_wait").Status != RuleProposed {
		t.Fatal("les règles non acceptées gardent statut et sans échéance")
	}
	// écrite sur disque à la version 3, relisible
	b, _ := os.ReadFile(filepath.Join(dir, "data.json"))
	var head struct{ Version int }
	if json.Unmarshal(b, &head); head.Version != dataVersion {
		t.Fatalf("version écrite : %d", head.Version)
	}
}

// Une règle échue est signalée, jamais désactivée : elle guide toujours l'IA.
func TestRuleDueIsFlaggedNotDisabled(t *testing.T) {
	e := newEnv(t)
	old := e.now.AddDate(0, 0, -100)
	addRules(t, e, accepted("r_old", "main", "Phrases courtes.", old))
	if err := e.st.store.Update(func(d *Data) error { migrateRules(d); return nil }); err != nil {
		t.Fatal(err)
	}
	d := e.st.store.Snapshot()
	rv := ComputeRulesReview(d, e.now)
	if !reflect.DeepEqual(rv.Due, []string{"r_old"}) {
		t.Fatalf("règle échue attendue : %v", rv.Due)
	}
	if !RuleDue(d.RuleProposal("r_old"), e.now) || RuleDue(d.RuleProposal("r_old"), old.AddDate(0, 0, 10)) {
		t.Fatal("échue seulement après l'échéance")
	}
	e.tick(t)
	d = e.st.store.Snapshot()
	if r := d.RuleProposal("r_old"); r.Status != RuleAccepted || strings.Join(d.RulesFor("main"), "|") != "Phrases courtes." {
		t.Fatalf("une règle échue reste en place : %+v", r)
	}
	// réexaminer repousse l'échéance ; l'intervalle est réglable par règle
	r, err := e.st.SetRuleReview("r_old", 30)
	if err != nil {
		t.Fatal(err)
	}
	if r.ReviewDays != 30 || !r.ReviewAt.Equal(e.now.AddDate(0, 0, 30)) || r.Text != "Phrases courtes." || r.Status != RuleAccepted {
		t.Fatalf("échéance réglée sans toucher au texte ni au statut : %+v", r)
	}
	if _, err := e.st.SetRuleReview("r_old", 5000); err == nil {
		t.Fatal("échéance hors plafond refusée")
	}
}

func (e *env) tick(t *testing.T) {
	t.Helper()
	e.st.Tick(t.Context())
}

// Le calcul de révision ne modifie rien ; la fusion exige une décision explicite.
func TestReviewProposesAndNeverModifies(t *testing.T) {
	e := newEnv(t)
	at := e.now.AddDate(0, 0, -10)
	addRules(t, e,
		accepted("r1", "main", "Écris des phrases courtes et directes.", at),
		accepted("r2", "main", "Écris des phrases directes et courtes.", at.Add(time.Hour)),
		accepted("r3", "main", "Évite les longues phrases.", at),
		accepted("r4", "main", "Écris des phrases longues avec des détails.", at),
	)
	before, _ := json.Marshal(e.st.store.Snapshot().RuleProposals)
	rv := ComputeRulesReview(e.st.store.Snapshot(), e.now)
	if len(rv.Duplicates) == 0 || rv.Duplicates[0].IDs[0] != "r1" || rv.Duplicates[0].IDs[1] != "r2" {
		t.Fatalf("doublon r1/r2 attendu : %+v", rv.Duplicates)
	}
	found := false
	for _, c := range rv.Contradictions {
		if (c.IDs[0] == "r1" && c.IDs[1] == "r4") || (c.IDs[0] == "r4" && c.IDs[1] == "r1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("contradiction r1/r4 (court / long) attendue : %+v", rv.Contradictions)
	}
	e.tick(t)
	after, _ := json.Marshal(e.st.store.Snapshot().RuleProposals)
	if string(before) != string(after) {
		t.Fatal("la révision et le tick ne doivent modifier aucune règle")
	}
	// fusion : validée par l'utilisateur seulement
	got, err := e.st.MergeRules([]string{"r1", "r2"}, "Écris court et direct.")
	if err != nil {
		t.Fatal(err)
	}
	d := e.st.store.Snapshot()
	if got.ID != "r1" || got.Text != "Écris court et direct." || d.RuleProposal("r2").Status != RuleRevoked || d.RuleProposal("r3").Status != RuleAccepted {
		t.Fatalf("fusion : r1 gardée, r2 retirée, r3 intacte : %+v", got)
	}
	if _, err := e.st.MergeRules([]string{"r1", "r2"}, ""); err == nil {
		t.Fatal("une règle déjà retirée ne se refusionne pas")
	}
	if _, err := e.st.MergeRules([]string{"r1"}, ""); err == nil {
		t.Fatal("deux règles au moins")
	}
	addRules(t, e, accepted("r5", "autre", "Phrases courtes.", at))
	if _, err := e.st.MergeRules([]string{"r1", "r5"}, ""); err == nil {
		t.Fatal("comptes différents refusés")
	}
}

// Les exemples sont copiés à la proposition et survivent à la suppression du retour.
func TestSynthesisKeepsExamples(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error {
		d.Learnings = append(d.Learnings,
			Learning{ID: "l_a", Text: "trop long", CreatedAt: e.now, Source: "manual"},
			Learning{ID: "l_b", Text: "trop long encore", CreatedAt: e.now, Source: "manual"})
		return nil
	})
	if _, err := e.st.Synthesize(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if err := e.st.store.Update(func(d *Data) error { d.Learnings = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	var r *RuleProposal
	for _, x := range e.st.store.Snapshot().RuleProposals {
		if x.Text == "Règle commune" {
			r = x
		}
	}
	if r == nil || len(r.Examples) != 2 || !strings.Contains(r.Examples[0].Text, "trop long") {
		t.Fatalf("exemples conservés malgré la suppression des retours : %+v", r)
	}
	if r.ReviewAt != nil {
		t.Fatal("une règle proposée n'a pas d'échéance avant acceptation")
	}
	acc, err := e.st.DecideRule(r.ID, "accept", "")
	if err != nil || acc.ReviewAt == nil || !acc.ReviewAt.Equal(e.now.AddDate(0, 0, 90)) {
		t.Fatalf("échéance posée à l'acceptation : %+v %v", acc, err)
	}
}

// La comparaison avant/après est descriptive et marque l'absence de mesure.
func TestRuleCompareIsDescriptive(t *testing.T) {
	e := newEnv(t)
	at := e.now.AddDate(0, 0, -10)
	addRules(t, e, accepted("r1", "main", "Reste court.", at))
	pub := func(id string, when time.Time) {
		w := when
		if err := e.st.store.Update(func(d *Data) error {
			d.Posts = append(d.Posts, &Post{ID: id, Account: "main", Text: "x", Status: StatusPublished, PublishedAt: &w, TweetID: "t" + id,
				Metrics: &Metrics{Views: 100, Likes: 5, EngagementRate: 5, UpdatedAt: w.Add(72 * time.Hour)}})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	pub("p1", at.AddDate(0, 0, -5))
	pub("p2", at.AddDate(0, 0, 3))
	pub("p3", at.AddDate(0, 0, -50)) // hors fenêtre
	c := ComputeRulesReview(e.st.store.Snapshot(), e.now).Compare["r1"]
	if c.Before.Posts != 1 || c.After.Posts != 1 || c.Before.MedianEngagement == nil {
		t.Fatalf("un post de chaque côté, le hors-fenêtre exclu : %+v", c)
	}
	if !strings.Contains(c.Note, "ne prouve pas") {
		t.Fatalf("note descriptive attendue : %q", c.Note)
	}
	// aucun post après : « indisponible » (nil), pas zéro
	e2 := newEnv(t)
	addRules(t, e2, accepted("r1", "main", "Reste court.", at))
	c = ComputeRulesReview(e2.st.store.Snapshot(), e2.now).Compare["r1"]
	if c.After.Posts != 0 || c.After.MedianEngagement != nil || c.After.Views != nil {
		t.Fatalf("mesure absente = indisponible : %+v", c.After)
	}
}

// (a) une fusion avec un identifiant en double est refusée sans rien changer.
func TestMergeRulesDuplicateIDRefused(t *testing.T) {
	e := newEnv(t)
	addRules(t, e, accepted("r1", "main", "Reste court.", e.now.AddDate(0, 0, -5)))
	if _, err := e.st.MergeRules([]string{"r1", "r1"}, ""); err == nil {
		t.Fatal("un identifiant en double doit être refusé")
	}
	if r := e.st.store.Snapshot().RuleProposal("r1"); r.Status != RuleAccepted {
		t.Fatalf("la règle ne doit pas être retirée : %+v", r)
	}
	// une règle seulement proposée ne se fusionne pas
	addRules(t, e, &RuleProposal{ID: "r_p", Account: "main", Text: "Proposée.", Status: RuleProposed, CreatedAt: e.now})
	if _, err := e.st.MergeRules([]string{"r1", "r_p"}, ""); err == nil {
		t.Fatal("une règle non en place ne se fusionne pas")
	}
	if r := e.st.store.Snapshot().RuleProposal("r_p"); r.Status != RuleProposed {
		t.Fatalf("statut inchangé : %+v", r)
	}
}

// (b) acceptation d'office : la règle issue de la synthèse reçoit son échéance.
func TestAutoAcceptedRuleGetsReviewDate(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error {
		d.Settings.RulesAutoAccept = true
		d.Learnings = append(d.Learnings,
			Learning{ID: "l_a", Text: "trop long", CreatedAt: e.now, Source: "manual"},
			Learning{ID: "l_b", Text: "trop long encore", CreatedAt: e.now, Source: "manual"})
		return nil
	})
	if _, err := e.st.Synthesize(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	var r *RuleProposal
	for _, x := range e.st.store.Snapshot().RuleProposals {
		if x.Text == "Règle commune" {
			r = x
		}
	}
	if r == nil || r.Status != RuleAccepted || r.ReviewAt == nil || !r.ReviewAt.Equal(e.now.AddDate(0, 0, defaultRuleReviewDays)) {
		t.Fatalf("règle acceptée d'office avec échéance à 90 j : %+v", r)
	}
}

// (c) (d) SetRuleReview refuse une règle non en place ; retirer efface l'échéance.
func TestSetRuleReviewGuardsAndRevoke(t *testing.T) {
	e := newEnv(t)
	addRules(t, e,
		&RuleProposal{ID: "r_prop", Text: "Proposée.", Status: RuleProposed, CreatedAt: e.now},
		accepted("r_rev", "main", "Retirée.", e.now.AddDate(0, 0, -5)),
		accepted("r_ok", "main", "En place.", e.now.AddDate(0, 0, -5)))
	if _, err := e.st.DecideRule("r_rev", "revoke", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.SetRuleReview("r_prop", 10); err == nil {
		t.Fatal("règle proposée : échéance refusée")
	}
	if _, err := e.st.SetRuleReview("r_rev", 10); err == nil {
		t.Fatal("règle retirée : échéance refusée")
	}
	if r, err := e.st.SetRuleReview("r_ok", 10); err != nil || r.ReviewAt == nil {
		t.Fatalf("échéance posée avant le retrait : %v", err)
	}
	if _, err := e.st.DecideRule("r_ok", "revoke", ""); err != nil {
		t.Fatal(err)
	}
	if r := e.st.store.Snapshot().RuleProposal("r_ok"); r.Status != RuleRevoked || r.ReviewAt != nil {
		t.Fatalf("retirer efface l'échéance : %+v", r)
	}
}

// (e) bornes du réglage commun.
func TestNormalizeSettingsRuleReviewDaysBounds(t *testing.T) {
	for _, n := range []int{-1, 731} {
		if err := NormalizeSettings(&Settings{RuleReviewDays: n}); err == nil {
			t.Fatalf("%d jours doit être refusé", n)
		}
	}
	for _, n := range []int{0, 730} {
		if err := NormalizeSettings(&Settings{RuleReviewDays: n}); err != nil {
			t.Fatalf("%d jours doit être accepté : %v", n, err)
		}
	}
}

// (f) une règle propre à un compte ignore les posts des autres comptes ;
// les jours « après » sont ceux réellement écoulés (au plus 30).
func TestCompareRuleAccountScopeAndAfterDays(t *testing.T) {
	e := newEnv(t)
	at := e.now.AddDate(0, 0, -10)
	addRules(t, e, accepted("r1", "main", "Reste court.", at))
	_ = e.st.store.Update(func(d *Data) error {
		for id, acct := range map[string]string{"mine": "main", "other": "autre"} {
			w := at.AddDate(0, 0, -3)
			d.Posts = append(d.Posts, &Post{ID: id, Account: acct, Text: "x", Status: StatusPublished, PublishedAt: &w, TweetID: "t" + id})
		}
		return nil
	})
	c := ComputeRulesReview(e.st.store.Snapshot(), e.now).Compare["r1"]
	if c.Before.Posts != 1 {
		t.Fatalf("seul le post du compte concerné compte : %+v", c.Before)
	}
	if c.AfterDays != 10 {
		t.Fatalf("10 jours écoulés depuis l'acceptation : %d", c.AfterDays)
	}
	old := e.now.AddDate(0, 0, -100)
	addRules(t, e, accepted("r2", "main", "Autre.", old))
	if c := ComputeRulesReview(e.st.store.Snapshot(), e.now).Compare["r2"]; c.AfterDays != ruleCompareDays {
		t.Fatalf("plafonné à %d jours : %d", ruleCompareDays, c.AfterDays)
	}
}
