package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// replyX : un backend factice qui sait lire les réponses d'un post.
type replyX struct {
	*fakeX
	replies map[string][]Tweet // par identifiant de post
	calls   int
	asked   []int // taille demandée à chaque appel
}

func (r *replyX) Replies(_ context.Context, id string, n int) ([]Tweet, error) {
	r.calls++
	r.asked = append(r.asked, n)
	return r.replies[id], nil
}

func inboxEnv(t *testing.T, in InboxSettings) (*env, *replyX, *Post) {
	t.Helper()
	e := newEnv(t)
	x := &replyX{fakeX: e.x, replies: map[string][]Tweet{}}
	e.st.backendFor = func(*Account) (XBackend, error) { return x, nil }
	// le compte publie un post (action explicite : validation signée)
	p := e.due(t, "Mon post sur les agents")
	if res := e.st.PublishDue(context.Background()); len(res) != 1 || res[0].Status != StatusPublished {
		t.Fatalf("publication : %+v", res)
	}
	if _, err := e.st.SetInboxSettings("main", in); err != nil {
		t.Fatal(err)
	}
	p = e.st.store.Snapshot().Post(p.ID)
	return e, x, p
}

func manyReplies(from, n int) []Tweet {
	var out []Tweet
	for i := from; i < from+n; i++ {
		out = append(out, tw(fmt.Sprint(5000+i), fmt.Sprintf("user%d", i), fmt.Sprintf("Réponse numéro %d", i)))
	}
	return out
}

// Inactive par défaut : aucune lecture tant que l'utilisateur ne l'active pas.
func TestInboxOffByDefault(t *testing.T) {
	e, x, _ := inboxEnv(t, InboxSettings{})
	x.replies["1001"] = manyReplies(0, 3)
	if r := e.st.SyncInbox(context.Background(), "", false); len(r) != 0 || x.calls != 0 {
		t.Fatalf("lecture sans activation : %v, %d appels", r, x.calls)
	}
}

// Pas de doublon : deux passages, une réponse déjà ignorée qui revient, les
// propres réponses du compte et le post lui-même sont écartés.
func TestInboxNoDuplicate(t *testing.T) {
	ctx := context.Background()
	e, x, p := inboxEnv(t, InboxSettings{Enabled: true})
	own := tw("6000", "moi", "Ma propre réponse")
	root := tw(p.TweetID, "moi", p.Text)
	x.replies[p.TweetID] = append([]Tweet{root, own}, manyReplies(0, 3)...)
	if r := e.st.SyncInbox(ctx, "", false); len(r) != 1 || r[0].New != 3 {
		t.Fatalf("premier passage : %+v", r)
	}
	// l'une est ignorée, puis le même lot (plus une nouvelle) revient
	d := e.st.store.Snapshot()
	if _, err := e.st.SetInboxStatus(d.Inbox[0].ID, InboxIgnored); err != nil {
		t.Fatal(err)
	}
	x.replies[p.TweetID] = append(x.replies[p.TweetID], manyReplies(10, 1)...)
	e.now = e.now.Add(2 * time.Hour)
	if r := e.st.SyncInbox(ctx, "", false); len(r) != 1 || r[0].New != 1 {
		t.Fatalf("second passage : %+v", r)
	}
	// forcer ne duplique pas non plus
	e.st.SyncInbox(ctx, "", true)
	d = e.st.store.Snapshot()
	if len(d.Inbox) != 4 {
		t.Fatalf("%d entrées, 4 attendues", len(d.Inbox))
	}
	seen := map[string]bool{}
	for _, in := range d.Inbox {
		if seen[in.ReplyID] {
			t.Fatalf("doublon de %s", in.ReplyID)
		}
		seen[in.ReplyID] = true
		if in.PostText != p.Text || in.PostTweetID != p.TweetID {
			t.Fatalf("contexte du post d'origine absent : %+v", in)
		}
	}
}

// Le plafond de lectures par jour est respecté, y compris en forçant, et
// repart le lendemain.
func TestInboxDailyCap(t *testing.T) {
	ctx := context.Background()
	e, x, p := inboxEnv(t, InboxSettings{Enabled: true, DailyCap: 25})
	x.replies[p.TweetID] = manyReplies(0, 100) // le backend en renverrait beaucoup plus que le plafond
	total := 0
	for i := 0; i < 6; i++ {
		for _, r := range e.st.SyncInbox(ctx, "", true) {
			total += r.Read
		}
	}
	for _, n := range x.asked {
		if n > 25 {
			t.Fatalf("demande de %d lectures au-dessus du plafond de 25", n)
		}
	}
	if st := inboxStatus(e.st.store.Snapshot(), e.now); st[0].Read > 25 {
		t.Fatalf("lectures du jour : %d (plafond 25)", st[0].Read)
	}
	// 25 demandées au premier appel, 100 renvoyés (le backend ignore n) : le
	// compteur est atteint, plus aucun appel ensuite.
	calls := x.calls
	e.st.SyncInbox(ctx, "", true)
	if x.calls != calls {
		t.Fatal("appel après plafond atteint")
	}
	e.now = e.now.Add(24 * time.Hour)
	e.st.SyncInbox(ctx, "", true)
	if x.calls == calls {
		t.Fatal("le plafond ne repart pas le lendemain")
	}
	_ = total
}

// Un passage vide ou en échec coûte une lecture : il ne peut pas boucler.
func TestInboxEmptyCallsAreCharged(t *testing.T) {
	ctx := context.Background()
	e, x, _ := inboxEnv(t, InboxSettings{Enabled: true, DailyCap: 10})
	for i := 0; i < 5; i++ {
		e.st.SyncInbox(ctx, "", true)
	}
	if x.calls != 1 {
		t.Fatalf("%d appels sous un plafond de 10 après un appel vide (1 attendu, plafond minimal d'une page)", x.calls)
	}
}

