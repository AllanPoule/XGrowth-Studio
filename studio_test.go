package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------- doublures

type fakeAI struct {
	calls      int
	lastSys    string
	lastMsg    string
	lastSchema map[string]any
	perf       func(user string) []map[string]any // réponse injectée de l'analyse des performances
}

func (f *fakeAI) JSON(_ context.Context, system, user string, schema map[string]any, out any) error {
	f.calls++
	f.lastSys, f.lastMsg, f.lastSchema = system, user, schema
	var resp any
	props := schema["properties"].(map[string]any)
	switch {
	case props["drafts"] != nil:
		drafts := []map[string]string{
			{"text": "Premier brouillon", "rationale": "r1"},
			{"text": "Second brouillon", "rationale": "r2"},
		}
		// le schéma d'un brouillon peut demander un type (le premier permis) et un visuel
		item := props["drafts"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		for _, d := range drafts {
			if ty, ok := item["type"].(map[string]any); ok {
				d["type"] = ty["enum"].([]any)[0].(string)
			}
			if item["visual"] != nil {
				d["visual"] = "Un schéma simple"
			}
		}
		resp = map[string]any{"drafts": drafts}
	case props["ideas"] != nil:
		resp = map[string]any{"ideas": []map[string]string{
			{"title": "Idée A", "angle": "angle A", "why": "pourquoi A"},
			{"title": "Idée B", "angle": "angle B", "why": "pourquoi B"},
			{"title": "Idée C", "angle": "angle C", "why": "pourquoi C"},
		}}
	case props["proposals"] != nil:
		if f.perf != nil {
			resp = map[string]any{"proposals": f.perf(user)}
			break
		}
		// une règle par clé de signal lue dans la demande (« - [type:utile] … »)
		var props []map[string]any
		for _, part := range strings.Split(user, "<signaux>")[1:] {
			part = part[:strings.Index(part, "</signaux>")]
			for _, line := range strings.Split(part, "\n- [")[1:] {
				k := line[:strings.Index(line, "]")]
				props = append(props, map[string]any{"text": "Privilégie le format " + k, "rationale": "test", "signals": []string{k}})
			}
		}
		resp = map[string]any{"proposals": props}
	case props["rules"] != nil:
		// une règle par retour cité (identifiants entre crochets)
		var rules []map[string]any
		for _, part := range strings.Split(user, "- [")[1:] {
			id := part[:strings.Index(part, "]")]
			rules = append(rules, map[string]any{"text": "Règle tirée de " + id, "rationale": "test", "evidence": []string{id}})
		}
		if len(rules) > 0 {
			rules = append(rules, map[string]any{"text": "Règle commune", "rationale": "deux retours", "evidence": []string{rules[0]["evidence"].([]string)[0], rules[len(rules)-1]["evidence"].([]string)[0], "inconnu"}})
		}
		resp = map[string]any{"rules": rules}
	case props["results"] != nil && strings.Contains(user, "<campagne"):
		// qualifie les candidats qui « galèrent », avec la première douleur active
		pain := ""
		if i := strings.Index(user, `<douleur id="`); i >= 0 {
			pain = user[i+13:]
			pain = pain[:strings.Index(pain, `"`)]
		}
		var res []map[string]any
		for _, part := range strings.Split(user, "<contenu_x id=\"")[1:] {
			id := part[:strings.Index(part, "\"")]
			ok := strings.Contains(part, "galère")
			reply, score := "", 1.0
			if ok {
				reply, score = "Essaie de couper le problème en deux.", 4
			}
			res = append(res, map[string]any{"id": id, "qualify": ok, "score": score, "pain_id": pain, "reason": "test", "reply": reply})
		}
		resp = map[string]any{"results": res}
	case props["results"] != nil:
		// garde les contenus dont le texte contient "agent"
		var res []map[string]any
		for _, line := range strings.Split(user, "<contenu_x id=\"")[1:] {
			id := line[:strings.Index(line, "\"")]
			keep := strings.Contains(line, "agent")
			score := 3.0
			if keep {
				score = 8
			}
			res = append(res, map[string]any{"id": id, "score": score, "keep": keep, "reason": "test", "angle": "angle"})
		}
		resp = map[string]any{"results": res}
	default:
		resp = map[string]string{"text": "Texte réécrit", "rationale": "plus court"}
	}
	b, _ := json.Marshal(resp)
	return json.Unmarshal(b, out)
}

type fakeX struct {
	posted []PostRequest
	feed   []Tweet
	fail   error
	me     string
}

func (f *fakeX) Name() string { return "fake" }
func (f *fakeX) Me(context.Context) (*Profile, error) {
	return &Profile{ScreenName: f.me, FollowersCount: 42}, nil
}
func (f *fakeX) Post(_ context.Context, r PostRequest) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	f.posted = append(f.posted, r)
	return "100" + string(rune('0'+len(f.posted))), nil
}
func (f *fakeX) Search(context.Context, string, int) ([]Tweet, error)    { return f.feed, nil }
func (f *fakeX) UserPosts(context.Context, string, int) ([]Tweet, error) { return f.feed, nil }
func (f *fakeX) Bookmarks(context.Context, int) ([]Tweet, error)         { return f.feed, nil }
func (f *fakeX) Tweet(_ context.Context, id string) (*Tweet, error) {
	t := Tweet{ID: id, Text: "Post cible sur les agents"}
	t.Author.ScreenName = "someone"
	t.Metrics.Views, t.Metrics.Likes = 1000, 50
	return &t, nil
}

