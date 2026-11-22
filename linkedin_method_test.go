package main

// Lot 3 LinkedIn : méthode (aides), anti-tics, ligne, plafond hebdomadaire,
// visuel. Aucun appel réseau LinkedIn : backends dryrun/manual ou faux serveur.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ticRules(tics []Tic) []string {
	var r []string
	for _, t := range tics {
		r = append(r, t.Rule)
	}
	return r
}

// C4 : table du détecteur.
func TestC4TicCheckTable(t *testing.T) {
	cases := []struct {
		name, text string
		rules      []string // règles attendues (vide = aucun tic)
		sev        string   // sévérité attendue de la première
	}{
		{"texte net", "Hier, un client m'a renvoyé mon devis avec trois lignes barrées. J'ai relu sans défendre mon prix.\n\nJe l'ai refait plus court.", nil, ""},
		{"deux tirets longs", "Le devis — trop long — est reparti.", []string{"tirets-longs"}, TicMedium},
		{"contraste", "Ce n'est pas du marketing, c'est de la confiance.", []string{"contraste"}, TicHigh},
		{"contraste en deux phrases", "Ce n'est pas une question de budget. C'est une question de temps.", []string{"contraste"}, TicHigh},
		{"ouverture creuse", "Dans un monde où tout va vite, je prends le temps.", []string{"ouverture-creuse"}, TicHigh},
		{"voici pourquoi", "Voici pourquoi j'ai arrêté.", []string{"ouverture-creuse"}, TicHigh},
		{"annonce", "Spoiler : ça n'a pas marché.", []string{"annonce"}, TicHigh},
		{"pont de révélation", "Le résultat ? Un client de moins.", []string{"annonce"}, TicHigh},
		{"enthousiasme creux", "Ravi de partager une nouvelle étape.", []string{"enthousiasme-creux"}, TicHigh},
		{"clôture appât", "Un devis refait en dix minutes.\n\nEt vous ?", []string{"cloture-appat"}, TicHigh},
		{"quatre hashtags", "Un devis refait.\n\n#a #b #c #d", []string{"hashtags"}, TicMedium},
		{"trois hashtags passent", "Un devis refait.\n\n#a #b #c", nil, ""},
		{"trois puces", "Trois choses :\n- le prix\n- le délai\n- la confiance", []string{"liste-trois"}, TicMedium},
		{"deux puces passent", "Deux choses :\n- le prix\n- le délai", nil, ""},
		{"emojis en puces", "Ce que j'ai changé :\n✅ le prix affiché\n🚀 le délai annoncé", []string{"emojis-puces"}, TicMedium},
		{"rafale", "Plus simple. Plus clair. Plus court. Voilà ce que j'ai fait cette semaine avec mes devis.", []string{"rafale-courte"}, TicMedium},
		{"accroche en question", "Vous savez ce qui coûte le plus cher ?\nUn devis flou.", []string{"accroche-creuse"}, TicMedium},
		{"x leçons", "3 leçons que j'ai apprises en dix ans.", []string{"x-lecons"}, TicMedium},
		{"mot accentué en fin de mot", "Une approche nuancée, robuste et holistique du devis.", []string{"vocabulaire-ia"}, TicMedium},
	}
	for _, c := range cases {
		got := TicCheck(c.text)
		if len(c.rules) == 0 {
			if len(got) != 0 {
				t.Errorf("%s : aucun tic attendu, obtenu %v", c.name, got)
			}
			continue
		}
		if len(got) != 1 {
			t.Errorf("%s : 1 tic attendu, obtenu %v", c.name, got)
			continue
		}
		if !reflect.DeepEqual(ticRules(got), c.rules) {
			t.Errorf("%s : règles %v, attendu %v", c.name, ticRules(got), c.rules)
			continue
		}
		if got[0].Severity != c.sev || got[0].Excerpt == "" {
			t.Errorf("%s : sévérité %q, extrait %q", c.name, got[0].Severity, got[0].Excerpt)
		}
	}
	// un seul tiret long réel : pas de tic
	if got := TicCheck("Le devis — trop long, je le sais. Je l'ai refait."); len(got) != 0 {
		t.Errorf("un seul tiret long ne doit pas déclencher : %v", got)
	}
}

