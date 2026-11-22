package main

// Lot 2 LinkedIn : OAuth, publication par l'API, programmation, expiration.
// Aucun appel réseau réel : un faux serveur httptest simule LinkedIn. Les
// jetons sont des valeurs factices évidentes.

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fakeToken  = "FAKE-TOKEN-do-not-leak-0001"
	fakeSecret = "FAKE-SECRET-do-not-leak-0002"
	fakeSub    = "abc123"
)

type liReq struct {
	Method, Path string
	Header       http.Header
	Body         []byte
}

// liFake simule LinkedIn : autorisation (non utilisée, le test la suit à la
// main), jeton, userinfo, images, posts.
type liFake struct {
	srv        *httptest.Server
	mu         sync.Mutex
	reqs       []liReq
	sub        string
	postStatus int
}

func newLiFake(t *testing.T) *liFake {
	t.Helper()
	f := &liFake{sub: fakeSub, postStatus: http.StatusCreated}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, liReq{r.Method, r.URL.RequestURI(), r.Header.Clone(), body})
		status, sub := f.postStatus, f.sub
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/oauth/v2/accessToken":
			_, _ = w.Write([]byte(`{"access_token":"` + fakeToken + `","expires_in":5184000}`))
		case r.URL.Path == "/v2/userinfo":
			_, _ = w.Write([]byte(`{"sub":"` + sub + `","name":"Allan Test"}`))
		case r.URL.Path == "/rest/images":
			_, _ = w.Write([]byte(`{"value":{"uploadUrl":"` + f.srv.URL + `/upload/img1","image":"urn:li:image:IMG1"}}`))
		case strings.HasPrefix(r.URL.Path, "/upload/"):
			w.WriteHeader(http.StatusCreated)
		case r.URL.Path == "/rest/posts":
			if status != http.StatusCreated {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"refus simulé ` + fakeToken + `"}`))
				return
			}
			w.Header().Set("x-restli-id", "urn:li:share:FAKE1")
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *liFake) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if strings.HasPrefix(r.Path, path) {
			n++
		}
	}
	return n
}

func (f *liFake) total() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.reqs) }

func (f *liFake) cfg() liConfig {
	c := liConfig{ClientID: "fake-client", ClientSecret: fakeSecret, RedirectURI: "https://studio.test/auth/linkedin/callback",
		AuthURL: f.srv.URL + "/oauth/v2/authorization", TokenURL: f.srv.URL + "/oauth/v2/accessToken", APIBase: f.srv.URL, insecure: true}
	c.defaults()
	return c
}

// liAPI : un studio avec un compte LinkedIn « pro » en backend linkedin,
// déjà connecté (jeton valide 30 jours) sauf connected=false.
func liAPI(t *testing.T, connected bool) (*env, *liFake) {
	t.Helper()
	e := newEnv(t)
	f := newLiFake(t)
	e.st.liCfg = f.cfg
	e.st.logf = func(string, ...any) {}
	e.st.backendFor = func(a *Account) (XBackend, error) {
		if isLinkedIn(a) && a.Backend == "linkedin" {
			return e.st.newLinkedInBackend(a), nil
		}
		return e.x, nil
	}
	if err := e.st.SaveAccount(&Account{Name: "pro", Platform: "linkedin", Line: "perso", Backend: "linkedin",
		Timezone: "UTC", Slots: []string{"09:00", "15:00"}}); err != nil {
		t.Fatal(err)
	}
	if connected {
		e.liConnect(t, 30*24*time.Hour)
	}
	return e, f
}

// liConnect pose un jeton factice valable `life` et l'auteur du compte.
func (e *env) liConnect(t *testing.T, life time.Duration) {
	t.Helper()
	if err := e.st.liTokens().update(func(f *liFile) {
		f.Accounts["pro"] = &liToken{AccessToken: fakeToken, ExpiresAt: e.now.Add(life), PersonID: fakeSub, DisplayName: "Allan Test", ConnectedAt: e.now}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.store.Update(func(d *Data) error { d.Account("pro").AuthorURN = "urn:li:person:" + fakeSub; return nil }); err != nil {
		t.Fatal(err)
	}
}

// validated : un post validé, programmé à `at`.
func (e *env) liValidated(t *testing.T, text string, at time.Time) *Post {
	t.Helper()
	p := e.liDraft(t, text)
	p, err := e.st.Validate(p.ID, "allan", &at, false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env) post(t *testing.T, id string) *Post {
	t.Helper()
	p := e.st.store.Snapshot().Post(id)
	if p == nil {
		t.Fatal("post introuvable")
	}
	return p
}

// noLeak échoue si une valeur de jeton ou de secret apparaît dans s.
func noLeak(t *testing.T, what, s string) {
	t.Helper()
	for _, secret := range []string{fakeToken, fakeSecret} {
		if strings.Contains(s, secret) {
			t.Errorf("%s : contient une valeur secrète", what)
		}
	}
}

// ---------------------------------------------------------------- B5

func TestEscapeLittle(t *testing.T) {
	cases := map[string]string{
		"Un test (très) #kinflux _a_ @x": `Un test \(très\) #kinflux \_a\_ \@x`,
		`a|b{c}d[e]f<g>h*i~j\k`:          `a\|b\{c\}d\[e\]f\<g\>h\*i\~j\\k`,
		"C# et # seul":                   `C\# et \# seul`,
		"#kin_flux, #2026 !":             "#kin_flux, #2026 !",
		"(@Allan)":                       `\(\@Allan\)`,
		"":                               "",
		"texte simple. Rien à échapper!": "texte simple. Rien à échapper!",
	}
	for in, want := range cases {
		if got := escapeLittle(in); got != want {
			t.Errorf("escapeLittle(%q) = %q, attendu %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------- B4, B6, B7

func TestLinkedInPostRequest(t *testing.T) {
	e, f := liAPI(t, true)
	p := e.liValidated(t, "Un test (simple) #kinflux", e.now.Add(-time.Minute))
	res := e.st.PublishDue(context.Background())
	if len(res) != 1 || res[0].Status != StatusPublished || res[0].PostURN != "urn:li:share:FAKE1" {
		t.Fatalf("résultat inattendu : %+v", res)
	}
	got := e.post(t, p.ID)
	if got.Status != StatusPublished || got.PostURN != "urn:li:share:FAKE1" {
		t.Fatalf("post : %s %q", got.Status, got.PostURN)
	}
	// identité (userinfo) puis publication, rien d'autre
	if f.total() != 2 || f.count("/v2/userinfo") != 1 || f.count("/rest/posts") != 1 {
		t.Fatalf("requêtes inattendues : %d", f.total())
	}
	r := f.reqs[1]
	if r.Header.Get("Authorization") != "Bearer "+fakeToken || r.Header.Get("X-Restli-Protocol-Version") != "2.0.0" ||
		r.Header.Get("Linkedin-Version") != linkedinDefaultAPIVersion || r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("en-têtes : %v", r.Header)
	}
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["author"] != "urn:li:person:"+fakeSub || body["commentary"] != `Un test \(simple\) #kinflux` ||
		body["visibility"] != "PUBLIC" || body["lifecycleState"] != "PUBLISHED" || body["isReshareDisabledByAuthor"] != false {
		t.Errorf("corps : %s", r.Body)
	}
	if d, _ := body["distribution"].(map[string]any); d["feedDistribution"] != "MAIN_FEED" {
		t.Errorf("distribution : %v", body["distribution"])
	}
	if _, ok := body["content"]; ok {
		t.Error("pas d'image : pas de content")
	}
}

func TestLinkedInPostRefusedWithoutValidation(t *testing.T) {
	e, f := liAPI(t, true)
	be := e.st.newLinkedInBackend(e.st.store.Snapshot().Account("pro"))
	for _, by := range []string{"", "   "} {
		if _, err := be.Post(context.Background(), PostRequest{Text: "x", ValidatedBy: by}); err == nil || !strings.Contains(err.Error(), "non validé") {
			t.Errorf("by=%q : err=%v", by, err)
		}
	}
	if f.total() != 0 {
		t.Fatalf("%d requête(s) envoyée(s) sans validation", f.total())
	}
	// commentaires et réponses : jamais par l'API
	if _, err := be.Post(context.Background(), PostRequest{Text: "x", ReplyTo: "1", ValidatedBy: "allan"}); err == nil {
		t.Error("une réponse ne doit pas partir par l'API")
	}
	if f.total() != 0 {
		t.Fatal("requête pour une réponse")
	}
}

// A5 version lot 2 : un post forgé « scheduled » sans validation repart en
// brouillon, sans aucune requête.
func TestLinkedInForgedPostNeverSent(t *testing.T) {
	e, f := liAPI(t, true)
	p := e.liDraft(t, "forgé")
	past := e.now.Add(-time.Hour)
	_ = e.st.store.Update(func(d *Data) error {
		q := d.Post(p.ID)
		q.Status, q.ScheduledAt = StatusScheduled, &past
		return nil
	})
	e.st.PublishDue(context.Background())
	if got := e.post(t, p.ID); got.Status != StatusDraft {
		t.Fatalf("statut %s", got.Status)
	}
	if f.total() != 0 {
		t.Fatal("requête envoyée pour un post non validé")
	}
}

func writePNG(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "visuel.png")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLinkedInPostWithImage(t *testing.T) {
	e, f := liAPI(t, true)
	p := e.liDraft(t, "Avec un visuel")
	if _, err := e.st.SetMedia(p.ID, writePNG(t)); err != nil {
		t.Fatal(err)
	}
	at := e.now.Add(-time.Minute)
	if _, err := e.st.Validate(p.ID, "allan", &at, false); err != nil {
		t.Fatal(err)
	}
	if res := e.st.PublishDue(context.Background()); len(res) != 1 || res[0].Status != StatusPublished {
		t.Fatalf("résultat : %+v", res)
	}
	var order []string
	for _, r := range f.reqs {
		order = append(order, r.Method+" "+strings.SplitN(r.Path, "?", 2)[0])
	}
	want := []string{"GET /v2/userinfo", "POST /rest/images", "PUT /upload/img1", "POST /rest/posts"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("ordre %v, attendu %v", order, want)
	}
	if !strings.Contains(f.reqs[1].Path, "action=initializeUpload") || !strings.Contains(string(f.reqs[1].Body), "urn:li:person:"+fakeSub) {
		t.Errorf("initializeUpload : %s %s", f.reqs[1].Path, f.reqs[1].Body)
	}
	if !bytes.HasPrefix(f.reqs[2].Body, []byte("\x89PNG")) {
		t.Error("le binaire envoyé n'est pas l'image")
	}
	var body struct {
		Content struct {
			Media struct {
				ID string `json:"id"`
			} `json:"media"`
		} `json:"content"`
	}
	_ = json.Unmarshal(f.reqs[3].Body, &body)
	if body.Content.Media.ID != "urn:li:image:IMG1" {
		t.Errorf("content.media.id = %q", body.Content.Media.ID)
	}
}

func TestLinkedInImageRefused(t *testing.T) {
	e, f := liAPI(t, true)
	be := e.st.newLinkedInBackend(e.st.store.Snapshot().Account("pro"))
	txt := filepath.Join(t.TempDir(), "faux.png")
	_ = os.WriteFile(txt, []byte("ce n'est pas une image"), 0o600)
	if _, err := be.Post(context.Background(), PostRequest{Text: "x", MediaPath: txt, ValidatedBy: "allan"}); err == nil {
		t.Fatal("un fichier qui n'est pas une image a été accepté")
	}
	if f.total() != 0 {
		t.Fatal("requête envoyée pour un visuel refusé")
	}
}

// B7 : erreur 4xx/5xx = failed, un seul appel de publication, aucun secret.
func TestLinkedInFailureNoRetryNoLeak(t *testing.T) {
	e, f := liAPI(t, true)
	f.postStatus = http.StatusInternalServerError
	p := e.liValidated(t, "va échouer", e.now.Add(-time.Minute))
	res := e.st.PublishDue(context.Background())
	got := e.post(t, p.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "HTTP 500") {
		t.Fatalf("statut %s, erreur %q", got.Status, got.Error)
	}
	e.st.PublishDue(context.Background())
	e.st.PublishDue(context.Background())
	if n := f.count("/rest/posts"); n != 1 {
		t.Fatalf("%d appels de publication, attendu 1 (jamais de relance)", n)
	}
	b, _ := json.Marshal([]any{res, e.st.store.Snapshot()})
	noLeak(t, "résultat et données", string(b))
}

// ---------------------------------------------------------------- B8

func TestLinkedInExpiredTokenWaitsThenReturnsToDraft(t *testing.T) {
	e, f := liAPI(t, true)
	due := e.now.Add(time.Hour)
	p := e.liValidated(t, "attend la reconnexion", due)
	// le jeton expire avant l'échéance
	e.liConnect(t, 30*time.Minute)
	e.now = due.Add(time.Minute)
	res := e.st.PublishDue(context.Background())
	got := e.post(t, p.ID)
	if got.Status != StatusScheduled || got.Error != "jeton LinkedIn expiré : reconnectez le compte" || got.ScheduledAt == nil {
		t.Fatalf("statut %s, erreur %q", got.Status, got.Error)
	}
	if len(res) != 1 || res[0].Status != "skipped" || f.total() != 0 {
		t.Fatalf("résultat %+v, %d requête(s)", res, f.total())
	}
	// à +25 h de retard : retour en brouillon, validation effacée
	e.now = due.Add(25 * time.Hour)
	e.st.PublishDue(context.Background())
	got = e.post(t, p.ID)
	if got.Status != StatusDraft || got.ValidatedBy != "" || !strings.Contains(got.Error, "à revalider") {
		t.Fatalf("après 25 h : %s %q validé par %q", got.Status, got.Error, got.ValidatedBy)
	}
	if f.total() != 0 {
		t.Fatal("requête envoyée avec un jeton expiré")
	}
}

func TestLinkedInReconnectResumesPublication(t *testing.T) {
	e, f := liAPI(t, true)
	due := e.now.Add(time.Hour)
	p := e.liValidated(t, "reprend", due)
	e.liConnect(t, 30*time.Minute) // expirera avant l'échéance
	e.now = due.Add(2 * time.Hour)
	e.st.PublishDue(context.Background())
	if got := e.post(t, p.ID); got.Status != StatusScheduled {
		t.Fatalf("statut %s", got.Status)
	}
	e.liConnect(t, 60*24*time.Hour) // reconnexion
	res := e.st.PublishDue(context.Background())
	if len(res) != 1 || res[0].Status != StatusPublished {
		t.Fatalf("résultat : %+v", res)
	}
	if f.count("/rest/posts") != 1 {
		t.Fatal("publication attendue une fois")
	}
}

func TestLinkedInDisconnectedWaits(t *testing.T) {
	e, f := liAPI(t, false)
	p := e.liValidated(t, "pas connecté", e.now.Add(-time.Minute))
	e.st.PublishDue(context.Background())
	got := e.post(t, p.ID)
	if got.Status != StatusScheduled || !strings.Contains(got.Error, "non connecté") || f.total() != 0 {
		t.Fatalf("statut %s, erreur %q, %d requête(s)", got.Status, got.Error, f.total())
	}
}

func TestLinkedInSendToManual(t *testing.T) {
	e, f := liAPI(t, false)
	p := e.liValidated(t, "à la main", e.now.Add(time.Hour))
	got, err := e.st.SendToManual(p.ID)
	if err != nil || got.Status != StatusManual || got.ValidatedBy != "allan" {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := e.st.MarkPublished(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	// un brouillon (non validé) ne peut pas passer en manuel
	d := e.liDraft(t, "brouillon")
	if _, err := e.st.SendToManual(d.ID); err == nil {
		t.Error("un brouillon a été envoyé en manuel")
	}
	if f.total() != 0 {
		t.Fatal("aucune requête attendue")
	}
}

// ---------------------------------------------------------------- B9

func TestLinkedInExpiryAlerts(t *testing.T) {
	e, _ := liAPI(t, false)
	exp := e.now.Add(60 * 24 * time.Hour)
	set := func() {
		_ = e.st.liTokens().update(func(f *liFile) {
			f.Accounts["pro"] = &liToken{AccessToken: fakeToken, ExpiresAt: exp, PersonID: fakeSub}
		})
	}
	set()
	warns := func() int {
		n := 0
		for _, l := range e.st.store.Snapshot().Log {
			if l.Level == "warn" && strings.Contains(l.Message, "jeton LinkedIn") {
				n++
				noLeak(t, "journal", l.Message)
			}
		}
		return n
	}
	steps := []struct {
		left time.Duration
		want int
	}{
		{30 * 24 * time.Hour, 0},
		{15 * 24 * time.Hour, 0},
		{14 * 24 * time.Hour, 1}, // J-14
		{13 * 24 * time.Hour, 1}, // pas de doublon
		{7 * 24 * time.Hour, 2},  // J-7
		{3*24*time.Hour + time.Hour, 2},
		{3 * 24 * time.Hour, 3}, // J-3
		{24 * time.Hour, 4},     // J-1
		{12 * time.Hour, 4},
		{0, 5}, // expiration
		{-time.Hour, 5},
	}
	for _, s := range steps {
		e.now = exp.Add(-s.left)
		e.st.Tick(context.Background())
		e.st.Tick(context.Background()) // le passage suivant ne double rien
		if got := warns(); got != s.want {
			t.Fatalf("à %v de l'expiration : %d alerte(s), attendu %d", s.left, got, s.want)
		}
	}
	// reconnexion : les seuils se réarment
	e.now = exp.Add(time.Hour)
	exp = e.now.Add(60 * 24 * time.Hour)
	set()
	e.st.Tick(context.Background())
	if got := warns(); got != 5 {
		t.Fatalf("la reconnexion ne doit pas alerter : %d", got)
	}
	e.now = exp.Add(-14 * 24 * time.Hour)
	e.st.Tick(context.Background())
	if got := warns(); got != 6 {
		t.Fatalf("seuil J-14 non réarmé : %d", got)
	}
}

func TestLinkedInStateOf(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		tok  *liToken
		want string
	}{
		{nil, "disconnected"},
		{&liToken{AccessToken: "x", ExpiresAt: now.Add(30 * 24 * time.Hour)}, "connected"},
		{&liToken{AccessToken: "x", ExpiresAt: now.Add(10 * 24 * time.Hour)}, "connected"},
		{&liToken{AccessToken: "x", ExpiresAt: now.Add(10*24*time.Hour - time.Second)}, "expiring"},
		{&liToken{AccessToken: "x", ExpiresAt: now.Add(time.Hour)}, "expiring"},
		{&liToken{AccessToken: "x", ExpiresAt: now}, "expired"},
	}
	for i, c := range cases {
		if got := liStateOf(c.tok, now); got.State != c.want {
			t.Errorf("cas %d : %s, attendu %s", i, got.State, c.want)
		}
	}
	if s := liStateOf(cases[1].tok, now); s.DaysLeft == nil || *s.DaysLeft != 30 {
		t.Errorf("jours restants : %v", s.DaysLeft)
	}
}

// ---------------------------------------------------------------- B10

func TestLinkedInSchedulingParity(t *testing.T) {
	e, _ := liAPI(t, true)
	// sans date : prochain créneau du compte (09:00 le jour même, puis 15:00)
	a := e.liDraft(t, "premier")
	pa, err := e.st.Validate(a.ID, "allan", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := pa.ScheduledAt.Format("2006-01-02 15:04"); got != "2026-11-18 09:00" {
		t.Errorf("premier créneau : %s", got)
	}
	b := e.liDraft(t, "second")
	pb, _ := e.st.Validate(b.ID, "allan", nil, false)
	if got := pb.ScheduledAt.Format("15:04"); got != "15:00" {
		t.Errorf("second créneau : %s", got)
	}
	// pas de limite journalière par défaut sur LinkedIn
	if acc := e.st.store.Snapshot().Account("pro"); acc.DailyLimit != 0 {
		t.Errorf("DailyLimit = %d, attendu 0", acc.DailyLimit)
	}
	// glisser-déposer : premier créneau libre du jour visé, comme pour X
	moved, err := e.st.MoveToDay(pb.ID, "2026-11-20")
	if err != nil {
		t.Fatal(err)
	}
	if got := moved.ScheduledAt.Format("2006-01-02 15:04"); got != "2026-11-20 09:00" {
		t.Errorf("après déplacement : %s", got)
	}
	// retour en brouillon : la validation est annulée
	if u, err := e.st.Unschedule(pa.ID); err != nil || u.Status != StatusDraft || u.ValidatedBy != "" {
		t.Errorf("unschedule : %v %+v", err, u)
	}
}

func TestLinkedInSchedulingFollowsTimingWindow(t *testing.T) {
	e, _ := liAPI(t, true)
	a := e.st.store.Snapshot().Account("pro")
	zero := 0
	a.Timing = Timing{Mode: "window", Start: "10:00", End: "12:00", Jitter: &zero}
	if err := e.st.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	p := e.liDraft(t, "dans la fenêtre")
	got, err := e.st.Validate(p.ID, "allan", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := got.ScheduledAt.UTC().Hour()
	if h < 10 || h > 12 {
		t.Fatalf("heure %v hors de la fenêtre 10:00-12:00", got.ScheduledAt)
	}
}

func TestLinkedInIdentityMismatchCancelsSend(t *testing.T) {
	e, f := liAPI(t, true)
	f.sub = "autre-membre"
	p := e.liValidated(t, "mauvais membre", e.now.Add(-time.Minute))
	e.st.PublishDue(context.Background())
	got := e.post(t, p.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "autre membre") {
		t.Fatalf("statut %s, erreur %q", got.Status, got.Error)
	}
	if f.count("/rest/posts") != 0 {
		t.Fatal("un post est parti sous une autre identité")
	}
}

func TestLinkedInDryrunAndManualUnchanged(t *testing.T) {
	// le backend linkedin n'est choisi que pour backend == linkedin
	e := newEnv(t)
	e.st.backendFor = nil
	e.st = NewStudio(e.st.store)
	e.st.now = func() time.Time { return e.now }
	_ = e.st.SaveAccount(&Account{Name: "man", Platform: "linkedin", Line: "kinflux", Backend: "manual", Timezone: "UTC", Slots: []string{"09:00"}})
	be, err := e.st.backendFor(e.st.store.Snapshot().Account("man"))
	if err != nil || be.Name() != "manual" {
		t.Fatalf("%v %v", be, err)
	}
}

// ---------------------------------------------------------------- OAuth

func liServer(t *testing.T, e *env) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(buildHandler(e.st, ServeOptions{NoAuth: true}))
	t.Cleanup(srv.Close)
	return srv
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func getJSON(t *testing.T, c *http.Client, url string) (int, map[string]any) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &out)
	return resp.StatusCode, out
}

// B1
func TestLinkedInConnectNotConfigured(t *testing.T) {
	e, _ := liAPI(t, false)
	e.st.liCfg = func() liConfig { var c liConfig; c.defaults(); return c }
	srv := liServer(t, e)
	code, out := getJSON(t, noRedirect, srv.URL+"/api/linkedin/connect?account=pro")
	if code != 400 {
		t.Fatalf("code %d", code)
	}
	msg, _ := out["error"].(map[string]any)["message"].(string)
	if !strings.Contains(msg, "app LinkedIn non configurée") {
		t.Fatalf("message %q", msg)
	}
}

func connectState(t *testing.T, srv *httptest.Server, account string) (authURL *url.URL, state string) {
	t.Helper()
	code, out := getJSON(t, noRedirect, srv.URL+"/api/linkedin/connect?account="+account)
	if code != 200 {
		t.Fatalf("connect : %d %v", code, out)
	}
	raw := out["data"].(map[string]any)["url"].(string)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u, u.Query().Get("state")
}

func TestLinkedInConnectURL(t *testing.T) {
	e, f := liAPI(t, false)
	srv := liServer(t, e)
	u, state := connectState(t, srv, "pro")
	q := u.Query()
	if !strings.HasPrefix(u.String(), f.srv.URL+"/oauth/v2/authorization?") || q.Get("response_type") != "code" ||
		q.Get("client_id") != "fake-client" || q.Get("redirect_uri") != f.cfg().RedirectURI ||
		q.Get("scope") != "openid profile w_member_social" || len(state) < 32 {
		t.Fatalf("url : %s", u)
	}
	if strings.Contains(u.String(), fakeSecret) {
		t.Fatal("le secret ne doit pas figurer dans l'URL")
	}
	if _, s2 := connectState(t, srv, "pro"); s2 == state {
		t.Fatal("state non aléatoire")
	}
	// un compte X ou inconnu n'a pas de connexion LinkedIn
	for _, acc := range []string{"main", "nope"} {
		if code, _ := getJSON(t, noRedirect, srv.URL+"/api/linkedin/connect?account="+acc); code != 400 {
			t.Errorf("%s : code %d", acc, code)
		}
	}
}

// B2
func TestLinkedInCallbackRejectsBadState(t *testing.T) {
	e, f := liAPI(t, false)
	srv := liServer(t, e)
	cb := func(state string) int {
		resp, err := noRedirect.Get(srv.URL + "/auth/linkedin/callback?code=abc&state=" + url.QueryEscape(state))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := cb("inconnu"); c != 401 {
		t.Errorf("state inconnu : %d", c)
	}
	if c := cb(""); c != 401 {
		t.Errorf("state vide : %d", c)
	}
	// expiré
	_, st := connectState(t, srv, "pro")
	e.now = e.now.Add(11 * time.Minute)
	if c := cb(st); c != 401 {
		t.Errorf("state expiré : %d", c)
	}
	// déjà utilisé
	_, st = connectState(t, srv, "pro")
	if c := cb(st); c != http.StatusFound {
		t.Fatalf("premier usage : %d", c)
	}
	f.mu.Lock()
	f.reqs = nil
	f.mu.Unlock()
	if c := cb(st); c != 401 {
		t.Errorf("state réutilisé : %d", c)
	}
	if f.count("/oauth/v2/accessToken") != 0 || f.total() != 0 {
		t.Fatal("un état invalide ne doit déclencher aucun échange")
	}
}

// B3 + flux nominal
func TestLinkedInOAuthFlow(t *testing.T) {
	e, f := liAPI(t, false)
	srv := liServer(t, e)
	_, state := connectState(t, srv, "pro")
	resp, err := noRedirect.Get(srv.URL + "/auth/linkedin/callback?code=CODE1&state=" + state)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/?linkedin=connected" {
		t.Fatalf("réponse : %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	// l'échange : formulaire, code et secret dans le corps (jamais dans l'URL)
	var tokReq liReq
	for _, r := range f.reqs {
		if strings.HasPrefix(r.Path, "/oauth/v2/accessToken") {
			tokReq = r
		}
	}
	form, _ := url.ParseQuery(string(tokReq.Body))
	if tokReq.Method != "POST" || tokReq.Header.Get("Content-Type") != "application/x-www-form-urlencoded" ||
		form.Get("grant_type") != "authorization_code" || form.Get("code") != "CODE1" ||
		form.Get("client_secret") != fakeSecret || strings.Contains(tokReq.Path, "CODE1") {
		t.Fatalf("échange : %s %s", tokReq.Path, tokReq.Body)
	}
	if strings.Contains(resp.Header.Get("Location"), fakeToken) {
		t.Fatal("jeton dans la redirection")
	}
	// linkedin.json : 0600, contient le jeton ; data.json, non
	path := e.st.liTokens().path()
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("linkedin.json : %v %v", st, err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), fakeToken) {
		t.Fatal("jeton non enregistré")
	}
	data, _ := os.ReadFile(e.st.store.path)
	noLeak(t, "data.json", string(data))
	// compte relié
	acc := e.st.store.Snapshot().Account("pro")
	if acc.AuthorURN != "urn:li:person:"+fakeSub || acc.Backend != "linkedin" {
		t.Fatalf("compte : %+v", acc)
	}
	// /api/state : état de connexion, jamais le jeton
	apiResp, _ := http.Get(srv.URL + "/api/state")
	body, _ := io.ReadAll(apiResp.Body)
	apiResp.Body.Close()
	noLeak(t, "/api/state", string(body))
	var state2 struct {
		Data struct {
			Linkedin struct {
				Configured bool                     `json:"configured"`
				Accounts   map[string]LinkedInState `json:"accounts"`
			} `json:"linkedin"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &state2)
	if s := state2.Data.Linkedin.Accounts["pro"]; s.State != "connected" || s.PersonName != "Allan Test" || s.DaysLeft == nil || *s.DaysLeft != 60 || !state2.Data.Linkedin.Configured {
		t.Fatalf("état : %+v", state2.Data.Linkedin)
	}
	// journaux : rien de secret
	for _, l := range e.st.store.Snapshot().Log {
		noLeak(t, "journal", l.Message)
	}
	// reconnexion : remplace le jeton, ne touche pas aux posts
	p := e.liValidated(t, "reste programmé", e.now.Add(48*time.Hour))
	_, st2 := connectState(t, srv, "pro")
	resp2, _ := noRedirect.Get(srv.URL + "/auth/linkedin/callback?code=CODE2&state=" + st2)
	resp2.Body.Close()
	if got := e.post(t, p.ID); got.Status != StatusScheduled || got.ValidatedBy != "allan" {
		t.Fatalf("la reconnexion a modifié le post : %+v", got)
	}
}

func TestLinkedInCallbackCancelledOrRefused(t *testing.T) {
	e, f := liAPI(t, false)
	srv := liServer(t, e)
	for q, want := range map[string]string{
		"error=user_cancelled_login":                   "/?linkedin=cancelled",
		"error=user_cancelled_authorize":               "/?linkedin=cancelled",
		"error=access_denied&error_description=secret": "/?linkedin=refused",
	} {
		_, st := connectState(t, srv, "pro")
		resp, _ := noRedirect.Get(srv.URL + "/auth/linkedin/callback?" + q + "&state=" + st)
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
			t.Errorf("%s : %d %s", q, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	if f.total() != 0 {
		t.Fatal("aucun appel LinkedIn attendu")
	}
	if e.st.liTokens().get("pro") != nil {
		t.Fatal("aucun jeton attendu")
	}
}

func TestLinkedInCallbackTokenRefusedByLinkedIn(t *testing.T) {
	e, f := liAPI(t, false)
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error_description":"détail brut ` + fakeSecret + `"}`))
	})
	srv := liServer(t, e)
	_, st := connectState(t, srv, "pro")
	resp, _ := noRedirect.Get(srv.URL + "/auth/linkedin/callback?code=X&state=" + st)
	resp.Body.Close()
	if resp.Header.Get("Location") != "/?linkedin=error" {
		t.Fatalf("Location %q", resp.Header.Get("Location"))
	}
	for _, l := range e.st.store.Snapshot().Log {
		noLeak(t, "journal", l.Message)
	}
	if e.st.liTokens().get("pro") != nil {
		t.Fatal("aucun jeton attendu")
	}
}

func TestLinkedInCallbackRefusesOtherMember(t *testing.T) {
	e, f := liAPI(t, true) // le compte est déjà lié à fakeSub
	f.sub = "un-autre"
	srv := liServer(t, e)
	_, st := connectState(t, srv, "pro")
	resp, _ := noRedirect.Get(srv.URL + "/auth/linkedin/callback?code=X&state=" + st)
	resp.Body.Close()
	if resp.Header.Get("Location") != "/?linkedin=identity" {
		t.Fatalf("Location %q", resp.Header.Get("Location"))
	}
	if tok := e.st.liTokens().get("pro"); tok == nil || tok.PersonID != fakeSub {
		t.Fatal("le jeton existant ne doit pas être remplacé par celui d'un autre membre")
	}
}

// Le cookie de session (SameSite=Strict) n'accompagne pas la redirection de
// LinkedIn : une page intermédiaire refait la navigation depuis le studio.
func TestLinkedInCallbackNeedsSession(t *testing.T) {
	e, f := liAPI(t, false)
	srv := httptest.NewServer(buildHandler(e.st, ServeOptions{})) // authentification active
	defer srv.Close()
	resp, _ := noRedirect.Get(srv.URL + "/auth/linkedin/callback?code=C&state=S")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), `http-equiv="refresh"`) || !strings.Contains(string(b), "hop=1") {
		t.Fatalf("page intermédiaire attendue : %d %s", resp.StatusCode, b)
	}
	resp, _ = noRedirect.Get(srv.URL + "/auth/linkedin/callback?code=C&state=S&hop=1")
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("sans session : %d", resp.StatusCode)
	}
	if r, _ := noRedirect.Get(srv.URL + "/api/linkedin/connect?account=pro"); r.StatusCode != 401 {
		t.Errorf("connect sans session : %d", r.StatusCode)
	}
	if f.total() != 0 {
		t.Fatal("aucun échange sans session")
	}
}

