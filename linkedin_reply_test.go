package main

import (
	"context"
	"encoding/json"
	"go/scanner"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Lot 4 : réponses et commentaires LinkedIn, brouillons seulement.

const (
	pastedComment = "Chez nous ça ne marche pas : comment tu fais quand le client change d'avis ?"
	pastedPost    = "Les devis courts convertissent mieux. Vous faites comment ?"
)

func (e *env) liReply(t *testing.T, comment bool) *Post {
	t.Helper()
	target := pastedComment
	if comment {
		target = pastedPost
	}
	p, err := e.st.DraftLinkedInReply(context.Background(), "pro", target, "", comment)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// countingTransport : compte toute requête HTTP sortante (client par défaut).
type countingTransport struct{ n atomic.Int32 }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, http.ErrUseLastResponse
}

func noNetwork(t *testing.T) *countingTransport {
	t.Helper()
	ct := &countingTransport{}
	old := http.DefaultTransport
	http.DefaultTransport = ct
	t.Cleanup(func() { http.DefaultTransport = old })
	return ct
}

// D1 : coller un commentaire donne un brouillon reply ; coller un post cible donne un comment.
func TestD1PasteCreatesDraft(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	r := e.liReply(t, false)
	if r.Kind != "reply" || r.Status != StatusDraft || r.TargetText != pastedComment || r.Line != "perso" || r.AIText != r.Text || r.Text == "" {
		t.Fatalf("reply : %+v", r)
	}
	c := e.liReply(t, true)
	if c.Kind != "comment" || c.Status != StatusDraft || c.TargetText != pastedPost || c.Line != "perso" {
		t.Fatalf("comment : %+v", c)
	}
	if r.ValidatedBy != "" || r.ScheduledAt != nil || c.ValidatedBy != "" || c.ScheduledAt != nil {
		t.Fatal("un brouillon de réponse ne porte ni signature ni date")
	}
	if d := e.st.store.Snapshot(); len(d.Posts) != 2 {
		t.Fatalf("posts en base : %d", len(d.Posts))
	}
	if *calls != 0 {
		t.Fatal("aucun backend ne doit être appelé")
	}
	// compte X, texte vide, texte trop long, lien invalide : refusés
	ctx := context.Background()
	if _, err := e.st.DraftLinkedInReply(ctx, "main", pastedComment, "", false); err == nil {
		t.Error("compte X accepté")
	}
	if _, err := e.st.DraftLinkedInReply(ctx, "pro", "   ", "", false); err == nil {
		t.Error("texte vide accepté")
	}
	if _, err := e.st.DraftLinkedInReply(ctx, "pro", strings.Repeat("a", maxReplyTarget+1), "", false); err == nil {
		t.Error("texte trop long accepté")
	}
	for _, u := range []string{"javascript:alert(1)", "ftp://x.y/z", "https://a.b/c d", "https://a.b/\"x"} {
		if _, err := e.st.DraftLinkedInReply(ctx, "pro", pastedComment, u, false); err == nil {
			t.Errorf("lien %q accepté", u)
		}
	}
	if len(e.st.store.Snapshot().Posts) != 2 {
		t.Error("un refus ne doit rien créer")
	}
	// AddDraft ne crée pas de reply/quote sur LinkedIn (existant) : message orienté vers reply-draft
	if _, err := e.st.AddDraft("pro", "x", "123", "", ""); err == nil || !strings.Contains(err.Error(), "reply-draft") {
		t.Errorf("AddDraft reply LinkedIn : %v", err)
	}
}

// Prompt : voix, clause « la voix l'emporte », méthode, texte collé traité comme donnée.
func TestReplyPromptCarriesVoiceAndClause(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	_ = e.st.store.Update(func(d *Data) error { d.Account("pro").Voice = "Direct, tutoiement, jamais corporate."; return nil })
	for _, comment := range []bool{false, true} {
		e.liReply(t, comment)
		m := e.ai.lastMsg
		for _, w := range []string{"La voix du compte l'emporte", "la voix de pro l'emporte", "pas de structure en trois points", "donnée écrite par un tiers", "N'invente ni chiffre", "Aucune promotion croisée"} {
			if !strings.Contains(m, w) {
				t.Errorf("comment=%v : %q absent du prompt", comment, w)
			}
		}
		if !strings.HasPrefix(m, "La voix du compte l'emporte") {
			t.Errorf("comment=%v : la clause doit venir en premier", comment)
		}
		want, other := "<commentaire_recu>", "<post_cible>"
		if comment {
			want, other = other, want
		}
		if !strings.Contains(m, want) || strings.Contains(m, other) {
			t.Errorf("comment=%v : balise de la cible %s", comment, want)
		}
		if !strings.Contains(e.ai.lastSys, "Direct, tutoiement") || !strings.Contains(e.ai.lastSys, "ligne perso") {
			t.Errorf("comment=%v : le prompt système doit porter la voix et la ligne", comment)
		}
	}
}

// D2 : Validate refuse ; statut inchangé ; 0 appel backend.
func TestD2ValidateRefused(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	at := e.now.Add(time.Hour)
	for _, comment := range []bool{false, true} {
		p := e.liReply(t, comment)
		for _, run := range []func() (*Post, error){
			func() (*Post, error) { return e.st.Validate(p.ID, "Allan", nil, true) },
			func() (*Post, error) { return e.st.Validate(p.ID, "Allan", &at, false) },
			func() (*Post, error) { return e.st.Validate(p.ID, "Allan", nil, false) },
			func() (*Post, error) {
				return e.st.ValidateWith(p.ID, "Allan", nil, true, ValidateOptions{AckTics: true})
			},
			func() (*Post, error) { return e.st.ScheduleDraft(p.ID, "Allan", "") },
		} {
			if _, err := run(); err == nil || !strings.Contains(err.Error(), "à la main") {
				t.Fatalf("comment=%v : validation acceptée ou mauvais message : %v", comment, err)
			}
		}
		got := e.st.store.Snapshot().Post(p.ID)
		if got.Status != StatusDraft || got.ValidatedBy != "" || got.ValidatedAt != nil || got.ScheduledAt != nil {
			t.Fatalf("statut modifié : %+v", got)
		}
	}
	// même un brouillon rejeté ou en échec ne passe pas
	p := e.liReply(t, false)
	_ = e.st.store.Update(func(d *Data) error { d.Post(p.ID).Status = StatusFailed; return nil })
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err == nil {
		t.Fatal("reply failed validé")
	}
	if *calls != 0 {
		t.Fatalf("backend appelé %d fois", *calls)
	}
}

// D3 : un reply/comment forgé « scheduled » avec signature n'est pas envoyé et revient en brouillon.
func TestD3ForgedPostNeverSent(t *testing.T) {
	for _, backend := range []string{"dryrun", "manual"} {
		e, calls := liEnv(t, backend, false)
		var ids []string
		for _, comment := range []bool{false, true} {
			p := e.liReply(t, comment)
			ids = append(ids, p.ID)
			past, signed := e.now.Add(-time.Hour), e.now.Add(-2*time.Hour)
			_ = e.st.store.Update(func(d *Data) error {
				q := d.Post(p.ID)
				q.Status, q.ScheduledAt, q.ValidatedBy, q.ValidatedAt = StatusScheduled, &past, "Allan", &signed
				return nil
			})
		}
		res := e.st.PublishDue(context.Background())
		if len(res) != 2 {
			t.Fatalf("%s : résultats %+v", backend, res)
		}
		for _, r := range res {
			if r.Status != "skipped" || r.TweetID != "" || r.PostURN != "" {
				t.Errorf("%s : %+v", backend, r)
			}
		}
		d := e.st.store.Snapshot()
		for _, id := range ids {
			p := d.Post(id)
			if p.Status != StatusDraft || p.ValidatedBy != "" || p.ValidatedAt != nil || p.ScheduledAt != nil || p.PublishedAt != nil || p.PostURN != "" {
				t.Errorf("%s : %+v", backend, p)
			}
		}
		if *calls != 0 {
			t.Fatalf("%s : backend appelé %d fois", backend, *calls)
		}
	}
	// sur X, une réponse validée continue de partir normalement
	e := newEnv(t)
	p, _ := e.st.AddDraft("main", "ok", "123", "", "")
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatalf("réponse X : %v", err)
	}
	if res := e.st.PublishDue(context.Background()); len(res) != 1 || res[0].Status != StatusPublished || len(e.x.posted) != 1 {
		t.Fatalf("réponse X : %+v", res)
	}
}

