package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// scheduled : un post validé dont l'heure est déjà passée.
func (e *env) due(t *testing.T, text string) *Post {
	t.Helper()
	at := e.now.Add(-time.Minute)
	p, err := e.st.Validate(e.draft(t, text).ID, "allan", &at, false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Un compte en pause n'est pas publié, la pause survit au redémarrage, la
// reprise publie.
func TestPausedAccountIsNotPublished(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.due(t, "Post fictif d'un compte en pause")
	if _, err := e.st.SetPaused("main", true); err != nil {
		t.Fatal(err)
	}
	if r := e.st.PublishDue(ctx); len(r) != 0 || len(e.x.posted) != 0 {
		t.Fatalf("un compte en pause a été publié : %v %d", r, len(e.x.posted))
	}
	// redémarrage : même dossier de données, nouveau Store et nouveau Studio
	store2, err := OpenStore(e.st.store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	st2 := NewStudio(store2)
	st2.backendFor = func(*Account) (XBackend, error) { return e.x, nil }
	st2.now = func() time.Time { return e.now }
	if !store2.Snapshot().Account("main").Paused {
		t.Fatal("la pause n'a pas survécu au redémarrage")
	}
	if st2.PublishDue(ctx); len(e.x.posted) != 0 {
		t.Fatal("publié après redémarrage alors que le compte est en pause")
	}
	if got := store2.Snapshot().Post(p.ID); got.Status != StatusValidated && got.Status != StatusScheduled {
		t.Fatalf("le post en pause a changé de statut : %s", got.Status)
	}
	// l'enregistrement du formulaire du compte ne lève pas la pause
	acc := *store2.Snapshot().Account("main")
	acc.Paused = false
	if err := st2.SaveAccount(&acc); err != nil {
		t.Fatal(err)
	}
	if !store2.Snapshot().Account("main").Paused {
		t.Fatal("SaveAccount a levé la pause")
	}
	if _, err := st2.SetPaused("main", false); err != nil {
		t.Fatal(err)
	}
	st2.PublishDue(ctx)
	if len(e.x.posted) != 1 || store2.Snapshot().Post(p.ID).Status != StatusPublished {
		t.Fatal("la reprise doit publier le post en retard")
	}
}

func TestPausePostLateButNotAnAlert(t *testing.T) {
	e := newEnv(t)
	e.due(t, "Retard voulu")
	_, _ = e.st.SetPaused("main", true)
	e.now = e.now.Add(time.Hour)
	r := e.st.ControlReport(e.st.store.Snapshot())
	if len(r.Late) != 1 || !r.Late[0].Paused {
		t.Fatalf("retard attendu, marqué en pause : %+v", r.Late)
	}
	// seules les boucles jamais passées comptent ici (2) : le retard en pause non
	if r.Attention != 2 {
		t.Fatalf("attention = %d, attendu 2 (boucles jamais passées)", r.Attention)
	}
}

// Un envoi au résultat incertain n'est jamais relancé automatiquement.
func TestUncertainSendIsNeverRetried(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.due(t, "Post fictif au résultat incertain")
	calls := 0
	e.st.backendFor = func(*Account) (XBackend, error) { return &uncertainX{fakeX: e.x, calls: &calls}, nil }
	r := e.st.PublishDue(ctx)
	got := e.st.store.Snapshot().Post(p.ID)
	if got.Status != StatusUncertain || len(r) != 1 || r[0].Status != StatusUncertain {
		t.Fatalf("statut %s, résultat %+v", got.Status, r)
	}
	// plus tard, des dizaines de passes, même après le délai d'envoi bloqué
	for i := 0; i < 5; i++ {
		e.now = e.now.Add(time.Hour)
		e.st.PublishDue(ctx)
	}
	if calls != 1 {
		t.Fatalf("%d envois, attendu 1 (aucune relance automatique)", calls)
	}
	// un délai dépassé (contexte) compte aussi comme incertain
	e.x.fail = context.DeadlineExceeded
	e.st.backendFor = func(*Account) (XBackend, error) { return e.x, nil }
	q := e.due(t, "Autre post fictif, délai dépassé")
	e.st.PublishDue(ctx)
	if s := e.st.store.Snapshot().Post(q.ID).Status; s != StatusUncertain {
		t.Fatalf("délai dépassé : statut %s", s)
	}
	// un refus net reste un échec, pas un doute
	e.x.fail = errors.New("refus simulé")
	f := e.due(t, "Troisième post fictif, refusé")
	e.st.PublishDue(ctx)
	if s := e.st.store.Snapshot().Post(f.ID).Status; s != StatusFailed {
		t.Fatalf("refus net : statut %s", s)
	}
}

type uncertainX struct {
	*fakeX
	calls *int
}

func (u *uncertainX) Post(context.Context, PostRequest) (string, error) {
	*u.calls++
	return "", markUncertain(errors.New("connexion coupée après l'envoi"))
}

// Résolution : lien du post publié, ou « pas parti » confirmé ; jamais sans l'un des deux.
func TestResolveUncertainNeedsVerification(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.x.fail = context.DeadlineExceeded
	a := e.due(t, "Premier post fictif à vérifier")
	b := e.due(t, "Second post fictif à vérifier")
	e.st.PublishDue(ctx)
	e.x.fail = nil
	for _, c := range []struct {
		url     string
		notSent bool
	}{{"", false}, {"https://x.com/moi/status/1234567890", true}, {"javascript:alert(1)", false}, {"https://example.com/moi/status/1234567890", false}, {"https://x.com/moi", false}} {
		if _, err := e.st.ResolveUncertain(a.ID, c.url, c.notSent); err == nil {
			t.Fatalf("résolution acceptée sans vérification valable : %+v", c)
		}
	}
	if s := e.st.store.Snapshot().Post(a.ID).Status; s != StatusUncertain {
		t.Fatalf("un refus a changé le statut : %s", s)
	}
	// ni l'édition ni la validation ne remettent le post en route
	if _, err := e.st.Validate(a.ID, "allan", nil, true); err == nil {
		t.Fatal("un envoi incertain a été revalidé sans vérification")
	}
	got, err := e.st.ResolveUncertain(a.ID, "https://x.com/moi/status/1234567890", false)
	if err != nil || got.Status != StatusPublished || got.TweetID != "1234567890" || got.PublishedAt == nil {
		t.Fatalf("%+v %v", got, err)
	}
	if len(e.x.posted) != 0 {
		t.Fatal("la résolution par lien ne doit rien envoyer")
	}
	// pas parti : relancé à la passe suivante
	if _, err := e.st.ResolveUncertain(b.ID, "", true); err != nil {
		t.Fatal(err)
	}
	e.st.PublishDue(ctx)
	if len(e.x.posted) != 1 || e.st.store.Snapshot().Post(b.ID).Status != StatusPublished {
		t.Fatal("le post vérifié non parti doit repartir")
	}
	if _, err := e.st.ResolveUncertain(b.ID, "", true); err == nil {
		t.Fatal("un post publié ne se résout pas")
	}
}

// Un envoi resté « publishing » (arrêt du processus) passe en incertain, jamais renvoyé.
func TestStuckPublishingBecomesUncertain(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.due(t, "Post fictif interrompu")
	_ = e.st.store.Update(func(d *Data) error {
		x := d.Post(p.ID)
		x.Status, x.UpdatedAt = StatusPublishing, e.now.Add(-time.Hour)
		return nil
	})
	e.st.PublishDue(ctx)
	if s := e.st.store.Snapshot().Post(p.ID).Status; s != StatusUncertain || len(e.x.posted) != 0 {
		t.Fatalf("statut %s, %d envoi(s)", s, len(e.x.posted))
	}
	// un envoi en cours récent n'est pas touché
	q := e.due(t, "Post fictif en cours")
	_ = e.st.store.Update(func(d *Data) error {
		x := d.Post(q.ID)
		x.Status, x.UpdatedAt = StatusPublishing, e.now
		return nil
	})
	e.st.PublishDue(ctx)
	if s := e.st.store.Snapshot().Post(q.ID).Status; s != StatusPublishing {
		t.Fatalf("un envoi récent a été touché : %s", s)
	}
}

// Backend xapi réel, transport simulé : coupure après l'envoi = incertain,
// refus HTTP et connexion impossible = échec net.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestXAPIPostErrorClassification(t *testing.T) {
	post := func(rt rtFunc) error {
		b := &xapiBackend{token: "jeton-factice", hc: &http.Client{Transport: rt}}
		_, err := b.Post(context.Background(), PostRequest{Text: "x"})
		return err
	}
	if err := post(func(*http.Request) (*http.Response, error) { return nil, errors.New("EOF") }); !isUncertain(err) {
		t.Fatalf("coupure après l'envoi : %v", err)
	}
	if err := post(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded }); !isUncertain(err) {
		t.Fatalf("délai dépassé : %v", err)
	}
	if err := post(func(*http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Err: errors.New("connexion refusée")}
	}); err == nil || isUncertain(err) {
		t.Fatalf("connexion impossible : %v", err)
	}
	if err := post(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: http.NoBody, Header: http.Header{}}, nil
	}); err == nil || isUncertain(err) || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("refus HTTP : %v", err)
	}
}

