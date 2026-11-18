package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// créneaux 09:00, 13:00 et 18:00, 2 posts par jour au plus, maintenant le 18/11 à 08:00 (UTC)
func at(day, h int) time.Time { return time.Date(2026, 11, day, h, 0, 0, 0, time.UTC) }

func (e *env) planned(t *testing.T, text string) *Post {
	t.Helper()
	p := e.draft(t, text)
	v, err := e.st.Validate(p.ID, "Allan", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (e *env) when(id string) time.Time { return *e.st.store.Snapshot().Post(id).ScheduledAt }

func TestReorderKeepsTheConfiguredTimes(t *testing.T) {
	e := newEnv(t)
	p1, p2, p3 := e.planned(t, "un"), e.planned(t, "deux"), e.planned(t, "trois")
	if !e.when(p1.ID).Equal(at(18, 9)) || !e.when(p2.ID).Equal(at(18, 13)) || !e.when(p3.ID).Equal(at(19, 9)) {
		t.Fatalf("créneaux de départ : %v %v %v", e.when(p1.ID), e.when(p2.ID), e.when(p3.ID))
	}
	if _, err := e.st.ReorderQueue([]string{p3.ID, p1.ID, p2.ID}); err != nil {
		t.Fatal(err)
	}
	if !e.when(p3.ID).Equal(at(18, 9)) || !e.when(p1.ID).Equal(at(18, 13)) || !e.when(p2.ID).Equal(at(19, 9)) {
		t.Fatalf("le nouvel ordre reprend les mêmes heures : %v %v %v", e.when(p3.ID), e.when(p1.ID), e.when(p2.ID))
	}
	d := e.draft(t, "brouillon")
	for _, bad := range [][]string{{p1.ID}, {p1.ID, p1.ID}, {p1.ID, d.ID}, {p1.ID, "inconnu"}} {
		if _, err := e.st.ReorderQueue(bad); err == nil {
			t.Errorf("ordre invalide accepté : %v", bad)
		}
	}
}

func TestDroppedDraftIsValidatedAndInserted(t *testing.T) {
	e := newEnv(t)
	p1, p2 := e.planned(t, "un"), e.planned(t, "deux") // 18/11 à 09:00 et 13:00
	d := e.draft(t, "glissé avant le premier")
	if _, err := e.st.ScheduleDraft(d.ID, "", p1.ID); err == nil {
		t.Fatal("une validation doit rester signée")
	}
	p, err := e.st.ScheduleDraft(d.ID, "Allan", p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusScheduled || p.ValidatedBy != "Allan" {
		t.Fatalf("brouillon validé : %+v", p)
	}
	// le nouveau créneau (19/11 à 09:00) rejoint les heures du compte, redistribuées dans le nouvel ordre
	if !e.when(d.ID).Equal(at(18, 9)) || !e.when(p1.ID).Equal(at(18, 13)) || !e.when(p2.ID).Equal(at(19, 9)) {
		t.Fatalf("insertion : %v %v %v", e.when(d.ID), e.when(p1.ID), e.when(p2.ID))
	}
	// sans cible : le prochain créneau libre, comme une validation ordinaire
	d2 := e.draft(t, "en fin de file")
	if p, _ := e.st.ScheduleDraft(d2.ID, "Allan", ""); !p.ScheduledAt.Equal(at(19, 13)) {
		t.Fatalf("en fin de file : %v", p.ScheduledAt)
	}
	// un brouillon bloqué par un garde-fou n'est ni validé ni déplacé
	_ = e.st.store.Update(func(d *Data) error { d.Settings.BannedPhrases = []string{"interdit"}; return nil })
	d3 := e.draft(t, "un mot interdit")
	if _, err := e.st.ScheduleDraft(d3.ID, "Allan", p1.ID); err == nil {
		t.Fatal("les garde-fous s'appliquent au glisser-déposer")
	}
}

func TestMoveToDayUsesTheAccountSchedule(t *testing.T) {
	e := newEnv(t)
	p1, p2, p3 := e.planned(t, "un"), e.planned(t, "deux"), e.planned(t, "trois")
	p, err := e.st.MoveToDay(p3.ID, "2026-11-21")
	if err != nil || !p.ScheduledAt.Equal(at(21, 9)) {
		t.Fatalf("déplacement : %v %v", p.ScheduledAt, err)
	}
	if _, err := e.st.MoveToDay(p3.ID, "2026-11-18"); err == nil {
		t.Fatal("le 18 est complet (2 posts par jour) : refus attendu")
	}
	if _, err := e.st.MoveToDay(p1.ID, "2026-11-17"); err == nil {
		t.Fatal("un jour passé doit être refusé")
	}
	if _, err := e.st.MoveToDay(p1.ID, "demain"); err == nil {
		t.Fatal("jour invalide accepté")
	}
	// aujourd'hui : seulement les créneaux encore à venir
	e.now = at(18, 14)
	if p, err := e.st.MoveToDay(p2.ID, "2026-11-18"); err != nil || !p.ScheduledAt.Equal(at(18, 18)) {
		t.Fatalf("aujourd'hui, après 14 h : %v %v", p.ScheduledAt, err)
	}
	d := e.draft(t, "brouillon")
	if _, err := e.st.MoveToDay(d.ID, "2026-11-22"); err == nil {
		t.Fatal("un brouillon ne se déplace pas sans validation")
	}
}

func TestDragAndDropWithWindowAndCampaigns(t *testing.T) {
	e := newEnv(t)
	old := randIntn
	randIntn = func(int) int { return 0 } // aléa du premier post : -15 min, reporté après le début de la plage
	t.Cleanup(func() { randIntn = old })
	acc := e.st.store.Snapshot().Account("main")
	acc.Timing = Timing{Mode: "window"}
	if err := e.st.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	p := e.planned(t, "dans la plage")
	if got, err := e.st.MoveToDay(p.ID, "2026-11-21"); err != nil || !got.ScheduledAt.Equal(time.Date(2026, 11, 21, 8, 15, 0, 0, time.UTC)) {
		t.Fatalf("plage horaire : %v %v", got.ScheduledAt, err)
	}
	// une réponse de campagne suit le rythme de sa campagne
	c, _ := e.st.SaveCampaign(testCampaign())
	r := e.draft(t, "réponse")
	_ = e.st.store.Update(func(d *Data) error {
		x := d.Post(r.ID)
		x.Kind, x.ReplyTo, x.CampaignID = "reply", "1", c.ID
		return nil
	})
	if _, err := e.st.Validate(r.ID, "Allan", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.MoveToDay(r.ID, "2026-11-22"); err == nil {
		t.Fatal("une réponse de campagne ne se déplace pas")
	}
	if _, err := e.st.ReorderQueue([]string{r.ID, p.ID}); err == nil {
		t.Fatal("une réponse de campagne ne se réordonne pas avec les posts")
	}
}

func TestAPIScheduleDragAndDrop(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(buildHandler(e.st, ServeOptions{NoAuth: true}))
	t.Cleanup(srv.Close)
	c := &http.Client{}
	p1, p2 := e.planned(t, "un"), e.planned(t, "deux")
	if st, m := call(t, c, "POST", srv.URL+"/api/schedule/reorder", `{"ids":["`+p2.ID+`","`+p1.ID+`"]}`); st != 200 {
		t.Fatalf("réordonnancement : %v", m)
	}
	if !e.when(p2.ID).Equal(at(18, 9)) {
		t.Fatal("ordre non appliqué")
	}
	d := e.draft(t, "glissé")
	if st, m := call(t, c, "POST", srv.URL+"/api/posts/"+d.ID+"/schedule", `{"by":"Allan","before":"`+p2.ID+`"}`); st != 200 {
		t.Fatalf("brouillon glissé : %v", m)
	}
	if st, m := call(t, c, "POST", srv.URL+"/api/posts/"+p1.ID+"/move", `{"day":"2026-11-22"}`); st != 200 {
		t.Fatalf("déplacement : %v", m)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/posts/"+p1.ID+"/move", `{"day":"2026-11-01"}`); st != 400 {
		t.Fatal("jour passé accepté")
	}
}