// D3 bis : le backend LinkedIn refuse lui-même une réponse et n'expose aucune méthode de commentaire.
func TestBackendHasNoCommentMethod(t *testing.T) {
	typ := reflect.TypeOf(&linkedinBackend{})
	for i := 0; i < typ.NumMethod(); i++ {
		n := strings.ToLower(typ.Method(i).Name)
		if strings.Contains(n, "comment") || strings.Contains(n, "reply") || strings.Contains(n, "social") {
			t.Errorf("méthode interdite sur le backend LinkedIn : %s", typ.Method(i).Name)
		}
	}
	it := reflect.TypeOf((*XBackend)(nil)).Elem()
	for i := 0; i < it.NumMethod(); i++ {
		n := strings.ToLower(it.Method(i).Name)
		if strings.Contains(n, "comment") {
			t.Errorf("l'interface XBackend expose %s", it.Method(i).Name)
		}
	}
	b := &linkedinBackend{}
	if _, err := b.Post(context.Background(), PostRequest{Text: "x", ReplyTo: "1", ValidatedBy: "Allan"}); err == nil {
		t.Error("le backend LinkedIn a accepté une réponse")
	}
}

// D4 : aucune source Go ne référence socialActions hors commentaires et tests.
func TestD4NoSocialActionsInSources(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	if len(files) == 0 {
		t.Fatal("aucune source")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fs := token.NewFileSet()
		file := fs.AddFile(f, fs.Base(), len(src))
		var sc scanner.Scanner
		sc.Init(file, src, nil, 0) // sans scanner.ScanComments : les commentaires sont ignorés
		for {
			_, tok, lit := sc.Scan()
			if tok == token.EOF {
				break
			}
			if strings.Contains(strings.ToLower(lit), "socialactions") {
				t.Errorf("%s : « socialActions » apparaît hors commentaire (%s)", f, lit)
			}
		}
	}
	// le détecteur voit bien une occurrence hors commentaire, pas une occurrence en commentaire
	check := func(src string) bool {
		fs := token.NewFileSet()
		file := fs.AddFile("x.go", fs.Base(), len(src))
		var sc scanner.Scanner
		sc.Init(file, []byte(src), nil, 0)
		for {
			_, tok, lit := sc.Scan()
			if tok == token.EOF {
				return false
			}
			if strings.Contains(strings.ToLower(lit), "socialactions") {
				return true
			}
		}
	}
	if !check(`package p; var u = "/socialActions/x"`) || check("package p // socialActions\n/* socialActions */") {
		t.Error("le détecteur de source ne distingue pas code et commentaire")
	}
}

