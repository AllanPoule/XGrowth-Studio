package main

// Boîte de réception communautaire X : les réponses reçues sur les posts
// publiés du compte, pour suivre les personnes qui réagissent déjà.
//
// Règles de sécurité :
//   - la récupération se fait dans la boucle IA (TickAI), jamais dans celle de
//     la publication ;
//   - elle est bornée par un plafond de lectures par jour et par compte
//     (l'API officielle facture chaque post lu) et par une fréquence réglable ;
//   - rien n'est jamais envoyé ici : « Rédiger une réponse » crée un brouillon
//     ordinaire (statut draft) qui ne part que par la validation habituelle.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	InboxTodo     = "todo"     // À traiter
	InboxAnswered = "answered" // Répondu
	InboxIgnored  = "ignored"  // Ignoré

	inboxDefaultEvery  = 60  // minutes entre deux passages
	inboxMinEvery      = 15  // minutes
	inboxDefaultCap    = 100 // lectures par jour
	inboxMinCap        = 10  // une page minimale de l'API X
	inboxMaxCap        = 1000
	inboxDefaultWindow = 7  // jours : on suit les posts publiés depuis moins de N jours
	inboxMaxWindow     = 30 // jours
	inboxPostsPerPass  = 3  // posts relus au plus par passage
	inboxMaxPage       = 100
	inboxKeepDays      = 90 // les réponses traitées plus anciennes sont purgées
	inboxMax           = 2000
)

// InboxSettings : réglages de la boîte de réception d'un compte. Désactivée par
// défaut : sur l'API officielle, chaque post lu coûte de l'argent.
type InboxSettings struct {
	Enabled      bool `json:"enabled,omitempty"`
	EveryMinutes int  `json:"everyMinutes,omitempty"` // 0 = 60
	DailyCap     int  `json:"dailyCap,omitempty"`     // réponses lues par jour, 0 = 100
	WindowDays   int  `json:"windowDays,omitempty"`   // 0 = 7
}

func (i InboxSettings) every() time.Duration {
	if i.EveryMinutes < inboxMinEvery {
		return inboxDefaultEvery * time.Minute
	}
	return time.Duration(i.EveryMinutes) * time.Minute
}

func (i InboxSettings) cap() int {
	if i.DailyCap < inboxMinCap {
		return inboxDefaultCap
	}
	if i.DailyCap > inboxMaxCap {
		return inboxMaxCap
	}
	return i.DailyCap
}

func (i InboxSettings) window() time.Duration {
	if i.WindowDays < 1 {
		return inboxDefaultWindow * 24 * time.Hour
	}
	if i.WindowDays > inboxMaxWindow {
		return inboxMaxWindow * 24 * time.Hour
	}
	return time.Duration(i.WindowDays) * 24 * time.Hour
}