func tw(id, author, text string) Tweet {
	t := Tweet{ID: id, Text: text}
	t.Author.ScreenName = author
	return t
}

type env struct {
	st  *Studio
	ai  *fakeAI
	x   *fakeX
	now time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &env{ai: &fakeAI{}, x: &fakeX{}, now: time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)}
	e.st = NewStudio(store)
	e.st.ai = func(Settings) AI { return e.ai }
	e.st.backendFor = func(*Account) (XBackend, error) { return e.x, nil }
	e.st.now = func() time.Time { return e.now }
	if err := e.st.SaveAccount(&Account{Name: "main", Handle: "@moi", Backend: "dryrun", Timezone: "UTC",
		Slots: []string{"09:00", "13:00", "18:00"}, DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) draft(t *testing.T, text string) *Post {
	t.Helper()
	p, err := e.st.AddDraft("main", text, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ---------------------------------------------------------------- tests

func TestGuardBlocksCompetitorsLengthAndDuplicates(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error {
		d.Settings.Competitors = []string{"Acme"}
		d.Settings.BannedPhrases = []string{"révolutionnaire"}
		return nil
	})
	d := e.st.store.Snapshot()
	a := d.Account("main")
	cases := map[string]bool{
		"Bonjour":                      true,
		"Mieux que @acme, évidemment":  false,
		"Chez ACME on fait pareil":     false,
		"Acmeville est une jolie cité": true, // pas un mot entier
		"Un outil révolutionnaire":     false,
		strings.Repeat("é", 281):       false,
		"   ":                          false,
	}
	for text, ok := range cases {
		if err := Guard(d, a, text); (err == nil) != ok {
			t.Errorf("Guard(%q) = %v, attendu ok=%v", text, err, ok)
		}
	}
	a.Premium = true
	if err := Guard(d, a, strings.Repeat("é", 281)); err != nil {
		t.Errorf("un compte Premium doit accepter 281 caractères : %v", err)
	}
}

func TestValidateRequiresSignatureAndUsesSlots(t *testing.T) {
	e := newEnv(t)
	p1, p2, p3 := e.draft(t, "un"), e.draft(t, "deux"), e.draft(t, "trois")
	if _, err := e.st.Validate(p1.ID, "", nil, false); err == nil {
		t.Fatal("une validation sans signature doit être refusée")
	}
	v1, err := e.st.Validate(p1.ID, "Allan", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if v1.Status != StatusScheduled || !v1.ScheduledAt.Equal(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("premier créneau attendu 09:00, obtenu %v %v", v1.Status, v1.ScheduledAt)
	}
	v2, _ := e.st.Validate(p2.ID, "Allan", nil, false)
	if !v2.ScheduledAt.Equal(time.Date(2026, 11, 18, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("deuxième créneau attendu 13:00, obtenu %v", v2.ScheduledAt)
	}
	// limite de 2 posts par jour : le troisième passe au lendemain
	v3, _ := e.st.Validate(p3.ID, "Allan", nil, false)
	if !v3.ScheduledAt.Equal(time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("troisième créneau attendu le 19 à 09:00, obtenu %v", v3.ScheduledAt)
	}
	if _, err := e.st.Validate(p1.ID, "Allan", nil, false); err == nil {
		t.Fatal("un post déjà validé ne doit pas être revalidé")
	}
}

func TestOnlyValidatedDuePostsArePublished(t *testing.T) {
	e := newEnv(t)
	draft := e.draft(t, "jamais validé")
	due := e.draft(t, "à publier")
	later := e.draft(t, "plus tard")
	if _, err := e.st.Validate(due.ID, "Allan", nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Validate(later.ID, "Allan", nil, false); err != nil {
		t.Fatal(err)
	}
	res := e.st.PublishDue(context.Background())
	if len(res) != 1 || res[0].PostID != due.ID || res[0].Status != StatusPublished {
		t.Fatalf("résultat inattendu : %+v", res)
	}
	if len(e.x.posted) != 1 || e.x.posted[0].Text != "à publier" {
		t.Fatalf("publications : %+v", e.x.posted)
	}
	d := e.st.store.Snapshot()
	if d.Post(draft.ID).Status != StatusDraft || d.Post(later.ID).Status != StatusScheduled {
		t.Fatal("le brouillon et le post futur ne doivent pas bouger")
	}
	// un second passage ne republie rien
	if r := e.st.PublishDue(context.Background()); len(r) != 0 {
		t.Fatalf("double publication : %+v", r)
	}
	// à 09:00 le post planifié part
	e.now = time.Date(2026, 11, 18, 9, 0, 30, 0, time.UTC)
	if r := e.st.PublishDue(context.Background()); len(r) != 1 {
		t.Fatalf("le post de 09:00 aurait dû partir : %+v", r)
	}
}

func TestEditResetsValidation(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "v1")
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatal(err)
	}
	ed, err := e.st.EditPost(p.ID, "v2 modifiée")
	if err != nil {
		t.Fatal(err)
	}
	if ed.Status != StatusDraft || ed.ValidatedBy != "" || ed.ScheduledAt != nil {
		t.Fatalf("une modification doit annuler la validation : %+v", ed)
	}
	if r := e.st.PublishDue(context.Background()); len(r) != 0 || len(e.x.posted) != 0 {
		t.Fatal("un post modifié après validation ne doit pas partir")
	}
}

func TestGuardRecheckedAtPublishTime(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "Nous comparons avec Globex")
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatal(err)
	}
	// le concurrent est ajouté après la validation
	_ = e.st.store.Update(func(d *Data) error { d.Settings.Competitors = []string{"Globex"}; return nil })
	res := e.st.PublishDue(context.Background())
	if len(e.x.posted) != 0 || len(res) != 1 || res[0].Status != "skipped" {
		t.Fatalf("le post aurait dû être bloqué : %+v", res)
	}
	if st := e.st.store.Snapshot().Post(p.ID).Status; st != StatusFailed {
		t.Fatalf("statut attendu failed, obtenu %s", st)
	}
}

func TestPublishFailureIsRecordedNotRetried(t *testing.T) {
	e := newEnv(t)
	e.x.fail = os.ErrDeadlineExceeded
	p := e.draft(t, "boum")
	_, _ = e.st.Validate(p.ID, "Allan", nil, true)
	res := e.st.PublishDue(context.Background())
	if len(res) != 1 || res[0].Status != StatusFailed {
		t.Fatalf("%+v", res)
	}
	e.x.fail = nil
	if r := e.st.PublishDue(context.Background()); len(r) != 0 {
		t.Fatal("un échec ne doit pas être relancé sans nouvelle validation")
	}
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatalf("un post en échec doit pouvoir être revalidé : %v", err)
	}
}