// D5 : « traité » : statut handled, date posée, aucun appel réseau.
func TestD5MarkHandled(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	ct := noNetwork(t)
	p := e.liReply(t, false)
	e.now = e.now.Add(time.Hour)
	got, err := e.st.MarkHandled(p.ID)
	if err != nil || got.Status != StatusHandled || got.HandledAt == nil || !got.HandledAt.Equal(e.now) {
		t.Fatalf("%+v %v", got, err)
	}
	if got.ValidatedBy != "" || got.PublishedAt != nil || got.ScheduledAt != nil {
		t.Fatalf("« traité » n'est ni validé ni publié : %+v", got)
	}
	if _, err := e.st.MarkHandled(p.ID); err == nil {
		t.Error("deux fois « traité »")
	}
	// un traité ne se modifie plus, ne se valide pas, n'est pas repris par PublishDue
	if _, err := e.st.EditPost(p.ID, "autre"); err == nil {
		t.Error("un post traité a été modifié")
	}
	if r := e.st.PublishDue(context.Background()); len(r) != 0 {
		t.Errorf("PublishDue : %+v", r)
	}
	// un post normal ou un post X ne se marque pas « traité »
	if _, err := e.st.MarkHandled(e.liDraft(t, "Un vrai post.").ID); err == nil {
		t.Error("un post LinkedIn normal marqué traité")
	}
	if _, err := e.st.MarkHandled(e.draft(t, "post X").ID); err == nil {
		t.Error("un post X marqué traité")
	}
	if _, err := e.st.MarkHandled("p_inconnu"); err == nil {
		t.Error("post inconnu")
	}
	// rejet possible comme un brouillon normal
	q := e.liReply(t, true)
	if r, err := e.st.Reject(q.ID, "hors sujet"); err != nil || r.Status != StatusRejected {
		t.Fatalf("rejet : %+v %v", r, err)
	}
	if ct.n.Load() != 0 || *calls != 0 {
		t.Fatalf("appels réseau : %d, backend : %d", ct.n.Load(), *calls)
	}
}

