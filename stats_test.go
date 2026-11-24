package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// countingX compte les relevés de tweet et peut les faire échouer.
type countingX struct {
	fakeX
	tweetCalls int
	tweetErr   error
	noViews    bool
	followers  int
}

func (c *countingX) Tweet(ctx context.Context, id string) (*Tweet, error) {
	c.tweetCalls++
	if c.tweetErr != nil {
		return nil, c.tweetErr
	}
	t, _ := c.fakeX.Tweet(ctx, id)
	if c.noViews {
		t.Metrics.Views = 0
	}
	return t, nil
}
func (c *countingX) Me(context.Context) (*Profile, error) {
	return &Profile{FollowersCount: c.followers}, nil
}

func statsEnv(t *testing.T) (*env, *countingX) {
	e := newEnv(t)
	x := &countingX{followers: 100}
	e.st.backendFor = func(*Account) (XBackend, error) { return x, nil }
	return e, x
}

func addPublished(t *testing.T, e *env, id string, ago time.Duration, m *Metrics, mut func(*Post)) {
	t.Helper()
	pub := e.now.Add(-ago)
	p := &Post{ID: id, Account: "main", Kind: "post", Text: "texte " + id, Status: StatusPublished, PublishedAt: &pub, TweetID: "77" + id, Metrics: m}
	if mut != nil {
		mut(p)
	}
	_ = e.st.store.Update(func(d *Data) error { d.Posts = append(d.Posts, p); return nil })
}

func TestCatchUpBeyond14Days(t *testing.T) {
	e, x := statsEnv(t)
	day := 24 * time.Hour
	// 20 jours, jamais relevé : rattrapage
	addPublished(t, e, "never", 20*day, nil, nil)
	// 20 jours, seulement une mesure précoce (1 h après) : rattrapage
	addPublished(t, e, "early", 20*day, &Metrics{Views: 10, UpdatedAt: e.now.Add(-20*day + time.Hour)}, nil)
	// 20 jours, mesure tardive : rien
	addPublished(t, e, "late", 20*day, &Metrics{Views: 10, UpdatedAt: e.now.Add(-18 * day)}, nil)
	n, err := e.st.RefreshMetrics(context.Background(), 0)
	if err != nil || n != 2 || x.tweetCalls != 2 {
		t.Fatalf("n=%d calls=%d err=%v", n, x.tweetCalls, err)
	}
	d := e.st.store.Snapshot()
	if m := d.Post("never").Metrics; m == nil || !hasLateMetrics(d.Post("never")) || m.Views != 1000 {
		t.Fatalf("pas de mesure tardive : %+v", m)
	}
	// une mesure tardive obtenue met fin au rattrapage
	if n, _ := e.st.RefreshMetrics(context.Background(), 0); n != 0 || x.tweetCalls != 2 {
		t.Fatalf("bouclage : n=%d calls=%d", n, x.tweetCalls)
	}
}

func TestCatchUpIsBoundedOnFailure(t *testing.T) {
	e, x := statsEnv(t)
	x.tweetErr = errors.New("indisponible")
	addPublished(t, e, "old", 30*24*time.Hour, nil, nil)
	for i := 0; i < 6; i++ {
		_, _ = e.st.RefreshMetrics(context.Background(), 0)
	}
	if x.tweetCalls != metricsCatchUpMax {
		t.Fatalf("%d tentatives, attendu %d", x.tweetCalls, metricsCatchUpMax)
	}
	if got := e.st.store.Snapshot().Post("old").MetricsCatchUps; got != metricsCatchUpMax {
		t.Fatalf("compteur %d", got)
	}
}

func TestWithin14DaysNotCountedAsCatchUp(t *testing.T) {
	e, x := statsEnv(t)
	addPublished(t, e, "recent", 3*24*time.Hour, nil, nil)
	for i := 0; i < 3; i++ {
		_, _ = e.st.RefreshMetrics(context.Background(), 0)
	}
	if x.tweetCalls != 3 || e.st.store.Snapshot().Post("recent").MetricsCatchUps != 0 {
		t.Fatalf("calls=%d", x.tweetCalls)
	}
}