// Détection des retards, des passages trop anciens et des connexions.
func TestControlReportLateLoopsAndConnections(t *testing.T) {
	e := newEnv(t)
	_ = e.st.store.Update(func(d *Data) error { d.Settings.MinGapMinutes = 1; d.Account("main").DailyLimit = 10; return nil })
	// planifié dans le futur : pas en retard ; à 2 min : toléré ; à 30 min : en retard
	at := func(text string, d time.Duration) *Post {
		p, err := e.st.Validate(e.draft(t, text).ID, "allan", nil, true)
		if err != nil {
			t.Fatal(err)
		}
		when := e.now.Add(d)
		_ = e.st.store.Update(func(x *Data) error { x.Post(p.ID).ScheduledAt = &when; return nil })
		return p
	}
	f, n, o := at("Post fictif futur", time.Hour), at("Post fictif tout juste échu", -2*time.Minute), at("Post fictif en retard", -30*time.Minute)
	r := e.st.ControlReport(e.st.store.Snapshot())
	if len(r.Late) != 1 || r.Late[0].ID != o.ID || r.Late[0].LateMin != 30 {
		t.Fatalf("retards : %+v (futur %s, échu %s)", r.Late, f.ID, n.ID)
	}
	if r.OK {
		t.Fatal("tout ne tourne pas : OK ne doit pas être vrai")
	}
	// jamais de passage : alerte ; un passage récent : calme ; ancien : alerte
	for _, l := range r.Loops {
		if !l.Stale || l.LastAt != nil {
			t.Fatalf("boucle sans passage : %+v", l)
		}
	}
	e.st.every = time.Minute
	e.st.recordLoop(loopPublish)
	e.st.recordLoop(loopWatch)
	for _, l := range e.st.ControlReport(e.st.store.Snapshot()).Loops {
		if l.Stale || l.LastAt == nil {
			t.Fatalf("passage récent jugé ancien : %+v", l)
		}
	}
	e.now = e.now.Add(6 * time.Minute)
	for _, l := range e.st.ControlReport(e.st.store.Snapshot()).Loops {
		if !l.Stale {
			t.Fatalf("passage de 6 minutes non signalé : %+v", l)
		}
	}
	// l'état des passages est sur disque : il survit au redémarrage
	if _, err := os.Stat(e.st.loopsPath()); err != nil {
		t.Fatal(err)
	}
	// connexions : xapi sans jeton = à renouveler ; agentx = non suivi
	t.Setenv("X_USER_TOKEN", "")
	if err := e.st.SaveAccount(&Account{Name: "api", Handle: "@api", Backend: "xapi", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveAccount(&Account{Name: "cli", Handle: "@cli", Backend: "agentx", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	r = e.st.ControlReport(e.st.store.Snapshot())
	if len(r.Connections) != 1 || r.Connections[0].Account != "api" || r.Connections[0].State != "missing" {
		t.Fatalf("connexions : %+v", r.Connections)
	}
	if len(r.Unmonitored) != 1 || r.Unmonitored[0] != "cli" {
		t.Fatalf("non suivis : %+v", r.Unmonitored)
	}
}

func TestControlReportListsUncertain(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.x.fail = context.DeadlineExceeded
	p := e.due(t, "Post fictif incertain")
	e.st.PublishDue(ctx)
	r := e.st.ControlReport(e.st.store.Snapshot())
	if len(r.Uncertain) != 1 || r.Uncertain[0].ID != p.ID || len(r.Late) != 0 {
		t.Fatalf("%+v", r)
	}
}