// InboxEntry : une réponse reçue sur un post du compte.
type InboxEntry struct {
	ID      string `json:"id"`
	Account string `json:"account"`
	ReplyID string `json:"replyId"` // identifiant X de la réponse reçue (clé de dédoublonnage)
	// contexte : le post d'origine, copié à la réception
	PostID      string    `json:"postId"`      // post du studio
	PostTweetID string    `json:"postTweetId"` // identifiant X du post d'origine
	PostText    string    `json:"postText"`
	Author      string    `json:"author"`
	AuthorName  string    `json:"authorName,omitempty"`
	Followers   int       `json:"followers,omitempty"` // 0 = inconnu
	Text        string    `json:"text"`
	CreatedAt   string    `json:"createdAt,omitempty"`
	Status      string    `json:"status"`
	DraftPostID string    `json:"draftPostId,omitempty"` // brouillon de réponse, sur demande
	FetchedAt   time.Time `json:"fetchedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// InboxState : suivi des lectures d'un compte (clé : nom du compte).
type InboxState struct {
	LastAt time.Time            `json:"lastAt,omitzero"`
	Day    string               `json:"day,omitempty"`  // jour (fuseau du compte) du compteur
	Read   int                  `json:"read,omitempty"` // lectures faites ce jour-là
	PostAt map[string]time.Time `json:"postAt,omitempty"`
}

// InboxResult : bilan d'un passage pour un compte.
type InboxResult struct {
	Account string `json:"account"`
	Posts   int    `json:"posts"`            // posts relus
	New     int    `json:"new"`              // réponses nouvelles
	Read    int    `json:"read"`             // lectures comptées
	Capped  bool   `json:"capped,omitempty"` // plafond du jour atteint
	Error   string `json:"error,omitempty"`
}

// replyFetcher : capacité facultative d'un backend (agentx, xapi). Les autres
// backends (dryrun, manual, LinkedIn) ne la proposent pas.
type replyFetcher interface {
	Replies(ctx context.Context, postID string, n int) ([]Tweet, error)
}

func (b *agentxBackend) Replies(ctx context.Context, postID string, n int) ([]Tweet, error) {
	// `agentx thread` : le post puis sa conversation
	var ts []Tweet
	if err := b.run(ctx, &ts, "thread", postID); err != nil {
		return nil, err
	}
	return ts, nil
}

func (b *xapiBackend) Replies(ctx context.Context, postID string, n int) ([]Tweet, error) {
	// recherche récente `conversation_id:<id>` : toutes les réponses au post
	var l xapiList
	q := url.QueryEscape("conversation_id:" + postID)
	err := b.do(ctx, "GET", fmt.Sprintf("/tweets/search/recent?query=%s&max_results=%d&%s", q, clampXAPI(n), xapiFields), nil, &l)
	return l.tweets(), err
}

func normHandle(h string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h), "@"))
}

// SetInboxSettings règle la boîte de réception d'un compte X.
func (s *Studio) SetInboxSettings(account string, in InboxSettings) (*Account, error) {
	if in.EveryMinutes != 0 && in.EveryMinutes < inboxMinEvery {
		return nil, fmt.Errorf("fréquence minimale : %d minutes", inboxMinEvery)
	}
	if in.DailyCap != 0 && (in.DailyCap < inboxMinCap || in.DailyCap > inboxMaxCap) {
		return nil, fmt.Errorf("plafond de lectures par jour : entre %d et %d", inboxMinCap, inboxMaxCap)
	}
	if in.WindowDays < 0 || in.WindowDays > inboxMaxWindow {
		return nil, fmt.Errorf("fenêtre de suivi : 1 à %d jours", inboxMaxWindow)
	}
	var out *Account
	err := s.store.Update(func(d *Data) error {
		a := d.Account(account)
		if a == nil {
			return fmt.Errorf("compte %q inconnu", account)
		}
		if isLinkedIn(a) {
			return errors.New("la boîte de réception ne concerne que les comptes X")
		}
		a.Inbox = in
		out = a
		return nil
	})
	return out, err
}

// SyncInbox récupère les réponses reçues sur les posts publiés des comptes X
// dont la boîte est activée. account vide = tous ; force ignore la fréquence
// (jamais le plafond). Appelée par TickAI et jamais par TickPublish.
func (s *Studio) SyncInbox(ctx context.Context, account string, force bool) []InboxResult {
	d := s.store.Snapshot()
	var res []InboxResult
	for _, a := range d.Accounts {
		if account != "" && !strings.EqualFold(a.Name, account) {
			continue
		}
		if isLinkedIn(a) || !a.Inbox.Enabled {
			continue
		}
		s.markInboxAnswered(a.Name)
		if r, ran := s.syncInboxAccount(ctx, d, a, force); ran {
			res = append(res, r)
		}
	}
	s.pruneInbox()
	return res
}

func (s *Studio) syncInboxAccount(ctx context.Context, d *Data, a *Account, force bool) (InboxResult, bool) {
	r := InboxResult{Account: a.Name}
	now := s.now()
	day := now.In(location(a)).Format("2006-01-02")
	st := d.InboxState[a.Name]
	if st != nil && !force && !st.LastAt.IsZero() && now.Sub(st.LastAt) < a.Inbox.every() {
		return r, false
	}
	used := 0
	if st != nil && st.Day == day {
		used = st.Read
	}
	limit := a.Inbox.cap()
	// le passage est daté AVANT toute lecture : un échec ne relance pas en boucle
	name := a.Name
	_ = s.store.Update(func(d *Data) error {
		x := d.inboxState(name)
		x.LastAt = now.UTC()
		if x.Day != day {
			x.Day, x.Read = day, 0
		}
		return nil
	})
	be, err := s.backendFor(a)
	if err != nil {
		r.Error = err.Error()
		return r, true
	}
	rf, ok := be.(replyFetcher)
	if !ok {
		r.Error = fmt.Sprintf("le backend %s ne permet pas de lire les réponses reçues", be.Name())
		return r, true
	}
	// posts à suivre : publiés dans la fenêtre, les moins récemment relus d'abord
	var posts []*Post
	for _, p := range d.Posts {
		if p.Account != a.Name || p.Status != StatusPublished || p.TweetID == "" || strings.HasPrefix(p.TweetID, "dry-") || p.PublishedAt == nil {
			continue
		}
		if now.Sub(*p.PublishedAt) > a.Inbox.window() {
			continue
		}
		posts = append(posts, p)
	}
	var last map[string]time.Time
	if st != nil {
		last = st.PostAt
	}
	sort.SliceStable(posts, func(i, j int) bool {
		li, lj := last[posts[i].ID], last[posts[j].ID]
		if !li.Equal(lj) {
			return li.Before(lj)
		}
		return posts[i].PublishedAt.After(*posts[j].PublishedAt)
	})
	me := normHandle(a.Handle)
	for _, p := range posts {
		if r.Posts >= inboxPostsPerPass {
			break
		}
		remaining := limit - used
		if remaining < inboxMinCap {
			r.Capped = true
			break
		}
		n := remaining
		if n > inboxMaxPage {
			n = inboxMaxPage
		}
		pid := p.ID
		ts, err := rf.Replies(ctx, p.TweetID, n)
		r.Posts++
		// agentx ne sait pas limiter la page : on ne garde (et ne compte) que n
		if len(ts) > n {
			ts = ts[:n]
		}
		// chaque appel coûte au moins une lecture, même vide ou en échec :
		// un backend en panne ne vide pas le plafond d'un coup, il l'use.
		charge := len(ts)
		if charge < 1 {
			charge = 1
		}
		used += charge
		r.Read += charge
		if err != nil {
			r.Error = err.Error()
		}
		added := 0
		_ = s.store.Update(func(d *Data) error {
			x := d.inboxState(name)
			if x.PostAt == nil {
				x.PostAt = map[string]time.Time{}
			}
			x.PostAt[pid] = now.UTC()
			x.Day, x.Read = day, used
			if err == nil {
				added = d.addInbox(a, p, ts, me, now.UTC())
			}
			return nil
		})
		r.New += added
		if err != nil {
			break // on n'insiste pas sur un backend en échec
		}
	}
	if r.New > 0 {
		_ = s.store.Update(func(d *Data) error {
			d.AddLog("info", "boîte de réception @%s : %d nouvelle(s) réponse(s)", a.Handle, r.New)
			return nil
		})
	}
	return r, true
}

func (d *Data) inboxState(name string) *InboxState {
	if d.InboxState == nil {
		d.InboxState = map[string]*InboxState{}
	}
	x := d.InboxState[name]
	if x == nil {
		x = &InboxState{}
		d.InboxState[name] = x
	}
	return x
}

// addInbox ajoute les réponses pas encore connues (jamais de doublon, même
// ignorées ou traitées) ; les propres réponses du compte et le post lui-même
// sont écartés.
func (d *Data) addInbox(a *Account, p *Post, ts []Tweet, me string, now time.Time) int {
	known := map[string]bool{}
	for _, e := range d.Inbox {
		if e.Account == a.Name {
			known[e.ReplyID] = true
		}
	}
	n := 0
	for _, t := range ts {
		if t.ID == "" || t.ID == p.TweetID || known[t.ID] || strings.TrimSpace(t.Text) == "" {
			continue
		}
		if me != "" && normHandle(t.Author.ScreenName) == me {
			continue
		}
		known[t.ID] = true
		d.Inbox = append(d.Inbox, &InboxEntry{ID: newID("ib"), Account: a.Name, ReplyID: t.ID, PostID: p.ID, PostTweetID: p.TweetID,
			PostText: p.Text, Author: t.Author.ScreenName, AuthorName: t.Author.Name, Followers: t.Author.Followers,
			Text: t.Text, CreatedAt: t.CreatedAt, Status: InboxTodo, FetchedAt: now, UpdatedAt: now})
		n++
	}
	return n
}

// markInboxAnswered : une réponse dont le brouillon a été publié (par l'action
// habituelle de l'utilisateur) passe à « Répondu ».
func (s *Studio) markInboxAnswered(account string) {
	_ = s.store.Update(func(d *Data) error {
		for _, e := range d.Inbox {
			if e.Account != account || e.Status != InboxTodo || e.DraftPostID == "" {
				continue
			}
			if p := d.Post(e.DraftPostID); p != nil && p.Status == StatusPublished {
				e.Status, e.UpdatedAt = InboxAnswered, s.now().UTC()
			}
		}
		return nil
	})
}

// pruneInbox purge les réponses traitées anciennes et borne la liste.
func (s *Studio) pruneInbox() {
	_ = s.store.Update(func(d *Data) error {
		if len(d.Inbox) == 0 {
			return nil
		}
		cut := s.now().Add(-inboxKeepDays * 24 * time.Hour)
		kept := d.Inbox[:0]
		for _, e := range d.Inbox {
			if e.Status != InboxTodo && e.UpdatedAt.Before(cut) {
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) > inboxMax {
			kept = kept[len(kept)-inboxMax:]
		}
		d.Inbox = kept
		return nil
	})
}

// SetInboxStatus : « À traiter », « Répondu » ou « Ignoré », par décision de l'utilisateur.
func (s *Studio) SetInboxStatus(id, status string) (*InboxEntry, error) {
	switch status {
	case InboxTodo, InboxAnswered, InboxIgnored:
	default:
		return nil, fmt.Errorf("statut %q inconnu : %s, %s ou %s", status, InboxTodo, InboxAnswered, InboxIgnored)
	}
	var out *InboxEntry
	err := s.store.Update(func(d *Data) error {
		e := d.inboxEntry(id)
		if e == nil {
			return fmt.Errorf("réponse %q inconnue", id)
		}
		e.Status, e.UpdatedAt = status, s.now().UTC()
		c := *e
		out = &c
		return nil
	})
	return out, err
}

func (d *Data) inboxEntry(id string) *InboxEntry {
	for _, e := range d.Inbox {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// DraftInboxReply rédige, sur demande seulement, un brouillon de réponse. Le
// brouillon est un post ordinaire au statut « brouillon » : il ne part que par
// la validation habituelle de l'utilisateur. Un brouillon déjà créé est rendu
// tel quel, sans nouvel appel IA.
func (s *Studio) DraftInboxReply(ctx context.Context, id string) (*Post, error) {
	d := s.store.Snapshot()
	e := d.inboxEntry(id)
	if e == nil {
		return nil, fmt.Errorf("réponse %q inconnue", id)
	}
	a := d.Account(e.Account)
	if a == nil || isLinkedIn(a) {
		return nil, errXOnly
	}
	if e.DraftPostID != "" {
		if p := d.Post(e.DraftPostID); p != nil && p.Status != StatusRejected {
			return p, nil
		}
	}
	t := Tweet{ID: e.ReplyID, Text: fmt.Sprintf("(réponse à votre post : « %s »)\n%s", e.PostText, e.Text)}
	t.Author.ScreenName = e.Author
	o, err := DraftReply(ctx, s.aiFor(a), a, d, &t, false)
	if err != nil {
		return nil, err
	}
	p := s.newPost(a, "reply", o.Text, o.Rationale)
	p.TargetText, p.AIText, p.ReplyTo = e.Text, p.Text, e.ReplyID
	err = s.store.Update(func(d *Data) error {
		x := d.inboxEntry(id)
		if x == nil {
			return fmt.Errorf("réponse %q inconnue", id)
		}
		if x.DraftPostID != "" {
			if q := d.Post(x.DraftPostID); q != nil && q.Status != StatusRejected {
				p = q // un autre appel a déjà créé le brouillon
				return nil
			}
		}
		d.Posts = append(d.Posts, p)
		x.DraftPostID, x.UpdatedAt = p.ID, s.now().UTC()
		return nil
	})
	return p, err
}

// inboxView : les réponses d'un compte (ou de tous), les plus récentes d'abord.
func inboxView(d *Data, max int) []*InboxEntry {
	out := append([]*InboxEntry(nil), d.Inbox...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].FetchedAt.After(out[j].FetchedAt) })
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// InboxAccountStatus : réglages effectifs et lectures du jour d'un compte.
type InboxAccountStatus struct {
	Account string     `json:"account"`
	Enabled bool       `json:"enabled"`
	Every   int        `json:"everyMinutes"`
	Cap     int        `json:"dailyCap"`
	Window  int        `json:"windowDays"`
	Read    int        `json:"readToday"`
	LastAt  *time.Time `json:"lastAt,omitempty"`
}

func inboxStatus(d *Data, now time.Time) []InboxAccountStatus {
	out := []InboxAccountStatus{}
	for _, a := range d.Accounts {
		if isLinkedIn(a) {
			continue
		}
		x := InboxAccountStatus{Account: a.Name, Enabled: a.Inbox.Enabled, Every: int(a.Inbox.every() / time.Minute),
			Cap: a.Inbox.cap(), Window: int(a.Inbox.window() / (24 * time.Hour))}
		if st := d.InboxState[a.Name]; st != nil {
			if st.Day == now.In(location(a)).Format("2006-01-02") {
				x.Read = st.Read
			}
			if !st.LastAt.IsZero() {
				t := st.LastAt
				x.LastAt = &t
			}
		}
		out = append(out, x)
	}
	return out
}