func TestWatchTriageAndAutoDraft(t *testing.T) {
	e := newEnv(t)
	if _, err := e.st.AddWatch("main", "user", "@source", 30); err != nil {
		t.Fatal(err)
	}
	_ = e.st.store.Update(func(d *Data) error {
		a := d.Account("main")
		a.AutoTriage, a.AutoDraft = true, true
		return nil
	})
	rt := tw("3", "x", "un retweet")
	rt.IsRetweet = true
	e.x.feed = []Tweet{tw("1", "a", "Les agents IA en production"), tw("2", "b", "La météo du jour"), rt}
	res := e.st.PollWatch(context.Background(), "", false)
	if len(res) != 1 || res[0].Added != 2 {
		t.Fatalf("veille : %+v", res)
	}
	// pas encore à échéance : rien de nouveau
	if r := e.st.PollWatch(context.Background(), "", false); len(r) != 0 {
		t.Fatalf("la cible n'était pas à échéance : %+v", r)
	}
	// dédoublonnage
	if r := e.st.PollWatch(context.Background(), "", true); r[0].Added != 0 {
		t.Fatalf("doublons ajoutés : %+v", r)
	}
	tr := e.st.TriagePending(context.Background(), "", true, false)
	if len(tr) != 1 || tr[0].Triaged != 2 || tr[0].Kept != 1 || tr[0].Drafted != 1 {
		t.Fatalf("tri : %+v", tr)
	}
	d := e.st.store.Snapshot()
	var drafts int
	for _, p := range d.Posts {
		if p.Status == StatusDraft && p.SourceItem != "" {
			drafts++
		}
	}
	if drafts != 2 { // le faux Claude renvoie toujours 2 brouillons
		t.Fatalf("brouillons auto attendus : 2, obtenus %d", drafts)
	}
	if !strings.Contains(e.ai.lastMsg, "Les agents IA en production") || !strings.Contains(e.ai.lastSys, "données à commenter") {
		t.Fatal("le contenu de veille doit être transmis comme donnée balisée")
	}
	// le contenu jugé peu pertinent n'est PAS ignoré : il reste à trier, avec la suggestion
	for _, it := range d.Items {
		switch it.TweetID {
		case "1":
			if it.Status != ItemDrafted || it.Suggestion != SuggestKeep {
				t.Fatalf("contenu suggéré : %+v", it)
			}
		case "2":
			if it.Status != ItemPending || it.Suggestion != SuggestSkip || it.TriagedAt == nil {
				t.Fatalf("un contenu écarté par l'IA doit rester à trier : %+v", it)
			}
		}
	}
	// un second passage ne renote pas ce qui a déjà été trié
	calls := e.ai.calls
	if tr := e.st.TriagePending(context.Background(), "", true, false); tr[0].Triaged != 0 || e.ai.calls != calls {
		t.Fatalf("contenus retriés : %+v", tr)
	}
}