// ---------------------------------------------------------------- jetons et fuites

func TestLinkedInTokenFileAtomic0600(t *testing.T) {
	dir := t.TempDir()
	tk := liTokens{dir: dir}
	for i := 0; i < 3; i++ {
		if err := tk.update(func(f *liFile) { f.Accounts["a"] = &liToken{AccessToken: fakeToken, AlertLevel: i} }); err != nil {
			t.Fatal(err)
		}
	}
	st, err := os.Stat(tk.path())
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", st, err)
	}
	ents, _ := os.ReadDir(dir)
	for _, en := range ents {
		if strings.HasSuffix(en.Name(), ".tmp") {
			t.Errorf("fichier temporaire oublié : %s", en.Name())
		}
	}
	if got := tk.get("A"); got == nil || got.AlertLevel != 2 {
		t.Errorf("lecture : %+v", got)
	}
}

func TestLinkedInNoTokenInOutputs(t *testing.T) {
	e, f := liAPI(t, true)
	f.postStatus = http.StatusForbidden // le message simulé contient le jeton
	p := e.liValidated(t, "403", e.now.Add(-time.Minute))
	res := e.st.PublishDue(context.Background())
	got := e.post(t, p.ID)
	if got.Status != StatusFailed {
		t.Fatalf("statut %s", got.Status)
	}
	srv := liServer(t, e)
	resp, _ := http.Get(srv.URL + "/api/state")
	st, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	chk, _ := e.st.CheckAccount(context.Background(), "pro")
	for what, v := range map[string]any{"résultat": res, "post": got, "contrôle": chk, "journal": e.st.store.Snapshot().Log, "état": linkedinStatesFor(e)} {
		b, _ := json.Marshal(v)
		noLeak(t, what, string(b))
	}
	noLeak(t, "/api/state", string(st))
}