// C5 : Validate LinkedIn, contrôle obligatoire, tic haut, accusé, texte modifié.
func TestC5ValidateRequiresTicCheck(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	p, _ := e.st.AddDraft("pro", "Un texte net, une seule idée.", "", "", "")
	// refusé sans contrôle
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err == nil || !strings.Contains(err.Error(), "anti-tics") {
		t.Fatalf("validation sans contrôle : %v", err)
	}
	// contrôle lancé : accepté
	if _, err := e.st.CheckTics(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatalf("validation après contrôle : %v", err)
	}

	// tic haut : refusé tant que non acquitté
	q, _ := e.st.AddDraft("pro", "Ce n'est pas du marketing, c'est de la confiance.", "", "", "")
	q, err := e.st.CheckTics(q.ID)
	if err != nil || len(highTics(q.Tics)) != 1 {
		t.Fatalf("contrôle : %v %v", q.Tics, err)
	}
	if _, err := e.st.Validate(q.ID, "Allan", nil, true); err == nil || !strings.Contains(err.Error(), "tics") {
		t.Fatalf("tic haut non acquitté : %v", err)
	}
	v, err := e.st.ValidateWith(q.ID, "Allan", nil, true, ValidateOptions{AckTics: true})
	if err != nil || !v.TicsAcknowledged || v.ValidatedBy != "Allan" {
		t.Fatalf("avec ackTics : %+v %v", v, err)
	}

	// le texte change ensuite : le contrôle tombe, la validation aussi
	e2, err := e.st.EditPost(q.ID, "Un autre texte, tout aussi net.")
	if err != nil || e2.Status != StatusDraft || e2.TicCheckedHash != "" || e2.TicsAcknowledged {
		t.Fatalf("après modification : %+v %v", e2, err)
	}
	if _, err := e.st.ValidateWith(q.ID, "Allan", nil, true, ValidateOptions{AckTics: true}); err == nil {
		t.Fatal("validation acceptée sans nouveau contrôle après modification du texte")
	}
	// texte changé hors EditPost (donnée forgée) : l'empreinte ne correspond plus
	_, _ = e.st.CheckTics(q.ID)
	_ = e.st.store.Update(func(d *Data) error { d.Post(q.ID).Text = "Texte glissé sans contrôle."; return nil })
	if _, err := e.st.Validate(q.ID, "Allan", nil, true); err == nil {
		t.Fatal("empreinte périmée acceptée")
	}
	if *calls != 0 {
		t.Fatalf("aucun backend ne doit être appelé : %d", *calls)
	}
	// le contrôle est réservé à LinkedIn
	x := e.draft(t, "post X")
	if _, err := e.st.CheckTics(x.ID); err == nil {
		t.Error("le contrôle anti-tics ne vaut que pour LinkedIn")
	}
}

// Un post X n'est pas soumis au contrôle (comportement X inchangé).
func TestC5XValidateUnchanged(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "Ce n'est pas du marketing, c'est de la confiance.")
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatalf("X : %v", err)
	}
}

// C6 : ligne vide ou hors liste.
func TestC6ValidateRefusedWithoutLine(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	p := e.liDraft(t, "Un texte net.")
	_ = e.st.store.Update(func(d *Data) error { d.Post(p.ID).Line = "autre"; return nil })
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err == nil || !strings.Contains(err.Error(), "ligne") {
		t.Fatalf("ligne hors liste : %v", err)
	}
	_ = e.st.store.Update(func(d *Data) error { d.Post(p.ID).Line = ""; d.Account("pro").Line = ""; return nil })
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err == nil {
		t.Fatal("ligne vide acceptée")
	}
}

// Changer l'étiquette remet la validation à zéro ; ligne inconnue refusée.
func TestSetLineResetsValidation(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	p := e.liDraft(t, "Un texte net.")
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatal(err)
	}
	q, err := e.st.SetLine(p.ID, "kinflux")
	if err != nil || q.Line != "kinflux" || q.Status != StatusDraft || q.ValidatedBy != "" {
		t.Fatalf("%+v %v", q, err)
	}
	if _, err := e.st.SetLine(p.ID, "inconnue"); err == nil {
		t.Error("ligne inconnue acceptée")
	}
	x := e.draft(t, "post X")
	if _, err := e.st.SetLine(x.ID, "perso"); err == nil {
		t.Error("une ligne n'existe pas sur un compte X")
	}
}

// Aucune promotion croisée : le prompt de chaque ligne interdit les autres.
func TestPromptForbidsCrossPromotion(t *testing.T) {
	d := &Data{}
	for _, line := range linkedinLines {
		sys := systemPrompt(&Account{Name: "x", Platform: "linkedin", Line: line}, d)
		if !strings.Contains(sys, "Aucune promotion croisée") || !strings.Contains(sys, "ligne "+line) {
			t.Errorf("ligne %s : consigne absente", line)
		}
		for _, other := range linkedinLines {
			listed := strings.Contains(sys, "("+strings.Join(othersOf(line), ", ")+")")
			if !listed {
				t.Errorf("ligne %s : les autres lignes (%v) doivent être citées comme interdites", line, othersOf(line))
			}
			_ = other
		}
	}
}