func TestTriageWithoutAutoDraftChangesNoStatus(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error {
		d.Items = append(d.Items,
			&Item{ID: "i1", Account: "main", TweetID: "1", Text: "agents en prod", Status: ItemPending},
			&Item{ID: "i2", Account: "main", TweetID: "2", Text: "météo", Status: ItemPending})
		return nil
	})
	tr := e.st.TriagePending(context.Background(), "main", false, false)
	if tr[0].Triaged != 2 || tr[0].Kept != 1 {
		t.Fatalf("%+v", tr)
	}
	for _, it := range e.st.store.Snapshot().Items {
		if it.Status != ItemPending {
			t.Fatalf("le tri seul ne doit changer aucun statut : %+v", it)
		}
	}
}

func TestMigrationRestoresItemsIgnoredByAI(t *testing.T) {
	dir := t.TempDir()
	old := `{"version":1,"settings":{"minGapMinutes":60,"maxItemsPerRun":40},"items":[
	 {"id":"a","account":"main","status":"ignored","score":3,"reason":"hors sujet","fetchedAt":"2026-11-18T10:00:00Z"},
	 {"id":"b","account":"main","status":"kept","score":8,"fetchedAt":"2026-11-18T10:00:00Z"},
	 {"id":"c","account":"main","status":"pending","score":0,"fetchedAt":"2026-11-18T10:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := store.Snapshot()
	if d.Version != dataVersion {
		t.Fatalf("version %d", d.Version)
	}
	if a := d.Item("a"); a.Status != ItemPending || a.Suggestion != SuggestSkip || a.TriagedAt == nil {
		t.Fatalf("contenu écarté par l'IA non restauré : %+v", a)
	}
	if b := d.Item("b"); b.Status != ItemKept || b.Suggestion != SuggestKeep {
		t.Fatalf("%+v", b)
	}
	if c := d.Item("c"); c.Status != ItemPending || c.TriagedAt != nil {
		t.Fatalf("un contenu jamais trié doit rester à trier par l'IA : %+v", c)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "data.json"))
	if !strings.Contains(string(raw), `"version": 2`) {
		t.Fatal("la migration doit être écrite sur disque")
	}
}

func TestRewriteStoresLearningAndFeedsPrompt(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "trop long et trop formel")
	rp, err := e.st.RewritePost(context.Background(), p.ID, "plus court, tutoie")
	if err != nil {
		t.Fatal(err)
	}
	if rp.Text != "Texte réécrit" || len(rp.History) != 1 {
		t.Fatalf("%+v", rp)
	}
	if _, err := e.st.GenerateDrafts(context.Background(), "main", "sujet", "", 2, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.ai.lastSys, "plus court, tutoie") {
		t.Fatal("la leçon doit être injectée dans le prompt système")
	}
}

func TestReplyDraftKeepsTarget(t *testing.T) {
	e := newEnv(t)
	p, err := e.st.DraftReply(context.Background(), "main", "https://x.com/someone/status/123?s=20", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "reply" || p.ReplyTo != "123" || p.TargetText == "" {
		t.Fatalf("%+v", p)
	}
	_, _ = e.st.Validate(p.ID, "Allan", nil, true)
	e.st.PublishDue(context.Background())
	if len(e.x.posted) != 1 || e.x.posted[0].ReplyTo != "123" {
		t.Fatalf("%+v", e.x.posted)
	}
}

func TestMetricsEngagementRate(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "mesuré")
	_, _ = e.st.Validate(p.ID, "Allan", nil, true)
	e.st.PublishDue(context.Background())
	n, err := e.st.RefreshMetrics(context.Background(), 0)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	m := e.st.store.Snapshot().Post(p.ID).Metrics
	if m == nil || m.Views != 1000 || m.EngagementRate != 5 {
		t.Fatalf("%+v", m)
	}
}

func TestStoreSharedBetweenProcesses(t *testing.T) {
	dir := t.TempDir()
	s1, _ := OpenStore(dir)
	s2, _ := OpenStore(dir)
	_ = s1.Update(func(d *Data) error { d.Learnings = append(d.Learnings, Learning{Text: "a"}); return nil })
	time.Sleep(10 * time.Millisecond)
	_ = s2.Update(func(d *Data) error { d.Learnings = append(d.Learnings, Learning{Text: "b"}); return nil })
	if n := len(s1.Snapshot().Learnings); n != 2 {
		t.Fatalf("les deux écritures doivent être conservées, obtenu %d", n)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "data.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("permissions %v", fi.Mode().Perm())
	}
}

func TestAgentxBackendWithFakeBinary(t *testing.T) {
	dir := t.TempDir()
	logf := filepath.Join(dir, "args.log")
	script := `#!/bin/sh
printf '%s\n' "$@" > ` + logf + `
case "$3" in
  me) echo '{"ok":true,"schema_version":"1","data":{"id":"9","name":"Moi","screenName":"moi","followersCount":120,"followingCount":3,"tweetsCount":50}}';;
  post|reply|quote) echo '{"ok":true,"schema_version":"1","data":{"id":"777","text":"x","author":{"screenName":"moi"},"metrics":{}}}';;
  search) echo '{"ok":true,"schema_version":"1","data":[{"id":"1","text":"hello","author":{"screenName":"a"},"metrics":{"likes":3,"views":40}}]}';;
  *) echo '{"ok":false,"schema_version":"1","error":{"code":"auth_expired","message":"cookie expiré"}}'; exit 1;;