func linkedinStatesFor(e *env) any { return e.st.linkedinStates(e.st.store.Snapshot()) }

func TestLinkedInCheckAccount(t *testing.T) {
	e, _ := liAPI(t, true)
	r, err := e.st.CheckAccount(context.Background(), "pro")
	if err != nil || !r.OK || r.Profile == nil || r.Profile.ID != fakeSub {
		t.Fatalf("%v %+v", err, r)
	}
	e.liConnect(t, -time.Hour)
	if r, _ := e.st.CheckAccount(context.Background(), "pro"); r.OK || !strings.Contains(r.Problem, "expiré") {
		t.Fatalf("jeton expiré : %+v", r)
	}
}

// Ancien comportement conservé : AuthorURN ne se règle pas depuis un formulaire.
func TestLinkedInAuthorURNKeptOnSave(t *testing.T) {
	e, _ := liAPI(t, true)
	a := e.st.store.Snapshot().Account("pro")
	a.AuthorURN = "urn:li:person:pirate"
	if err := e.st.SaveAccount(a); err != nil {
		t.Fatal(err)
	}
	if got := e.st.store.Snapshot().Account("pro").AuthorURN; got != "urn:li:person:"+fakeSub {
		t.Fatalf("AuthorURN = %q", got)
	}
	n := &Account{Name: "neuf", Platform: "linkedin", Line: "kinflux", Backend: "linkedin", AuthorURN: "urn:li:person:pirate"}
	if err := e.st.SaveAccount(n); err != nil {
		t.Fatal(err)
	}
	if got := e.st.store.Snapshot().Account("neuf").AuthorURN; got != "" {
		t.Fatalf("AuthorURN d'un nouveau compte = %q", got)
	}
}