func othersOf(line string) []string {
	var o []string
	for _, l := range linkedinLines {
		if l != line {
			o = append(o, l)
		}
	}
	return o
}

// C2 + C3 : clauses de prompt et aides sans effet de bord.
func TestC2C3AidsClauseAndNoSideEffects(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	p := e.liDraft(t, "Hier, un client m'a renvoyé mon devis.\n\nJe l'ai refait plus court.")
	before := e.st.store.Snapshot().Post(p.ID)
	postsBefore := len(e.st.store.Snapshot().Posts)
	clause := []string{"La voix du compte l'emporte", "la voix de pro l'emporte", "pas de structure en trois points", "pas de liste à puces numérotée", "X leçons que j'ai apprises"}
	mustHaveClause := func(name string) {
		t.Helper()
		for _, c := range clause {
			if !strings.Contains(e.ai.lastMsg, c) {
				t.Errorf("%s : %q absent du prompt", name, c)
			}
		}
		if !strings.HasPrefix(e.ai.lastMsg, "La voix du compte l'emporte") {
			t.Errorf("%s : la clause doit précéder la consigne de méthode", name)
		}
		if !strings.Contains(e.ai.lastSys, "ligne perso") {
			t.Errorf("%s : le prompt système doit porter la ligne", name)
		}
	}
	ctx := context.Background()

	hooks, err := e.st.SuggestHooks(ctx, p.ID)
	if err != nil || len(hooks) != 3 {
		t.Fatalf("accroches : %v %v", hooks, err)
	}
	mustHaveClause("accroches")
	prop, err := e.st.Humanize(ctx, p.ID)
	if err != nil || prop.Text != "Texte humanisé" || len(prop.Changes) != 1 {
		t.Fatalf("humanizer : %+v %v", prop, err)
	}
	mustHaveClause("humanizer")
	rep, err := e.st.Audit(ctx, p.ID)
	if err != nil || len(rep.Suggestions) != 1 {
		t.Fatalf("audit : %+v %v", rep, err)
	}
	mustHaveClause("audit")
	ideas, err := e.st.PlanWeek(ctx, "pro")
	if err != nil || len(ideas) == 0 {
		t.Fatalf("plan : %v %v", ideas, err)
	}
	mustHaveClause("plan 7 jours")

	// C3 : texte et historique inchangés, rien d'appliqué
	after := e.st.store.Snapshot().Post(p.ID)
	if after.Text != before.Text || len(after.History) != 0 || after.Status != before.Status || after.TicCheckedHash != before.TicCheckedHash {
		t.Fatalf("une aide a modifié le post : %+v", after)
	}
	// C10 : le plan crée des idées, zéro post
	d := e.st.store.Snapshot()
	if len(d.Posts) != postsBefore {
		t.Fatalf("le plan a créé des posts : %d → %d", postsBefore, len(d.Posts))
	}
	for _, i := range ideas {
		if i.Status != IdeaProposed || i.PostID != "" || !strings.EqualFold(i.Account, "pro") {
			t.Errorf("idée %+v", i)
		}
	}
	if len(d.Ideas) != len(ideas) {
		t.Errorf("idées en base : %d", len(d.Ideas))
	}

	// l'Humanizer n'écrit que via « Appliquer » : une révision est ajoutée
	got, err := e.st.ApplyHumanized(p.ID, prop.Text)
	if err != nil || got.Text != "Texte humanisé" || len(got.History) != 1 || got.History[0].Text != before.Text || got.TicCheckedHash != "" {
		t.Fatalf("appliquer : %+v %v", got, err)
	}
	if len(e.st.store.Snapshot().Learnings) != 0 {
		t.Error("appliquer l'humanizer ne doit pas devenir une leçon d'Allan")
	}
}