esac
`
	bin := filepath.Join(dir, "agentx")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTX_BIN", bin)
	be, err := BackendFor(&Account{Name: "main", Backend: "agentx", AgentxAccount: "perso"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := be.Post(context.Background(), PostRequest{Text: "--salut à tous", ReplyTo: "42"})
	if err != nil || id != "777" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	args, _ := os.ReadFile(logf)
	if got := strings.TrimSpace(string(args)); got != "-a\nperso\nreply\n42\n--salut à tous" {
		t.Fatalf("arguments passés à agentx :\n%s", got)
	}
	me, err := be.Me(context.Background())
	if err != nil || me.ScreenName != "moi" || me.FollowersCount != 120 {
		t.Fatalf("me=%+v err=%v", me, err)
	}
	ts, err := be.Search(context.Background(), "golang", 5)
	if err != nil || len(ts) != 1 || ts[0].Metrics.Views != 40 {
		t.Fatalf("ts=%+v err=%v", ts, err)
	}
	if _, err := be.Bookmarks(context.Background(), 5); err == nil || !strings.Contains(err.Error(), "cookie expiré") {
		t.Fatalf("l'erreur agentx doit remonter : %v", err)
	}
}

func TestWrongConnectedAccountBlocksPublishing(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error { d.Account("main").Backend = "agentx"; return nil })
	e.x.me = "un_autre_compte"
	r, err := e.st.CheckAccount(context.Background(), "main")
	if err != nil || r.OK || !strings.Contains(r.Problem, "@un_autre_compte") {
		t.Fatalf("la vérification doit signaler le mauvais compte : %+v %v", r, err)
	}
	p := e.draft(t, "ne doit pas partir")
	_, _ = e.st.Validate(p.ID, "Allan", nil, true)
	res := e.st.PublishDue(context.Background())
	if len(e.x.posted) != 0 || len(res) != 1 || res[0].Status != StatusFailed {
		t.Fatalf("l'envoi depuis le mauvais compte doit être annulé : %+v", res)
	}
	// bon compte (casse indifférente) : la publication passe après revalidation
	e.x.me = "MOI"
	if r, _ := e.st.CheckAccount(context.Background(), "main"); !r.OK {
		t.Fatalf("%+v", r)
	}
	_, _ = e.st.Validate(p.ID, "Allan", nil, true)
	if res := e.st.PublishDue(context.Background()); len(res) != 1 || res[0].Status != StatusPublished {
		t.Fatalf("%+v", res)
	}
}

func TestParseArgs(t *testing.T) {
	a := parseArgs([]string{"main", "--topic", "IA", "-n", "2", "--now", "--at=+2h", "reste"})
	if a.arg(0) != "main" || a.str("topic") != "IA" || a.integer("count", 0) != 2 || !a.boolean("now") || a.str("at") != "+2h" || a.rest(1) != "reste" {
		t.Fatalf("%+v", a)
	}
	if parseTweetID("https://twitter.com/u/status/99/photo/1") != "99" {
		t.Fatal("parseTweetID")
	}
}