// D6 : ces posts ne comptent ni dans WeeklyCap ni dans DailyLimit.
func TestD6NoCapNoDailyLimit(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	_ = e.st.store.Update(func(d *Data) error {
		a := d.Account("pro")
		a.WeeklyCap, a.DailyLimit = 1, 1
		return nil
	})
	// des réponses forgées « publiées » aujourd'hui et planifiées à 09:00 ne prennent aucune place
	for _, comment := range []bool{false, true} {
		p := e.liReply(t, comment)
		at, pub := e.now.Add(time.Hour), e.now.Add(-time.Minute)
		_ = e.st.store.Update(func(d *Data) error {
			q := d.Post(p.ID)
			q.Status, q.ScheduledAt, q.PublishedAt, q.ValidatedBy = StatusPublished, &at, &pub, "Allan"
			return nil
		})
	}
	sched := e.liReply(t, false).ID
	_ = e.st.store.Update(func(d *Data) error { // et une planifiée
		q := d.Post(sched)
		at := e.now.Add(time.Hour)
		q.Status, q.ScheduledAt, q.ValidatedBy = StatusScheduled, &at, "Allan"
		return nil
	})
	d := e.st.store.Snapshot()
	a := d.Account("pro")
	if n := publishedToday(d, a, e.now); n != 0 {
		t.Errorf("DailyLimit : %d réponse(s) comptée(s)", n)
	}
	if ts := weekPostTimes(d, a, ""); len(ts) != 0 {
		t.Errorf("WeeklyCap : %v", ts)
	}
	slot, err := NextSlot(d, a, e.now, "")
	if err != nil || !slot.Equal(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("NextSlot : %v %v", slot, err)
	}
	// un vrai post passe malgré plafond 1 et limite 1
	p := e.liDraft(t, "Un vrai post.")
	if _, err := e.st.Validate(p.ID, "Allan", nil, false); err != nil {
		t.Fatalf("un vrai post est bloqué par des réponses : %v", err)
	}
	// et sur un compte X, les réponses comptent toujours dans la limite quotidienne (inchangé)
	x := e.st.store.Snapshot()
	xa := x.Account("main")
	_ = e.st.store.Update(func(d *Data) error {
		d.Posts = append(d.Posts, &Post{ID: "px", Account: "main", Kind: "reply", Status: StatusPublished, PublishedAt: &e.now})
		return nil
	})
	if publishedToday(e.st.store.Snapshot(), xa, e.now) != 1 {
		t.Error("une réponse X doit compter dans DailyLimit")
	}
}