// Accroches : « Appliquer » remplace la 1re ligne ; refus d'une accroche multiligne.
func TestApplyHookReplacesFirstLine(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	p := e.liDraft(t, "Première ligne.\n\nSuite du post.")
	got, err := e.st.ApplyHook(p.ID, "Nouvelle\naccroche")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Nouvelle accroche\n\nSuite du post." || len(got.History) != 1 || got.TicCheckedHash != "" {
		t.Fatalf("%q %+v", got.Text, got.History)
	}
	if _, err := e.st.ApplyHook(p.ID, "  "); err == nil {
		t.Error("accroche vide acceptée")
	}
	if _, err := e.st.ApplyHook(p.ID, strings.Repeat("a", 211)); err == nil {
		t.Error("accroche trop longue acceptée")
	}
	for _, bad := range []string{"<b>gras</b>", "Voir https://exemple.fr", "Un lien HTTP://x.fr"} {
		if _, err := e.st.ApplyHook(p.ID, bad); err == nil {
			t.Errorf("accroche acceptée : %q", bad)
		}
	}
	one := e.liDraft(t, "Une seule ligne sans point")
	if _, err := e.st.ApplyHook(one.ID, "x"); err == nil {
		t.Error("post d'une ligne : l'accroche ne peut pas remplacer tout le texte")
	}
	x := e.draft(t, "post X\n\nsuite")
	if _, err := e.st.SuggestHooks(context.Background(), x.ID); err == nil {
		t.Error("les aides ne valent que pour LinkedIn")
	}
}

// Plan 7 jours : borné par le plafond réglé par Allan ; X refusé.
func TestPlanWeekRespectsCapAndPlatform(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	_ = e.st.store.Update(func(d *Data) error { d.Account("pro").WeeklyCap = 2; return nil })
	ideas, err := e.st.PlanWeek(context.Background(), "pro")
	if err != nil || len(ideas) != 2 {
		t.Fatalf("plafond 2 : %d idées, %v", len(ideas), err)
	}
	if _, err := e.st.PlanWeek(context.Background(), "main"); err == nil {
		t.Error("le plan 7 jours ne vaut que pour LinkedIn")
	}
}

// Les aides ne sont jamais lancées par Tick.
func TestTickNeverRunsAids(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	e.liDraft(t, "Un texte net.")
	n := e.ai.count()
	e.st.Tick(context.Background())
	if e.ai.count() != n {
		t.Fatalf("Tick a appelé l'IA (%d → %d)", n, e.ai.count())
	}
}

// C7 : plafond hebdomadaire.
func TestC7WeeklyCap(t *testing.T) {
	e, _ := liEnv(t, "dryrun", true)
	_ = e.st.store.Update(func(d *Data) error { d.Account("pro").WeeklyCap = 2; return nil })
	// mercredi 18/11 08:00 : créneau 09:00 chaque jour, semaine du lundi 16 au dimanche 22
	slot := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, time.UTC) }
	a, b, c := e.liDraft(t, "un"), e.liDraft(t, "deux"), e.liDraft(t, "trois")
	va, _ := e.st.Validate(a.ID, "Allan", nil, false)
	vb, _ := e.st.Validate(b.ID, "Allan", nil, false)
	vc, err := e.st.Validate(c.ID, "Allan", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !va.ScheduledAt.Equal(slot(18, 9)) || !vb.ScheduledAt.Equal(slot(19, 9)) {
		t.Fatalf("deux premiers : %v %v", va.ScheduledAt, vb.ScheduledAt)
	}
	if !vc.ScheduledAt.Equal(slot(23, 9)) {
		t.Fatalf("le 3e post de la semaine doit passer au lundi suivant (23/11) : %v", vc.ScheduledAt)
	}
	// date imposée dans la semaine pleine : refus
	d := e.liDraft(t, "quatre")
	at := slot(21, 10)
	if _, err := e.st.Validate(d.ID, "Allan", &at, false); err == nil || !strings.Contains(err.Error(), "plafond de 2 post(s) atteint la semaine du 16/11/2026") {
		t.Fatalf("date imposée en semaine pleine : %v", err)
	}
	if _, err := e.st.Validate(d.ID, "Allan", nil, true); err == nil {
		t.Fatal("« publier maintenant » dépasse le plafond")
	}
	// date imposée dans une semaine libre : acceptée
	free := slot(24, 10)
	if _, err := e.st.Validate(d.ID, "Allan", &free, false); err != nil {
		t.Fatalf("semaine libre : %v", err)
	}
	// un post replacé sort de sa propre comptabilité (MoveToDay dans la semaine)
	if _, err := e.st.MoveToDay(vb.ID, "2026-11-20"); err != nil {
		t.Fatalf("déplacer dans sa semaine : %v", err)
	}
}