// Fréquence réglable : pas de nouveau passage avant l'échéance, sauf demande manuelle.
func TestInboxFrequency(t *testing.T) {
	ctx := context.Background()
	e, x, _ := inboxEnv(t, InboxSettings{Enabled: true, EveryMinutes: 30})
	e.st.SyncInbox(ctx, "", false)
	e.now = e.now.Add(10 * time.Minute)
	e.st.SyncInbox(ctx, "", false)
	if x.calls != 1 {
		t.Fatalf("%d appels avant l'échéance", x.calls)
	}
	e.now = e.now.Add(25 * time.Minute)
	e.st.SyncInbox(ctx, "", false)
	if x.calls != 2 {
		t.Fatalf("%d appels après l'échéance", x.calls)
	}
	if _, err := e.st.SetInboxSettings("main", InboxSettings{Enabled: true, EveryMinutes: 5}); err == nil {
		t.Fatal("fréquence sous le minimum acceptée")
	}
}

// La récupération est dans la boucle IA, jamais dans celle de la publication.
func TestInboxRunsInAILoopOnly(t *testing.T) {
	ctx := context.Background()
	e, x, p := inboxEnv(t, InboxSettings{Enabled: true})
	x.replies[p.TweetID] = manyReplies(0, 2)
	e.st.TickPublish(ctx)
	if x.calls != 0 {
		t.Fatal("la boucle de publication a lu des réponses")
	}
	out := e.st.TickAI(ctx)
	if x.calls != 1 || out["inbox"] == nil {
		t.Fatalf("la boucle IA n'a pas lu : %d appels, %v", x.calls, out)
	}
}

// Aucune publication sans action explicite : récupérer et rédiger ne publient rien.
func TestInboxNeverPublishesByItself(t *testing.T) {
	ctx := context.Background()
	e, x, p := inboxEnv(t, InboxSettings{Enabled: true})
	x.replies[p.TweetID] = manyReplies(0, 2)
	posted := len(e.x.posted)
	e.st.TickAI(ctx)
	e.st.TickPublish(ctx)
	if len(e.x.posted) != posted {
		t.Fatal("un envoi est parti à la récupération")
	}
	// pas d'IA sans demande
	if e.ai.count() != 0 {
		t.Fatalf("%d appel(s) IA sans demande", e.ai.count())
	}
	entry := e.st.store.Snapshot().Inbox[0]
	dr, err := e.st.DraftInboxReply(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dr.Status != StatusDraft || dr.ReplyTo != entry.ReplyID || dr.ValidatedBy != "" || dr.Kind != "reply" {
		t.Fatalf("brouillon : %+v", dr)
	}
	// un second appel rend le même brouillon, sans nouvel appel IA
	calls := e.ai.count()
	if again, err := e.st.DraftInboxReply(ctx, entry.ID); err != nil || again.ID != dr.ID || e.ai.count() != calls {
		t.Fatalf("brouillon redemandé : %v %v", again, err)
	}
	e.now = e.now.Add(3 * time.Hour)
	e.st.TickPublish(ctx)
	e.st.TickAI(ctx)
	if len(e.x.posted) != posted {
		t.Fatal("le brouillon est parti sans validation")
	}
	// l'utilisateur valide : seulement alors la réponse part, puis l'entrée passe à « Répondu »
	at := e.now.Add(-time.Minute)
	if _, err := e.st.Validate(dr.ID, "allan", &at, false); err != nil {
		t.Fatal(err)
	}
	e.st.PublishDue(ctx)
	if len(e.x.posted) != posted+1 || e.x.posted[posted].ReplyTo != entry.ReplyID {
		t.Fatalf("envois : %+v", e.x.posted)
	}
	e.st.SyncInbox(ctx, "", true)
	if got := e.st.store.Snapshot().Inbox[0]; got.Status != InboxAnswered {
		t.Fatalf("statut : %s", got.Status)
	}
}

// Statuts à la main et statut inconnu refusé.
func TestInboxStatuses(t *testing.T) {
	e, x, p := inboxEnv(t, InboxSettings{Enabled: true})
	x.replies[p.TweetID] = manyReplies(0, 1)
	e.st.SyncInbox(context.Background(), "", true)
	id := e.st.store.Snapshot().Inbox[0].ID
	if got := e.st.store.Snapshot().Inbox[0].Status; got != InboxTodo {
		t.Fatalf("statut initial : %s", got)
	}
	for _, s := range []string{InboxIgnored, InboxAnswered, InboxTodo} {
		if r, err := e.st.SetInboxStatus(id, s); err != nil || r.Status != s {
			t.Fatalf("%s : %v %v", s, r, err)
		}
	}
	if _, err := e.st.SetInboxStatus(id, "supprimé"); err == nil {
		t.Fatal("statut inconnu accepté")
	}
}

// Un backend sans lecture des réponses (dryrun) donne une erreur claire, sans appel ni plantage.
func TestInboxUnsupportedBackend(t *testing.T) {
	e, _, _ := inboxEnv(t, InboxSettings{Enabled: true})
	e.st.backendFor = func(*Account) (XBackend, error) { return e.x, nil } // fakeX n'a pas Replies
	r := e.st.SyncInbox(context.Background(), "", true)
	if len(r) != 1 || r[0].Error == "" {
		t.Fatalf("%+v", r)
	}
}