func TestAbsentIsNotZero(t *testing.T) {
	e, x := statsEnv(t)
	x.noViews = true
	addPublished(t, e, "novw", 3*24*time.Hour, nil, nil)
	_, _ = e.st.RefreshMetrics(context.Background(), 0)
	// un vrai zéro : le backend a renvoyé views=0
	addPublished(t, e, "zero", 3*24*time.Hour, &Metrics{Views: 0, ViewsKnown: true, Likes: 0, UpdatedAt: e.now}, nil)
	addPublished(t, e, "never", 2*24*time.Hour, nil, nil)
	v := ComputeStats(e.st.store.Snapshot(), StatsFilter{}, e.now)
	by := map[string]StatPost{}
	for _, p := range v.Posts {
		by[p.ID] = p
	}
	if p := by["novw"]; !p.Measured || p.Views != nil || p.EngagementPct != nil || p.Likes == nil || *p.Likes != 50 {
		t.Fatalf("vues absentes : %+v", p)
	}
	if p := by["zero"]; p.Views == nil || *p.Views != 0 || p.Likes == nil || *p.Likes != 0 {
		t.Fatalf("vrai zéro : %+v", p)
	}
	if p := by["never"]; p.Measured || p.Views != nil || p.Likes != nil || p.Unavailable == "" || p.MeasuredAt != nil {
		t.Fatalf("jamais relevé : %+v", p)
	}
	if v.Totals.NeverMeasure != 1 || v.Totals.ViewsPosts != 1 || v.Totals.Views == nil || *v.Totals.Views != 0 {
		t.Fatalf("totaux : %+v", v.Totals)
	}
	b, _ := json.Marshal(by["never"])
	if !strings.Contains(string(b), `"views":null`) || !strings.Contains(string(b), `"measuredAt":null`) {
		t.Fatalf("JSON : %s", b)
	}
	// sans aucune vue connue, le total est indisponible, pas 0
	e2, _ := statsEnv(t)
	addPublished(t, e2, "a", time.Hour, &Metrics{Likes: 3, UpdatedAt: e2.now}, nil)
	if v2 := ComputeStats(e2.st.store.Snapshot(), StatsFilter{}, e2.now); v2.Totals.Views != nil || v2.Totals.Likes == nil {
		t.Fatalf("%+v", v2.Totals)
	}
}

func TestTweetViewsKnownFromJSON(t *testing.T) {
	var a, b Tweet
	_ = json.Unmarshal([]byte(`{"id":"1","metrics":{"likes":2,"views":0}}`), &a)
	_ = json.Unmarshal([]byte(`{"id":"1","metrics":{"likes":2}}`), &b)
	if !a.Metrics.ViewsKnown || b.Metrics.ViewsKnown {
		t.Fatalf("a=%v b=%v", a.Metrics.ViewsKnown, b.Metrics.ViewsKnown)
	}
	var x xapiTweet
	_ = json.Unmarshal([]byte(`{"id":"1","public_metrics":{"like_count":1}}`), &x)
	if x.toTweet([2]string{}).Metrics.ViewsKnown {
		t.Fatal("impression absente prise pour connue")
	}
	_ = json.Unmarshal([]byte(`{"id":"1","public_metrics":{"impression_count":0}}`), &x)
	if !x.toTweet([2]string{}).Metrics.ViewsKnown {
		t.Fatal("impression à 0 prise pour absente")
	}
}

func TestStatsFilters(t *testing.T) {
	e, _ := statsEnv(t)
	_ = e.st.SaveAccount(&Account{Name: "other", Handle: "@autre", Backend: "dryrun", Timezone: "UTC", Slots: []string{"09:00"}, DailyLimit: 1})
	day := 24 * time.Hour
	m := func(v int, r float64) *Metrics {
		return &Metrics{Views: v, ViewsKnown: true, EngagementRate: r, UpdatedAt: e.now}
	}
	addPublished(t, e, "a1", 2*day, m(100, 4), nil)
	addPublished(t, e, "a2", 40*day, m(200, 6), nil)
	addPublished(t, e, "b1", 3*day, m(300, 8), func(p *Post) { p.Account = "other" })
	d := e.st.store.Snapshot()
	ids := func(f StatsFilter) []string {
		var o []string
		for _, p := range ComputeStats(d, f, e.now).Posts {
			o = append(o, p.ID)
		}
		return o
	}
	if g := ids(StatsFilter{}); len(g) != 3 {
		t.Fatalf("tous : %v", g)
	}
	if g := ids(StatsFilter{Account: "main"}); len(g) != 2 {
		t.Fatalf("compte : %v", g)
	}
	if g := ids(StatsFilter{Days: 30}); len(g) != 2 || g[0] != "a1" || g[1] != "b1" {
		t.Fatalf("période : %v", g)
	}
	if g := ids(StatsFilter{Account: "main", Days: 30}); len(g) != 1 || g[0] != "a1" {
		t.Fatalf("compte+période : %v", g)
	}
	if v := ComputeStats(d, StatsFilter{Account: "main", Days: 30}, e.now); v.Totals.Views == nil || *v.Totals.Views != 100 {
		t.Fatalf("totaux filtrés : %+v", v.Totals)
	}
}