func TestC7WeeklyCapZeroLimitsNothing(t *testing.T) {
	e, _ := liEnv(t, "dryrun", true)
	var last time.Time
	for i, txt := range []string{"un", "deux", "trois", "quatre"} {
		p := e.liDraft(t, txt)
		v, err := e.st.Validate(p.ID, "Allan", nil, false)
		if err != nil {
			t.Fatalf("post %d : %v", i, err)
		}
		last = *v.ScheduledAt
	}
	if !last.Equal(time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)) { // un par jour : mer, jeu, ven, sam
		t.Fatalf("sans plafond, le 4e post prend le 4e créneau : %v", last)
	}
}

// C8 : un compte LinkedIn neuf n'impose ni plafond hebdomadaire ni limite journalière.
func TestC8NewLinkedInAccountNoCap(t *testing.T) {
	e := newEnv(t)
	if err := e.st.SaveAccount(&Account{Name: "neuf", Platform: "linkedin", Line: "kinflux", Backend: "manual"}); err != nil {
		t.Fatal(err)
	}
	a := e.st.store.Snapshot().Account("neuf")
	if a.WeeklyCap != 0 || a.DailyLimit != 0 {
		t.Fatalf("plafond %d, limite journalière %d", a.WeeklyCap, a.DailyLimit)
	}
	if err := e.st.SaveAccount(&Account{Name: "neg", Platform: "linkedin", Line: "kinflux", Backend: "manual", WeeklyCap: -1}); err == nil {
		t.Error("plafond négatif accepté")
	}
}

// Le plafond suit le fuseau du compte, autour du changement d'heure.
func TestWeekStartAroundDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("fuseau Europe/Paris indisponible")
	}
	// dimanche 28/03/2027 : passage à l'heure d'été à 02:00
	sun := time.Date(2027, 3, 28, 23, 30, 0, 0, loc)
	mon := time.Date(2027, 3, 29, 0, 30, 0, 0, loc)
	if ws := weekStart(sun, loc); !ws.Equal(time.Date(2027, 3, 22, 0, 0, 0, 0, loc)) {
		t.Errorf("dimanche : %v", ws)
	}
	if ws := weekStart(mon, loc); !ws.Equal(time.Date(2027, 3, 29, 0, 0, 0, 0, loc)) {
		t.Errorf("lundi : %v", ws)
	}
	// la semaine du changement d'heure ne fait que 167 h : lundi 22 00:00 → lundi 29 00:00 locales
	if got := weekStart(sun, loc).AddDate(0, 0, 7); !got.Equal(weekStart(mon, loc)) {
		t.Errorf("fin de semaine %v ≠ début suivant %v", got, weekStart(mon, loc))
	}
	// plafond de 1 : un post dimanche 23:30 locales ne remplit pas la semaine suivante
	a := &Account{Name: "pro", Platform: "linkedin", WeeklyCap: 1, Timezone: "Europe/Paris"}
	times := []time.Time{sun.UTC()}
	if !weekFull(a, times, sun.Add(-24*time.Hour)) {
		t.Error("la semaine du dimanche est pleine")
	}
	if weekFull(a, times, mon) {
		t.Error("le lundi suivant appartient à une autre semaine")
	}
}

// Le plafond se vérifie aussi à l'heure de publier : jamais dépassé, report.
func TestWeeklyCapPublishDueDefers(t *testing.T) {
	e, calls := liEnv(t, "dryrun", true)
	_ = e.st.store.Update(func(d *Data) error { d.Account("pro").WeeklyCap = 2; return nil })
	a, b := e.liDraft(t, "un"), e.liDraft(t, "deux")
	at := e.now
	for _, p := range []*Post{a, b} {
		if _, err := e.st.Validate(p.ID, "Allan", &at, false); err != nil {
			t.Fatal(err)
		}
	}
	// Allan abaisse ensuite le plafond à 1 : le deuxième post échu doit attendre
	_ = e.st.store.Update(func(d *Data) error { d.Account("pro").WeeklyCap = 1; return nil })
	res := e.st.PublishDue(context.Background())
	var published, skipped int
	for _, r := range res {
		switch r.Status {
		case StatusPublished:
			published++
		case "skipped":
			skipped++
		}
	}
	if published != 1 || skipped != 1 {
		t.Fatalf("résultats : %+v", res)
	}
	d := e.st.store.Snapshot()
	var deferred *Post
	for _, p := range d.Posts {
		if p.Account == "pro" && p.Status == StatusScheduled {
			deferred = p
		}
	}
	if deferred == nil || !deferred.ScheduledAt.After(e.now.AddDate(0, 0, 4)) || !strings.Contains(deferred.Error, "plafond") {
		t.Fatalf("le post doit être reporté à la semaine suivante : %+v", deferred)
	}
	if *calls != 0 {
		t.Fatal("backend factice appelé")
	}
}