// D7 : le lien collé n'est jamais appelé.
func TestD7URLNeverCalled(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	ct := noNetwork(t)
	link := srv.URL + "/feed/update/urn:li:activity:1?commentUrn=x#frag"
	p, err := e.st.DraftLinkedInReply(context.Background(), "pro", pastedComment, "  "+link+"  ", false)
	if err != nil || p.TargetURL != link {
		t.Fatalf("lien stocké tel quel : %+v %v", p, err)
	}
	if _, err := e.st.EditPost(p.ID, "Réponse retouchée"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.RewritePost(context.Background(), p.ID, "plus court"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.MarkHandled(p.ID); err != nil {
		t.Fatal(err)
	}
	_ = e.st.PublishDue(context.Background())
	if hits.Load() != 0 || ct.n.Load() != 0 || *calls != 0 {
		t.Fatalf("serveur : %d, client par défaut : %d, backend : %d", hits.Load(), ct.n.Load(), *calls)
	}
	if strings.Contains(e.ai.lastMsg, link) {
		t.Error("le lien n'a pas à partir vers l'IA")
	}
}

// Anti-tics : avertissement non bloquant, recalculé à chaque retouche ; visuel refusé.
type textAI struct{ text string }

func (a textAI) JSON(_ context.Context, _, _ string, _ map[string]any, out any) error {
	b, _ := json.Marshal(map[string]string{"text": a.text, "rationale": "r"})
	return json.Unmarshal(b, out)
}

func TestReplyTicsWarningNonBlocking(t *testing.T) {
	e, _ := liEnv(t, "dryrun", false)
	e.st.ai = func(Settings) AI { return textAI{"Ce n'est pas du marketing, c'est de la confiance."} }
	p := e.liReply(t, false)
	if len(p.Tics) == 0 || len(p.Warnings) == 0 || !strings.Contains(p.Warnings[0], "tics") {
		t.Fatalf("avertissement attendu : %+v", p)
	}
	if p.Status != StatusDraft {
		t.Fatal("un tic ne bloque pas le brouillon")
	}
	// Allan retouche : l'avertissement tombe, une correction est apprise
	got, err := e.st.EditPost(p.ID, "Je réponds à la question du client.")
	if err != nil || len(got.Warnings) != 0 || len(got.Tics) != 0 {
		t.Fatalf("après retouche : %+v %v", got, err)
	}
	if n := len(e.st.store.Snapshot().Learnings); n != 1 {
		t.Fatalf("la correction d'Allan doit alimenter l'apprentissage : %d", n)
	}
	// pas de visuel sur une réponse
	if _, err := e.st.SetMedia(p.ID, "x.png"); err == nil {
		t.Error("visuel accepté sur une réponse")
	}
	if _, err := e.st.SetMediaAlt(p.ID, "alt"); err == nil {
		t.Error("texte alternatif accepté sur une réponse")
	}
}

// Aucun mode auto ne crée ni ne modifie ces brouillons.
func TestTickLeavesReplyDraftsAlone(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	_ = e.st.store.Update(func(d *Data) error {
		a := d.Account("pro")
		a.AutoDraft, a.AutoTriage = true, true
		return nil
	})
	e.liReply(t, false)
	e.liReply(t, true)
	before, _ := json.Marshal(e.st.store.Snapshot().Posts)
	aiBefore := e.ai.count()
	e.now = e.now.Add(48 * time.Hour)
	e.st.Tick(context.Background())
	after, _ := json.Marshal(e.st.store.Snapshot().Posts)
	if string(before) != string(after) {
		t.Fatalf("Tick a touché aux brouillons :\n%s\n%s", before, after)
	}
	if e.ai.count() != aiBefore || *calls != 0 {
		t.Errorf("Tick : appels IA %d→%d, backend %d", aiBefore, e.ai.count(), *calls)
	}
}

// Routes : création, « traité », verrou de validation.
func TestReplyRoutes(t *testing.T) {
	e, calls := liEnv(t, "dryrun", false)
	srv := httptest.NewServer(buildHandler(e.st, ServeOptions{NoAuth: true}))
	t.Cleanup(srv.Close)
	c := srv.Client()
	body, _ := json.Marshal(map[string]any{"account": "pro", "target": pastedPost, "url": "https://www.linkedin.com/posts/x", "comment": true})
	code, m := call(t, c, "POST", srv.URL+"/api/reply-draft", string(body))
	if code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	data := m["data"].(map[string]any)
	id := data["id"].(string)
	if data["kind"] != "comment" || data["status"] != "draft" || data["targetUrl"] != "https://www.linkedin.com/posts/x" || data["line"] != "perso" {
		t.Fatalf("%v", data)
	}
	if code, _ := call(t, c, "POST", srv.URL+"/api/posts/"+id+"/validate", `{"by":"Allan","now":true}`); code == 200 {
		t.Fatal("validation acceptée par la route")
	}
	if code, m := call(t, c, "POST", srv.URL+"/api/reply-draft", `{"account":"pro","target":""}`); code == 200 {
		t.Fatalf("texte vide accepté : %v", m)
	}
	code, m = call(t, c, "POST", srv.URL+"/api/posts/"+id+"/handled", `{}`)
	if code != 200 || m["data"].(map[string]any)["status"] != "handled" || m["data"].(map[string]any)["handledAt"] == nil {
		t.Fatalf("%d %v", code, m)
	}
	if *calls != 0 {
		t.Fatal("backend appelé")
	}
}

// Licence : la notice de linkedin-skills nomme les deux consignes du lot 4.
func TestReplyThirdPartyNotice(t *testing.T) {
	b, err := os.ReadFile("THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"linkedin-reply-handler", "linkedin-comment-drafter", "liReplyMethod", "liCommentMethod"} {
		if !strings.Contains(string(b), w) {
			t.Errorf("THIRD_PARTY_NOTICES.md ne mentionne pas %s", w)
		}
	}
}