func TestStatsCompareByFormatAndOrigin(t *testing.T) {
	e, _ := statsEnv(t)
	m := func(r float64) *Metrics {
		return &Metrics{Views: 100, ViewsKnown: true, EngagementRate: r, UpdatedAt: e.now}
	}
	long := string(make([]rune, 0))
	for i := 0; i < 300; i++ {
		long += "x"
	}
	addPublished(t, e, "s1", time.Hour, m(2), nil)
	addPublished(t, e, "s2", time.Hour, m(4), nil)
	addPublished(t, e, "l1", time.Hour, m(10), func(p *Post) { p.Text = long; p.IdeaID = "i1"; p.MediaFile = "f.png" })
	addPublished(t, e, "r1", time.Hour, nil, func(p *Post) { p.Kind = "reply"; p.CampaignID = "c1" })
	v := ComputeStats(e.st.store.Snapshot(), StatsFilter{}, e.now)
	find := func(dim, key string) StatGroup {
		for _, g := range v.Compare[dim] {
			if g.Key == key {
				return g
			}
		}
		t.Fatalf("groupe %s/%s absent : %+v", dim, key, v.Compare[dim])
		return StatGroup{}
	}
	if g := find("length", "court"); g.Posts != 3 || g.MedianEngagement == nil || *g.MedianEngagement != 3 || g.WithEngagement != 2 {
		t.Fatalf("court : %+v", g)
	}
	if g := find("length", "long"); g.Posts != 1 || *g.MedianEngagement != 10 {
		t.Fatalf("long : %+v", g)
	}
	if g := find("media", "avec"); g.Posts != 1 {
		t.Fatalf("visuel : %+v", g)
	}
	if g := find("origin", "campagne"); g.Posts != 1 || g.MedianEngagement != nil || g.Views != nil {
		t.Fatalf("campagne sans mesure doit rester indisponible : %+v", g)
	}
	if g := find("origin", "idée"); g.Posts != 1 {
		t.Fatalf("idée : %+v", g)
	}
	if g := find("kind", "reply"); g.Posts != 1 {
		t.Fatalf("kind : %+v", g)
	}
	if v.Note == "" {
		t.Fatal("note manquante")
	}
}

func TestFollowersDailyTracking(t *testing.T) {
	e, x := statsEnv(t)
	acc := e.st.store.Snapshot().Account("main")
	acc.Backend = "agentx"
	_ = e.st.SaveAccount(acc)
	_ = e.st.SaveAccount(&Account{Name: "dry", Handle: "@d", Backend: "dryrun", Timezone: "UTC", Slots: []string{"09:00"}, DailyLimit: 1})
	ctx := context.Background()
	if n, _ := e.st.RecordFollowers(ctx); n != 1 {
		t.Fatalf("n=%d", n)
	}
	if n, _ := e.st.RecordFollowers(ctx); n != 0 {
		t.Fatal("deux relevés le même jour")
	}
	e.now = e.now.Add(24 * time.Hour)
	x.followers = 112
	if n, _ := e.st.RecordFollowers(ctx); n != 1 {
		t.Fatal("pas de relevé le lendemain")
	}
	// nombre non renvoyé : aucune ligne, jamais 0
	e.now = e.now.Add(24 * time.Hour)
	x.followers = 0
	if n, err := e.st.RecordFollowers(ctx); n != 0 || err == nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	v := ComputeStats(e.st.store.Snapshot(), StatsFilter{}, e.now)
	got := map[string]FollowerSeries{}
	for _, f := range v.Followers {
		got[f.Account] = f
	}
	if f := got["main"]; !f.Available || len(f.Points) != 2 || f.Change == nil || *f.Change != 12 {
		t.Fatalf("main : %+v", f)
	}
	if f := got["dry"]; f.Available || f.Unavailable == "" || len(f.Points) != 0 || f.Change != nil {
		t.Fatalf("dry : %+v", f)
	}
	// aucun relevé encore : indisponible
	if f := followerSeries(&Data{}, acc, time.Time{}); f.Unavailable == "" || f.Change != nil {
		t.Fatalf("%+v", f)
	}
}

func TestStatsEndpointValidatesFilters(t *testing.T) {
	t.Setenv("XGROWTH_HOME", t.TempDir())
	store, err := OpenStore(homeDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(buildHandler(NewStudio(store), ServeOptions{NoAuth: true}))
	defer srv.Close()
	c := &http.Client{}
	if st, m := call(t, c, "GET", srv.URL+"/api/stats?days=30", ""); st != 200 {
		t.Fatalf("%d %v", st, m)
	} else if d := m["data"].(map[string]any); d["posts"] == nil || d["followers"] == nil {
		t.Fatalf("listes nulles : %v", d)
	}
	for _, q := range []string{"account=nope", "days=abc", "days=-1"} {
		if st, _ := call(t, c, "GET", srv.URL+"/api/stats?"+q, ""); st != 400 {
			t.Errorf("%s : %d", q, st)
		}
	}
}