// Les réponses de campagne et les posts d'un autre compte ne comptent pas.
func TestWeeklyCapCountsOnlyOwnPosts(t *testing.T) {
	a := &Account{Name: "pro", Platform: "linkedin", WeeklyCap: 1, Timezone: "UTC"}
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	d := &Data{Posts: []*Post{
		{ID: "1", Account: "autre", Kind: "post", Status: StatusScheduled, ScheduledAt: &now},
		{ID: "2", Account: "pro", Kind: "post", Status: StatusScheduled, ScheduledAt: &now, CampaignID: "c"},
		{ID: "3", Account: "pro", Kind: "reply", Status: StatusScheduled, ScheduledAt: &now},
		{ID: "4", Account: "pro", Kind: "post", Status: StatusRejected, ScheduledAt: &now},
	}}
	if weekFull(a, weekPostTimes(d, a, ""), now) {
		t.Fatal("seuls les posts du compte, programmés ou publiés, comptent")
	}
	d.Posts = append(d.Posts, &Post{ID: "5", Account: "pro", Kind: "post", Status: StatusManual, ScheduledAt: &now})
	if !weekFull(a, weekPostTimes(d, a, ""), now) {
		t.Fatal("un post « à publier à la main » compte")
	}
	if weekFull(a, weekPostTimes(d, a, "5"), now) {
		t.Fatal("le post que l'on place ne se compte pas lui-même")
	}
}

// ---------------------------------------------------------------- visuel

func tinyFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// pngHeaderOnly : en-tête PNG valide annonçant w×h pixels, sans image (suffit à DecodeConfig).
func pngHeaderOnly(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(typ string, data []byte) {
		_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
		b.WriteString(typ)
		b.Write(data)
		_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), data...)))
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8] = 8 // profondeur 8 bits, niveaux de gris
	chunk("IHDR", ihdr)
	chunk("IDAT", nil)
	return b.Bytes()
}

