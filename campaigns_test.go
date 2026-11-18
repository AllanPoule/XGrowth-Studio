package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testCampaign() *Campaign {
	return &Campaign{Account: "main", Name: "Audit", Active: true, Hours: "08-22", MaxFollowers: 5000, RepliesPerDay: 2, SearchesPerDay: 3,
		Pains: []Pain{{Text: "je galère à savoir ce que mon agent a fait", Searches: []string{"agent IA logs", "agent IA boîte noire"},
			Matches: []string{"Impossible de savoir ce que l'agent a modifié"}, NonMatches: []string{"Les agents IA vont tout changer"},
			Solution: "journaliser chaque action de l'agent", VerifiedOn: "docs du SDK", Active: true}}}
}

func feedTweet(id, author, text string, followers int) Tweet {
	t := tw(id, author, text)
	t.Author.Followers = followers
	return t
}

func TestCampaignValidation(t *testing.T) {
	e := newEnv(t)
	c, err := e.st.SaveCampaign(&Campaign{Account: "main", Name: "Vide"})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.RepliesPerDay != 10 || c.GapMinutes != 15 || c.AuthorCooldown != 7 || c.SearchesPerDay != 12 {
		t.Fatalf("valeurs par défaut : %+v", c)
	}
	bad := []*Campaign{
		{Account: "main"},
		{Account: "inconnu", Name: "x"},
		{Account: "main", Name: "x", Hours: "22-08"},
		{Account: "main", Name: "x", Pains: []Pain{{Text: "p", Active: true, Searches: []string{"q"}, VerifiedOn: "v"}}},
		{Account: "main", Name: "x", Pains: []Pain{{Text: "p", Active: true, Searches: []string{"q"}, Solution: "s"}}},
		{Account: "main", Name: "x", Pains: []Pain{{Text: "p", Active: true, Solution: "s", VerifiedOn: "v"}}},
	}
	for _, b := range bad {
		if _, err := e.st.SaveCampaign(b); err == nil {
			t.Errorf("campagne invalide acceptée : %+v", b)
		}
	}
	// une douleur en pause peut rester incomplète
	if _, err := e.st.SaveCampaign(&Campaign{Account: "main", Name: "Brouillon", Pains: []Pain{{Text: "à préciser"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCampaignRunFindsQualifiesAndDrafts(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error { d.Account("main").Handle = "moi"; return nil })
	e.x.feed = []Tweet{
		feedTweet("1", "alice", "Je galère : mon agent a modifié des fichiers et je ne sais pas lesquels", 120),
		feedTweet("2", "bob", "Les agents IA, c'est magique, blague du jour", 300),
		feedTweet("3", "moi", "Je galère aussi, dit le compte lui-même", 10),
		feedTweet("4", "star", "Je galère avec mon agent", 900000),
	}
	rt := feedTweet("5", "carol", "RT je galère", 10)
	rt.IsRetweet = true
	e.x.feed = append(e.x.feed, rt)
	c, err := e.st.SaveCampaign(testCampaign())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := e.st.RunCampaign(ctx, c.ID, true)
	if r.Error != "" || r.Searches != 2 || r.Found != 2 || r.Qualified != 1 || r.Dismissed != 1 {
		t.Fatalf("passage : %+v", r)
	}
	mustContain(t, "demande", e.ai.lastMsg, "je galère à savoir ce que mon agent a fait", "journaliser chaque action de l'agent",
		"vérifiée sur : docs du SDK", "Correspond :", "Ne correspond pas :", "La réponse aide d'abord", `auteur="alice"`)
	d := e.st.store.Snapshot()
	var reply *Post
	for _, p := range d.Posts {
		if p.CampaignID == c.ID {
			reply = p
		}
	}
	if reply == nil || reply.Kind != "reply" || reply.ReplyTo != "1" || reply.Status != StatusDraft || !strings.Contains(reply.TargetText, "Je galère") {
		t.Fatalf("réponse rédigée : %+v", reply)
	}
	for _, x := range d.Candidates {
		switch x.Author {
		case "alice":
			if x.Status != CandDrafted || x.PostID != reply.ID || x.PainID != c.Pains[0].ID {
				t.Fatalf("candidat retenu : %+v", x)
			}
		case "bob":
			if x.Status != CandDismissed {
				t.Fatalf("candidat écarté : %+v", x)
			}
		default:
			t.Fatalf("candidat inattendu (compte lui-même, trop d'abonnés ou repost) : %+v", x)
		}
	}

	// pas de doublon, période de repos de l'auteur, budget de recherche du jour
	e.x.feed = []Tweet{feedTweet("1", "alice", "Je galère", 1), feedTweet("6", "alice", "Je galère encore", 1), feedTweet("7", "dan", "Je galère moi aussi", 1)}
	r = e.st.RunCampaign(ctx, c.ID, true)
	if r.Searches != 1 || r.Found != 1 || r.Qualified != 1 {
		t.Fatalf("second passage (budget de 3 recherches, alice au repos) : %+v", r)
	}
	if r = e.st.RunCampaign(ctx, c.ID, true); r.Searches != 0 {
		t.Fatalf("budget épuisé : %+v", r)
	}
	e.now = e.now.Add(24 * time.Hour)
	if r = e.st.RunCampaign(ctx, c.ID, true); r.Searches != 2 {
		t.Fatalf("le budget repart chaque jour : %+v", r)
	}
}

func TestCampaignSchedulerPace(t *testing.T) {
	e := newEnv(t)
	c, _ := e.st.SaveCampaign(testCampaign())
	ctx := context.Background()
	e.now = time.Date(2026, 11, 18, 6, 0, 0, 0, time.UTC)
	if r := e.st.RunCampaign(ctx, c.ID, false); r.Skipped == "" {
		t.Fatal("hors de la fenêtre 08-22 : pas de passage")
	}
	e.now = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	if out := e.st.Tick(ctx); out["campaigns"] == nil {
		t.Fatalf("le planificateur doit faire tourner la campagne : %v", out)
	}
	e.now = e.now.Add(30 * time.Minute)
	if r := e.st.RunCampaign(ctx, c.ID, false); r.Skipped != "passage récent" {
		t.Fatalf("au plus un passage par heure : %+v", r)
	}
	c.Active = false
	_, _ = e.st.SaveCampaign(c)
	e.now = e.now.Add(2 * time.Hour)
	if r := e.st.RunCampaign(ctx, c.ID, false); r.Skipped != "campagne en pause" {
		t.Fatalf("campagne en pause : %+v", r)
	}
}

func TestReplySchedulingFollowsCampaignPace(t *testing.T) {
	e := newEnv(t)
	c, _ := e.st.SaveCampaign(testCampaign()) // 2 réponses par jour, 15 min d'écart, 08-22
	e.now = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	reply := func() *Post {
		p := e.draft(t, "Réponse "+newID(""))
		_ = e.st.store.Update(func(d *Data) error {
			x := d.Post(p.ID)
			x.Kind, x.ReplyTo, x.CampaignID = "reply", "123", c.ID
			return nil
		})
		v, err := e.st.Validate(p.ID, "Allan", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	day := func(h, m int) time.Time { return time.Date(2026, 11, 18, h, m, 0, 0, time.UTC) }
	if got := *reply().ScheduledAt; !got.Equal(day(10, 1)) {
		t.Fatalf("première réponse : %v", got)
	}
	if got := *reply().ScheduledAt; !got.Equal(day(10, 16)) {
		t.Fatalf("seconde réponse, 15 min plus tard : %v", got)
	}
	if got := *reply().ScheduledAt; !got.Equal(day(8, 0).AddDate(0, 0, 1)) {
		t.Fatalf("au-delà de 2 par jour : le lendemain à l'ouverture, obtenu %v", got)
	}
	// les réponses n'occupent pas les créneaux des posts du compte
	p := e.draft(t, "un post ordinaire")
	v, err := e.st.Validate(p.ID, "Allan", nil, false)
	if err != nil || !v.ScheduledAt.Equal(day(13, 0)) {
		t.Fatalf("créneau du post : %v %v", v.ScheduledAt, err)
	}
}

func TestAPICampaigns(t *testing.T) {
	srv, e := tuningServer(t)
	c := &http.Client{}
	body, _ := json.Marshal(testCampaign())
	st, m := call(t, c, "POST", srv.URL+"/api/campaigns", string(body))
	if st != 200 {
		t.Fatalf("création : %v", m)
	}
	id := m["data"].(map[string]any)["id"].(string)
	if st, _ := call(t, c, "POST", srv.URL+"/api/campaigns", `{"account":"main","name":"x","pains":[{"text":"p","active":true}]}`); st != 400 {
		t.Fatal("douleur active sans solution acceptée")
	}
	e.x.feed = []Tweet{feedTweet("9", "erin", "Je galère avec les logs de mon agent", 50)}
	if st, m := call(t, c, "POST", srv.URL+"/api/campaigns/"+id+"/run", `{}`); st != 200 || m["data"].(map[string]any)["qualified"].(float64) != 1 {
		t.Fatalf("passage : %v", m)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/settings", `{"commentsPrompt":"Réponds en une phrase."}`); st != 200 {
		t.Fatal("consigne des commentaires refusée")
	}
	_, _ = call(t, c, "POST", srv.URL+"/api/campaigns/"+id+"/run", `{}`)
	_, m = call(t, c, "GET", srv.URL+"/api/state", "")
	data := m["data"].(map[string]any)
	stats := data["campaignStats"].(map[string]any)[id].(map[string]any)
	if len(data["campaigns"].([]any)) != 1 || len(data["candidates"].([]any)) != 1 || stats["toValidate"].(float64) != 1 || data["commentsDefault"] != DefaultCommentsPrompt {
		t.Fatalf("état : %v", stats)
	}
	if st, _ := call(t, c, "POST", srv.URL+"/api/campaigns/"+id+"/remove", `{}`); st != 200 {
		t.Fatal("suppression refusée")
	}
}

func TestLegacyDataHasNoCampaignFields(t *testing.T) {
	e := newEnv(t)
	b, _ := json.Marshal(e.st.store.Snapshot())
	for _, absent := range []string{`"campaigns"`, `"candidates"`, `"campaignId"`, `"commentsPrompt"`} {
		if strings.Contains(string(b), absent) {
			t.Errorf("champ vide %s écrit sans campagne", absent)
		}
	}
}

func TestCommentsPromptSetting(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error { d.Settings.CommentsPrompt = "Une phrase, un conseil."; return nil })
	c, _ := e.st.SaveCampaign(testCampaign())
	e.x.feed = []Tweet{feedTweet("1", "alice", "Je galère", 1)}
	e.st.RunCampaign(context.Background(), c.ID, true)
	mustContain(t, "consigne", e.ai.lastMsg, "Une phrase, un conseil.")
	if strings.Contains(e.ai.lastMsg, "La réponse aide d'abord") {
		t.Fatal("la consigne réglée remplace la consigne par défaut")
	}
}