// C9 : import d'image.
func TestC9ImageImport(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	p := e.liDraft(t, "Un post avec visuel")

	var png1 bytes.Buffer
	_ = png.Encode(&png1, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	good := tinyFile(t, "ok.png", png1.Bytes())
	bmp := tinyFile(t, "image.bmp", append([]byte("BM"), make([]byte, 60)...))
	big := tinyFile(t, "gros.png", append(png1.Bytes(), make([]byte, 10<<20+1)...))
	huge := tinyFile(t, "immense.png", pngHeaderOnly(7000, 6000)) // 42 000 000 px ≥ 36 152 320
	notImg := tinyFile(t, "faux.png", []byte("ce n'est pas une image"))

	for name, path := range map[string]string{"bmp": bmp, "trop lourd": big, "trop de pixels": huge, "pas une image": notImg, "absent": filepath.Join(t.TempDir(), "nope.png")} {
		if _, err := e.st.SetMedia(p.ID, path); err == nil {
			t.Errorf("%s : accepté", name)
		}
		if got := e.st.store.Snapshot().Post(p.ID); got.MediaPath != "" {
			t.Errorf("%s : le chemin a été enregistré", name)
		}
	}
	// un brouillon créé avec un visuel refusé n'est pas créé
	if _, err := e.st.AddDraft("pro", "avec bmp", "", "", bmp); err == nil {
		t.Error("AddDraft avec un .bmp accepté")
	}
	// à la limite haute : juste sous 36 152 320 px
	if _, err := e.st.SetMedia(p.ID, tinyFile(t, "limite.png", pngHeaderOnly(6000, 6025))); err != nil { // 36 150 000
		t.Errorf("juste sous la limite de pixels : %v", err)
	}
	got, err := e.st.SetMedia(p.ID, good)
	if err != nil || got.MediaPath != good {
		t.Fatalf("PNG valide : %+v %v", got, err)
	}
	// un compte X garde son comportement : pas de contrôle d'image à l'enregistrement
	x := e.draft(t, "post X")
	if _, err := e.st.SetMedia(x.ID, bmp); err != nil {
		t.Errorf("X : %v", err)
	}
}

// Texte alternatif : plafond de 4 086 caractères, image requise.
func TestMediaAlt(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	p := e.liDraft(t, "Un post")
	if _, err := e.st.SetMediaAlt(p.ID, "un texte"); err == nil {
		t.Error("texte alternatif sans image accepté")
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if _, err := e.st.SetMedia(p.ID, tinyFile(t, "a.png", buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	got, err := e.st.SetMediaAlt(p.ID, "  Un schéma\n simple ")
	if err != nil || got.MediaAlt != "Un schéma simple" {
		t.Fatalf("%q %v", got.MediaAlt, err)
	}
	if _, err := e.st.SetMediaAlt(p.ID, strings.Repeat("é", maxMediaAlt+1)); err == nil {
		t.Error("texte alternatif trop long accepté")
	}
	if got, err := e.st.SetMediaAlt(p.ID, strings.Repeat("é", maxMediaAlt)); err != nil || got.MediaAlt == "" {
		t.Errorf("4 086 caractères : %v", err)
	}
}

// C9 : avertissement non bloquant si un brief de visuel n'a pas d'image.
func TestC9VisualWarning(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	p := e.liDraft(t, "Un post")
	_ = e.st.store.Update(func(d *Data) error { d.Post(p.ID).VisualBrief = "Une photo du devis"; return nil })
	v, err := e.st.Validate(p.ID, "Allan", nil, true)
	if err != nil {
		t.Fatalf("l'avertissement ne doit pas bloquer : %v", err)
	}
	if len(v.Warnings) != 1 || !strings.Contains(v.Warnings[0], "visuel") {
		t.Fatalf("avertissement attendu : %v", v.Warnings)
	}
	// avec une image : pas d'avertissement
	q := e.liDraft(t, "Un autre post")
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	_, _ = e.st.SetMedia(q.ID, tinyFile(t, "b.png", buf.Bytes()))
	_ = e.st.store.Update(func(d *Data) error { d.Post(q.ID).VisualBrief = "Une photo"; return nil })
	if v, err := e.st.Validate(q.ID, "Allan", nil, true); err != nil || len(v.Warnings) != 0 {
		t.Fatalf("%v %v", v.Warnings, err)
	}
}

// LinkedIn : une idée de visuel est toujours demandée au brouillon.
func TestLinkedInDraftAlwaysAsksVisual(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	ps, err := e.st.GenerateDrafts(context.Background(), "pro", "un sujet", "", 1, "")
	if err != nil || len(ps) == 0 {
		t.Fatal(err)
	}
	item := e.ai.lastSchema["properties"].(map[string]any)["drafts"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if item["visual"] == nil || ps[0].VisualBrief == "" {
		t.Fatalf("le visuel doit être demandé sur LinkedIn : %v / %q", item, ps[0].VisualBrief)
	}
	// un compte X sans guide de visuel ne le demande pas
	if _, err := e.st.GenerateDrafts(context.Background(), "main", "un sujet", "", 1, ""); err != nil {
		t.Fatal(err)
	}
	item = e.ai.lastSchema["properties"].(map[string]any)["drafts"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if item["visual"] != nil {
		t.Error("X : visuel demandé sans guide")
	}
}

// C9 : publication avec image et texte alternatif, vérifiée sur le faux serveur.
func TestC9PublishWithImageAndAlt(t *testing.T) {
	e, f := liAPI(t, true)
	p := e.liDraft(t, "Avec un visuel et son alt")
	if _, err := e.st.SetMedia(p.ID, writePNG(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.SetMediaAlt(p.ID, "Un schéma simple"); err != nil {
		t.Fatal(err)
	}
	at := e.now.Add(-time.Minute)
	if _, err := e.st.Validate(p.ID, "allan", &at, false); err != nil {
		t.Fatal(err)
	}
	if res := e.st.PublishDue(context.Background()); len(res) != 1 || res[0].Status != StatusPublished {
		t.Fatalf("résultat : %+v", res)
	}
	last := f.reqs[len(f.reqs)-1]
	var body struct {
		Content struct {
			Media struct {
				ID  string `json:"id"`
				Alt string `json:"altText"`
			} `json:"media"`
		} `json:"content"`
	}
	_ = json.Unmarshal(last.Body, &body)
	if body.Content.Media.ID != "urn:li:image:IMG1" || body.Content.Media.Alt != "Un schéma simple" {
		t.Fatalf("content.media = %+v (%s)", body.Content.Media, last.Body)
	}
}

// C1 : la notice de linkedin-skills.
func TestC1ThirdPartyNotice(t *testing.T) {
	b, err := os.ReadFile("THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"linkedin-skills", "https://github.com/sergebulaev/linkedin-skills", "32083c0", "License: MIT", "MIT License", "Copyright (c) 2026 Sergey Bulaev", "Permission is hereby granted"} {
		if !strings.Contains(s, want) {
			t.Errorf("THIRD_PARTY_NOTICES.md : %q absent", want)
		}
	}
}

// Les routes des aides : rien ne s'applique sans le clic « Appliquer ».
func TestAidRoutes(t *testing.T) {
	e, _ := liEnv(t, "manual", true)
	srv := httptest.NewServer(buildHandler(e.st, ServeOptions{NoAuth: true}))
	t.Cleanup(srv.Close)
	c := srv.Client()
	p := e.liDraft(t, "Première ligne.\n\nSuite.")
	base := srv.URL + "/api/posts/" + p.ID
	for _, path := range []string{"/hooks", "/humanize", "/audit"} {
		if code, m := call(t, c, "POST", base+path, `{}`); code != 200 {
			t.Fatalf("%s : %d %v", path, code, m)
		}
	}
	if got := e.st.store.Snapshot().Post(p.ID); got.Text != "Première ligne.\n\nSuite." || len(got.History) != 0 {
		t.Fatalf("une route d'aide a modifié le post : %+v", got)
	}
	if code, m := call(t, c, "POST", base+"/hooks/apply", `{"hook":"Accroche choisie"}`); code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	if got := e.st.store.Snapshot().Post(p.ID); got.Text != "Accroche choisie\n\nSuite." {
		t.Fatalf("%q", got.Text)
	}
	if code, m := call(t, c, "POST", base+"/humanize/apply", `{"text":"Texte humanisé"}`); code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	// contrôle puis validation avec accusé, par les routes
	if code, m := call(t, c, "POST", base+"/tics", `{}`); code != 200 || m["data"].(map[string]any)["needsAck"] != false {
		t.Fatalf("%d %v", code, m)
	}
	if code, m := call(t, c, "POST", base+"/line", `{"line":"kinflux"}`); code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	// le texte n'a pas changé depuis le contrôle : la validation passe
	if code, m := call(t, c, "POST", base+"/validate", `{"by":"Allan","now":true}`); code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	// un tic haut exige ackTics par la route aussi
	q := e.liDraft(t, "Ce n'est pas du marketing, c'est de la confiance.")
	qb := srv.URL + "/api/posts/" + q.ID
	if code, m := call(t, c, "POST", qb+"/validate", `{"by":"Allan","now":true}`); code == 200 {
		t.Fatalf("tic haut accepté sans accusé : %v", m)
	}
	if code, m := call(t, c, "POST", qb+"/validate", `{"by":"Allan","ackTics":true,"now":true}`); code != 200 {
		t.Fatalf("%d %v", code, m)
	}
}

// NextSlot multi-semaines : plafond de 1 en Europe/Paris autour du changement d'heure.
func TestWeeklyCapNextSlotParisDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("fuseau Europe/Paris indisponible")
	}
	for _, mode := range []string{"slots", "window"} {
		a := &Account{Name: "pro", Platform: "linkedin", Line: "perso", Timezone: "Europe/Paris", Slots: []string{"09:00"}, WeeklyCap: 1}
		if mode == "window" {
			j := 0
			a.Slots, a.Timing = nil, Timing{Mode: "window", Start: "09:00", End: "22:00", GapMin: 30, GapMax: 30, Jitter: &j}
		}
		sunday := time.Date(2027, 3, 28, 9, 0, 0, 0, loc) // dernier jour de la semaine du changement d'heure
		d := &Data{Settings: Settings{MinGapMinutes: 60}, Accounts: []*Account{a}, Posts: []*Post{
			{ID: "1", Account: "pro", Kind: "post", Status: StatusScheduled, ScheduledAt: &sunday},
		}}
		after := time.Date(2027, 3, 24, 10, 0, 0, 0, loc) // mercredi : la semaine est pleine
		got, err := NextSlot(d, a, after.UTC(), "")
		if err != nil {
			t.Fatalf("%s : %v", mode, err)
		}
		want := time.Date(2027, 3, 29, 9, 0, 0, 0, loc) // lundi suivant, 09:00 heure d'été
		if got.In(loc).Before(want) || got.In(loc).After(want.Add(time.Hour)) {
			t.Fatalf("%s : %v, attendu autour de %v", mode, got.In(loc), want)
		}
		if mode == "slots" && !got.Equal(want) {
			t.Fatalf("slots : %v ≠ %v", got, want)
		}
		// sans plafond, le créneau reste dans la semaine en cours
		a.WeeklyCap = 0
		got, _ = NextSlot(d, a, after.UTC(), "")
		if !got.In(loc).Before(want) {
			t.Fatalf("%s sans plafond : %v", mode, got.In(loc))
		}
	}
}
