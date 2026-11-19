package main

// Le studio : brouillons -> validation humaine -> planification -> publication,
// plus la veille (comptes suivis, recherches, signets) et son tri par Claude.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Studio struct {
	store      *Store
	ai         func(s Settings) AI
	backendFor func(a *Account) (XBackend, error)
	now        func() time.Time
}

func NewStudio(store *Store) *Studio {
	return &Studio{
		store:      store,
		ai:         NewAI,
		backendFor: BackendFor,
		now:        time.Now,
	}
}

// aiSettings renvoie les réglages d'IA, fournisseur détecté si non choisi.
func (s *Studio) aiSettings() Settings {
	var st Settings
	s.store.View(func(d *Data) { st = d.Settings })
	if st.AIProvider == "" {
		st.AIProvider = DefaultProvider()
	}
	return st
}

func location(a *Account) *time.Location {
	if a.Timezone != "" {
		if loc, err := time.LoadLocation(a.Timezone); err == nil {
			return loc
		}
	}
	return time.Local
}

var slotRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// ---------------------------------------------------------------- comptes

func (s *Studio) SaveAccount(a *Account) error {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" {
		return errors.New("le compte doit avoir un nom (--name)")
	}
	a.Handle = strings.TrimPrefix(strings.TrimSpace(a.Handle), "@")
	switch a.Backend {
	case "":
		a.Backend = "dryrun"
	case "agentx", "xapi", "dryrun":
	default:
		return fmt.Errorf("backend %q inconnu : agentx, xapi ou dryrun", a.Backend)
	}
	if a.Timezone != "" {
		if _, err := time.LoadLocation(a.Timezone); err != nil {
			return fmt.Errorf("fuseau horaire %q inconnu", a.Timezone)
		}
	}
	for _, sl := range a.Slots {
		if !slotRe.MatchString(sl) {
			return fmt.Errorf("créneau %q invalide (format HH:MM)", sl)
		}
	}
	sort.Strings(a.Slots)
	if a.DailyLimit == 0 {
		a.DailyLimit = 6
	}
	if err := normalizeTuning(a); err != nil {
		return err
	}
	if err := normalizeSources(&a.Sources); err != nil {
		return err
	}
	return s.store.Update(func(d *Data) error {
		if old := d.Account(a.Name); old != nil {
			if a.Watch == nil {
				a.Watch = old.Watch
			}
			*old = *a
			return nil
		}
		d.Accounts = append(d.Accounts, a)
		return nil
	})
}

func (s *Studio) AddWatch(account, kind, value string, every int) (*WatchTarget, error) {
	if kind != "user" && kind != "search" {
		return nil, errors.New("type de veille : user ou search")
	}
	value = strings.TrimSpace(value)
	if kind == "user" {
		value = strings.TrimPrefix(value, "@")
	}
	if value == "" {
		return nil, errors.New("valeur de veille vide")
	}
	if every <= 0 {
		every = 60
	}
	w := &WatchTarget{ID: newID("w"), Kind: kind, Value: value, IntervalMin: every}
	err := s.store.Update(func(d *Data) error {
		a := d.Account(account)
		if a == nil {
			return fmt.Errorf("compte %q inconnu", account)
		}
		a.Watch = append(a.Watch, *w)
		return nil
	})
	return w, err
}

func (s *Studio) RemoveWatch(id string) error {
	return s.store.Update(func(d *Data) error {
		for _, a := range d.Accounts {
			for i, w := range a.Watch {
				if w.ID == id {
					a.Watch = append(a.Watch[:i], a.Watch[i+1:]...)
					return nil
				}
			}
		}
		return fmt.Errorf("veille %q inconnue", id)
	})
}

type CheckResult struct {
	Account  string   `json:"account"`
	Backend  string   `json:"backend"`
	OK       bool     `json:"ok"`
	Profile  *Profile `json:"profile,omitempty"`
	Expected string   `json:"expectedHandle,omitempty"`
	Problem  string   `json:"problem,omitempty"`
}

// CheckAccount vérifie que le backend répond et qu'il est connecté au BON
// compte X : publier depuis les cookies d'un autre compte serait grave.
func (s *Studio) CheckAccount(ctx context.Context, name string) (*CheckResult, error) {
	a, _, err := s.accountAndData(name)
	if err != nil {
		return nil, err
	}
	r := &CheckResult{Account: a.Name, Backend: a.Backend, Expected: a.Handle}
	be, err := s.backendFor(a)
	if err != nil {
		r.Problem = err.Error()
		return r, nil
	}
	p, err := be.Me(ctx)
	if err != nil {
		r.Problem = err.Error()
		return r, nil
	}
	r.Profile = p
	switch {
	case a.Backend == "dryrun":
		r.OK = true
	case p.ScreenName == "":
		r.Problem = "le backend n'a pas renvoyé de compte connecté"
	case a.Handle != "" && !strings.EqualFold(p.ScreenName, a.Handle):
		r.Problem = fmt.Sprintf("connecté en tant que @%s alors que le compte attend @%s : vérifiez --agentx-account ou --handle", p.ScreenName, a.Handle)
	default:
		r.OK = true
	}
	return r, nil
}

// ---------------------------------------------------------------- garde-fous

func containsWord(text, word string) bool {
	word = strings.TrimSpace(word)
	if word == "" {
		return false
	}
	re, err := regexp.Compile(`(?i)(^|[^\pL\pN_])@?` + regexp.QuoteMeta(word) + `($|[^\pL\pN_])`)
	if err != nil {
		return strings.Contains(strings.ToLower(text), strings.ToLower(word))
	}
	return re.MatchString(text)
}

// Guard vérifie un texte avant validation ET juste avant publication.
func Guard(d *Data, a *Account, text string) error {
	t := strings.TrimSpace(text)
	if t == "" {
		return errors.New("texte vide")
	}
	if ph := Placeholders(t); len(ph) > 0 {
		return fmt.Errorf("le texte contient encore %d repère(s) à compléter (%s) : remplacez-les par du vécu réel", len(ph), ph[0])
	}
	if n := utf8.RuneCountInString(t); n > maxLen(a) {
		return fmt.Errorf("%d caractères, maximum %d pour ce compte", n, maxLen(a))
	}
	for _, c := range d.Settings.Competitors {
		if containsWord(t, c) {
			return fmt.Errorf("le texte nomme un concurrent (%s)", c)
		}
	}
	for _, p := range d.Settings.BannedPhrases {
		if p != "" && strings.Contains(strings.ToLower(t), strings.ToLower(p)) {
			return fmt.Errorf("le texte contient une expression interdite (%s)", p)
		}
	}
	for _, p := range d.RecentPublished(a.Name, 50) {
		if strings.EqualFold(strings.TrimSpace(p.Text), t) {
			return errors.New("texte identique à un post déjà publié")
		}
	}
	return nil
}

// ---------------------------------------------------------------- brouillons

func (s *Studio) newPost(account, kind, text, rationale string) *Post {
	now := s.now().UTC()
	return &Post{ID: newID("p"), Account: account, Kind: kind, Text: strings.TrimSpace(text), Rationale: rationale,
		Status: StatusDraft, CreatedAt: now, UpdatedAt: now}
}

func (s *Studio) accountAndData(name string) (*Account, *Data, error) {
	d := s.store.Snapshot()
	a := d.Account(name)
	if a == nil {
		return nil, nil, fmt.Errorf("compte %q inconnu", name)
	}
	return a, d, nil
}

// AddDraft enregistre un brouillon écrit à la main (ou par un agent externe).
func (s *Studio) AddDraft(account, text, replyTo, quoteOf, media string) (*Post, error) {
	kind := "post"
	if replyTo != "" {
		kind = "reply"
	} else if quoteOf != "" {
		kind = "quote"
	}
	p := s.newPost(account, kind, text, "")
	p.ReplyTo, p.QuoteOf, p.MediaPath = parseTweetID(replyTo), parseTweetID(quoteOf), media
	if p.Text == "" && media == "" {
		return nil, errors.New("texte vide")
	}
	err := s.store.Update(func(d *Data) error {
		if d.Account(account) == nil {
			return fmt.Errorf("compte %q inconnu", account)
		}
		d.Posts = append(d.Posts, p)
		return nil
	})
	return p, err
}

// GenerateDrafts fait rédiger n brouillons par Claude, à partir d'un brief
// et/ou d'un contenu de veille.
func (s *Studio) GenerateDrafts(ctx context.Context, account, brief, itemID string, n int, postType string) ([]*Post, error) {
	a, d, err := s.accountAndData(account)
	if err != nil {
		return nil, err
	}
	var it *Item
	if itemID != "" {
		if it = d.Item(itemID); it == nil {
			return nil, fmt.Errorf("contenu %q inconnu", itemID)
		}
	}
	outs, err := GenerateDrafts(ctx, s.aiFor(a), a, d, DraftRequest{Brief: brief, Item: it, N: n, Type: postType, Now: s.now()})
	if err != nil {
		return nil, err
	}
	var posts []*Post
	for _, o := range outs {
		p := s.newPost(a.Name, "post", o.Text, o.Rationale)
		p.PostType, p.VisualBrief, p.AIText = o.Type, strings.TrimSpace(o.Visual), p.Text
		if it != nil {
			p.SourceItem = it.ID
		}
		posts = append(posts, p)
	}
	return posts, s.store.Update(func(d *Data) error {
		d.Posts = append(d.Posts, posts...)
		if it != nil {
			if x := d.Item(it.ID); x != nil {
				x.Status = ItemDrafted
			}
		}
		return nil
	})
}

// DraftReply récupère un post X et fait rédiger une réponse (ou une citation).
func (s *Studio) DraftReply(ctx context.Context, account, ref string, quote bool) (*Post, error) {
	a, d, err := s.accountAndData(account)
	if err != nil {
		return nil, err
	}
	be, err := s.backendFor(a)
	if err != nil {
		return nil, err
	}
	id := parseTweetID(ref)
	t, err := be.Tweet(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Text == "" {
		return nil, fmt.Errorf("impossible de lire le post %s", id)
	}
	o, err := DraftReply(ctx, s.aiFor(a), a, d, t, quote)
	if err != nil {
		return nil, err
	}
	kind := "reply"
	if quote {
		kind = "quote"
	}
	p := s.newPost(a.Name, kind, o.Text, o.Rationale)
	p.TargetText, p.AIText = t.Text, p.Text
	if quote {
		p.QuoteOf = id
	} else {
		p.ReplyTo = id
	}
	return p, s.store.Update(func(d *Data) error { d.Posts = append(d.Posts, p); return nil })
}

func editable(p *Post) error {
	switch p.Status {
	case StatusDraft, StatusScheduled, StatusValidated, StatusFailed, StatusRejected:
		return nil
	}
	return fmt.Errorf("le post %s est %s : il ne peut plus être modifié", p.ID, p.Status)
}

// resetValidation : toute modification d'un post validé le renvoie en brouillon.
func resetValidation(p *Post) {
	p.Status = StatusDraft
	p.ScheduledAt, p.ValidatedAt, p.ValidatedBy, p.Error = nil, nil, "", ""
}

func (s *Studio) EditPost(id, text string) (*Post, error) {
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if err := editable(p); err != nil {
			return err
		}
		p.History = append(p.History, Revision{Text: p.Text, At: s.now().UTC()})
		p.Text = strings.TrimSpace(text)
		recordCorrection(d, p, s.now().UTC())
		resetValidation(p)
		p.UpdatedAt = s.now().UTC()
		out = p
		return nil
	})
	return out, err
}

func (s *Studio) SetMedia(id, path string) (*Post, error) {
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if err := editable(p); err != nil {
			return err
		}
		p.MediaPath = path
		resetValidation(p)
		out = p
		return nil
	})
	return out, err
}

// RewritePost fait réécrire un brouillon et garde le retour comme leçon.
func (s *Studio) RewritePost(ctx context.Context, id, feedback string) (*Post, error) {
	d := s.store.Snapshot()
	p := d.Post(id)
	if p == nil {
		return nil, fmt.Errorf("post %q inconnu", id)
	}
	if err := editable(p); err != nil {
		return nil, err
	}
	a := d.Account(p.Account)
	if a == nil {
		return nil, fmt.Errorf("compte %q inconnu", p.Account)
	}
	o, err := Rewrite(ctx, s.aiFor(a), a, d, p, feedback)
	if err != nil {
		return nil, err
	}
	var out *Post
	err = s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		p.History = append(p.History, Revision{Text: p.Text, Feedback: feedback, At: s.now().UTC()})
		p.Text, p.Rationale = strings.TrimSpace(o.Text), o.Rationale
		p.AIText = p.Text
		resetValidation(p)
		p.UpdatedAt = s.now().UTC()
		d.Learnings = append(d.Learnings, Learning{ID: newID("l"), Account: p.Account, Text: feedback, Source: FeedbackRewrite, PostID: p.ID, CreatedAt: s.now().UTC()})
		out = p
		return nil
	})
	return out, err
}

func (s *Studio) Reject(id, reason string) (*Post, error) {
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if err := editable(p); err != nil {
			return err
		}
		resetValidation(p)
		p.Status = StatusRejected
		p.Error = reason
		p.UpdatedAt = s.now().UTC()
		if strings.TrimSpace(reason) != "" {
			d.Learnings = append(d.Learnings, Learning{ID: newID("l"), Account: p.Account, Text: "Brouillon rejeté : " + reason, Source: FeedbackReject, PostID: p.ID, CreatedAt: s.now().UTC()})
		}
		out = p
		return nil
	})
	return out, err
}

func (s *Studio) Unschedule(id string) (*Post, error) {
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if p.Status != StatusScheduled && p.Status != StatusValidated {
			return fmt.Errorf("le post %s n'est pas planifié", id)
		}
		resetValidation(p)
		out = p
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------- validation et planification

// NextSlot renvoie le premier créneau libre du compte après `after`.
func NextSlot(d *Data, a *Account, after time.Time, skipID string) (time.Time, error) {
	if a.Timing.Mode == "window" {
		return nextWindowSlot(d, a, after, skipID)
	}
	if len(a.Slots) == 0 {
		return time.Time{}, fmt.Errorf("le compte %s n'a pas de créneaux : ajoutez-en (--slots 09:00,13:00) ou validez avec --now / --at", a.Name)
	}
	loc := location(a)
	gap := time.Duration(d.Settings.MinGapMinutes) * time.Minute
	var taken []time.Time
	for _, p := range d.Posts {
		// les réponses de campagne ont leur propre rythme
		if p.Account != a.Name || p.ID == skipID || p.CampaignID != "" {
			continue
		}
		switch {
		case (p.Status == StatusScheduled || p.Status == StatusValidated || p.Status == StatusPublishing) && p.ScheduledAt != nil:
			taken = append(taken, *p.ScheduledAt)
		case p.Status == StatusPublished && p.PublishedAt != nil:
			taken = append(taken, *p.PublishedAt)
		}
	}
	perDay := map[string]int{}
	for _, t := range taken {
		perDay[t.In(loc).Format("2006-01-02")]++
	}
	start := after.In(loc)
	for day := 0; day < 60; day++ {
		dd := start.AddDate(0, 0, day)
		key := dd.Format("2006-01-02")
		if a.DailyLimit > 0 && perDay[key] >= a.DailyLimit {
			continue
		}
		for _, sl := range a.Slots {
			var h, m int
			fmt.Sscanf(sl, "%d:%d", &h, &m)
			cand := time.Date(dd.Year(), dd.Month(), dd.Day(), h, m, 0, 0, loc)
			if !cand.After(after.Add(2 * time.Minute)) {
				continue
			}
			free := true
			for _, t := range taken {
				diff := cand.Sub(t)
				if diff < 0 {
					diff = -diff
				}
				if diff < gap || diff == 0 {
					free = false
					break
				}
			}
			if free {
				return cand.UTC(), nil
			}
		}
	}
	return time.Time{}, errors.New("aucun créneau libre dans les 60 prochains jours")
}

// Validate est l'acte humain qui autorise la publication. `by` est obligatoire.
// at : date imposée ; now : publier au prochain passage ; sinon prochain créneau.
func (s *Studio) Validate(id, by string, at *time.Time, now bool) (*Post, error) {
	by = strings.TrimSpace(by)
	if by == "" {
		return nil, errors.New("la validation doit être signée (--by <nom>)")
	}
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if p.Status != StatusDraft && p.Status != StatusFailed && p.Status != StatusRejected {
			return fmt.Errorf("le post %s est %s : seul un brouillon peut être validé", id, p.Status)
		}
		a := d.Account(p.Account)
		if a == nil {
			return fmt.Errorf("compte %q inconnu", p.Account)
		}
		if err := Guard(d, a, p.Text); err != nil {
			return fmt.Errorf("validation refusée : %w", err)
		}
		t := s.now().UTC()
		switch {
		case now:
			p.Status = StatusValidated
			p.ScheduledAt = &t
		case at != nil:
			if at.Before(t.Add(-time.Minute)) {
				return errors.New("la date de publication est passée")
			}
			u := at.UTC()
			p.Status, p.ScheduledAt = StatusScheduled, &u
		case p.CampaignID != "":
			c := d.Campaign(p.CampaignID)
			if c == nil {
				return errors.New("la campagne de cette réponse a été supprimée : choisissez l'heure ou publiez maintenant")
			}
			slot, err := nextReplySlot(d, a, c, t, p.ID)
			if err != nil {
				return err
			}
			p.Status, p.ScheduledAt = StatusScheduled, &slot
		default:
			slot, err := NextSlot(d, a, t, p.ID)
			if err != nil {
				return err
			}
			p.Status, p.ScheduledAt = StatusScheduled, &slot
		}
		p.ValidatedBy, p.ValidatedAt, p.Error = by, &t, ""
		p.UpdatedAt = t
		out = p
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------- publication

func publishedToday(d *Data, a *Account, now time.Time) int {
	loc := location(a)
	today := now.In(loc).Format("2006-01-02")
	n := 0
	for _, p := range d.Posts {
		if p.Account == a.Name && p.CampaignID == "" && p.Status == StatusPublished && p.PublishedAt != nil && p.PublishedAt.In(loc).Format("2006-01-02") == today {
			n++
		}
	}
	return n
}

type PublishResult struct {
	PostID  string `json:"postId"`
	Account string `json:"account"`
	Status  string `json:"status"`
	TweetID string `json:"tweetId,omitempty"`
	Error   string `json:"error,omitempty"`
}

// PublishDue publie les posts validés dont l'heure est venue. Chaque post est
// d'abord « réservé » (publishing) pour qu'aucun autre processus ne l'envoie.
func (s *Studio) PublishDue(ctx context.Context) []PublishResult {
	var results []PublishResult
	var due []string
	s.store.View(func(d *Data) {
		now := s.now()
		for _, p := range d.Posts {
			if (p.Status == StatusScheduled || p.Status == StatusValidated) && p.ScheduledAt != nil && !p.ScheduledAt.After(now) {
				due = append(due, p.ID)
			}
		}
	})
	for _, id := range due {
		var post Post
		var acct Account
		skip := ""
		err := s.store.Update(func(d *Data) error {
			p := d.Post(id)
			if p == nil || (p.Status != StatusScheduled && p.Status != StatusValidated) {
				return errors.New("déjà traité")
			}
			a := d.Account(p.Account)
			if a == nil {
				p.Status, p.Error = StatusFailed, "compte supprimé"
				skip = p.Error
				return nil
			}
			if p.ValidatedBy == "" || p.ValidatedAt == nil {
				p.Status, p.Error = StatusDraft, "post non validé : renvoyé en brouillon"
				skip = p.Error
				return nil
			}
			if err := Guard(d, a, p.Text); err != nil {
				p.Status, p.Error = StatusFailed, "bloqué avant publication : "+err.Error()
				skip = p.Error
				return nil
			}
			if p.CampaignID == "" && a.DailyLimit > 0 && publishedToday(d, a, s.now()) >= a.DailyLimit {
				next, err := NextSlot(d, a, s.now().UTC(), p.ID)
				if err != nil {
					p.Status, p.Error = StatusFailed, "limite quotidienne atteinte et aucun créneau libre"
				} else {
					p.ScheduledAt = &next
					p.Error = "limite quotidienne atteinte : reporté"
				}
				skip = p.Error
				return nil
			}
			p.Status = StatusPublishing
			post, acct = *p, *a
			return nil
		})
		if err != nil {
			continue
		}
		if skip != "" {
			results = append(results, PublishResult{PostID: id, Status: "skipped", Error: skip})
			continue
		}
		var tweetID string
		be, err := s.backendFor(&acct)
		if err == nil && acct.Backend != "dryrun" && acct.Handle != "" {
			// vérifie l'identité du compte connecté avant chaque envoi
			var me *Profile
			if me, err = be.Me(ctx); err == nil && !strings.EqualFold(me.ScreenName, acct.Handle) {
				err = fmt.Errorf("envoi annulé : le backend est connecté en tant que @%s, pas @%s", me.ScreenName, acct.Handle)
			}
		}
		if err == nil {
			tweetID, err = be.Post(ctx, PostRequest{Text: post.Text, ReplyTo: post.ReplyTo, QuoteOf: post.QuoteOf, MediaPath: post.MediaPath})
		}
		r := PublishResult{PostID: id, Account: acct.Name}
		_ = s.store.Update(func(d *Data) error {
			p := d.Post(id)
			if p == nil {
				return nil
			}
			t := s.now().UTC()
			p.UpdatedAt = t
			if err != nil {
				p.Status, p.Error = StatusFailed, err.Error()
				d.AddLog("error", "publication de %s (@%s) échouée : %v", id, acct.Handle, err)
				r.Status, r.Error = StatusFailed, err.Error()
				return nil
			}
			p.Status, p.TweetID, p.PublishedAt, p.Error = StatusPublished, tweetID, &t, ""
			d.AddLog("info", "publié %s sur @%s (%s, %s)", id, acct.Handle, tweetID, acct.Backend)
			r.Status, r.TweetID = StatusPublished, tweetID
			return nil
		})
		results = append(results, r)
	}
	return results
}

// ---------------------------------------------------------------- veille

func itemFromTweet(account, source string, t Tweet, now time.Time) *Item {
	return &Item{ID: newID("i"), Account: account, Source: source, TweetID: t.ID,
		URL:    fmt.Sprintf("https://x.com/%s/status/%s", t.Author.ScreenName, t.ID),
		Author: t.Author.ScreenName, Text: t.Text, Likes: t.Metrics.Likes, Views: t.Metrics.Views,
		Status: ItemPending, FetchedAt: now.UTC()}
}

func (s *Studio) addItems(account, source string, ts []Tweet, limit int) int {
	added := 0
	_ = s.store.Update(func(d *Data) error {
		for _, t := range ts {
			if added >= limit {
				break
			}
			if t.ID == "" || t.IsRetweet || strings.TrimSpace(t.Text) == "" || d.HasItemTweet(account, t.ID) {
				continue
			}
			d.Items = append(d.Items, itemFromTweet(account, source, t, s.now()))
			added++
		}
		return nil
	})
	return added
}

type FetchResult struct {
	Account string `json:"account"`
	Source  string `json:"source"`
	Added   int    `json:"added"`
	Error   string `json:"error,omitempty"`
}

// PollWatch interroge les cibles de veille arrivées à échéance (toutes si force).
func (s *Studio) PollWatch(ctx context.Context, account string, force bool) []FetchResult {
	d := s.store.Snapshot()
	var res []FetchResult
	for _, a := range d.Accounts {
		if account != "" && !strings.EqualFold(a.Name, account) {
			continue
		}
		be, err := s.backendFor(a)
		if err != nil {
			res = append(res, FetchResult{Account: a.Name, Error: err.Error()})
			continue
		}
		for _, w := range a.Watch {
			if !force && !w.LastPolled.IsZero() && s.now().Sub(w.LastPolled) < time.Duration(w.IntervalMin)*time.Minute {
				continue
			}
			var ts []Tweet
			src := "watch:" + w.Kind + ":" + w.Value
			if w.Kind == "user" {
				ts, err = be.UserPosts(ctx, w.Value, 20)
			} else {
				ts, err = be.Search(ctx, w.Value, 20)
			}
			r := FetchResult{Account: a.Name, Source: src}
			if err != nil {
				r.Error = err.Error()
			} else {
				r.Added = s.addItems(a.Name, src, ts, 20)
			}
			wid := w.ID
			_ = s.store.Update(func(d *Data) error {
				if acc := d.Account(a.Name); acc != nil {
					for i := range acc.Watch {
						if acc.Watch[i].ID == wid {
							acc.Watch[i].LastPolled = s.now().UTC()
						}
					}
				}
				if r.Error != "" {
					d.AddLog("error", "veille %s (%s) : %s", src, a.Name, r.Error)
				}
				return nil
			})
			res = append(res, r)
		}
	}
	return res
}

func (s *Studio) ImportBookmarks(ctx context.Context, account string, n int) (FetchResult, error) {
	a, _, err := s.accountAndData(account)
	if err != nil {
		return FetchResult{}, err
	}
	be, err := s.backendFor(a)
	if err != nil {
		return FetchResult{}, err
	}
	ts, err := be.Bookmarks(ctx, n)
	if err != nil {
		return FetchResult{}, err
	}
	return FetchResult{Account: a.Name, Source: "bookmarks", Added: s.addItems(a.Name, "bookmarks", ts, n)}, nil
}

func (s *Studio) AddItem(ctx context.Context, account, ref string) (*Item, error) {
	a, _, err := s.accountAndData(account)
	if err != nil {
		return nil, err
	}
	be, err := s.backendFor(a)
	if err != nil {
		return nil, err
	}
	t, err := be.Tweet(ctx, parseTweetID(ref))
	if err != nil {
		return nil, err
	}
	it := itemFromTweet(a.Name, "manual", *t, s.now())
	return it, s.store.Update(func(d *Data) error { d.Items = append(d.Items, it); return nil })
}

func (s *Studio) SetItemStatus(id, status string) (*Item, error) {
	var out *Item
	err := s.store.Update(func(d *Data) error {
		it := d.Item(id)
		if it == nil {
			return fmt.Errorf("contenu %q inconnu", id)
		}
		it.Status = status
		out = it
		return nil
	})
	return out, err
}

type TriageResult struct {
	Account string `json:"account"`
	Triaged int    `json:"triaged"`
	Kept    int    `json:"kept"`
	Drafted int    `json:"drafted"`
	Error   string `json:"error,omitempty"`
}

// TriagePending fait noter par l'IA les contenus « à trier » qu'elle n'a pas
// encore vus. L'IA ne fait que suggérer (Suggestion, Score, Reason, Angle) :
// le contenu reste « à trier » jusqu'à une action de l'utilisateur. Seule
// exception, voulue par l'utilisateur : avec AutoDraft (ou draft=true), un
// brouillon est rédigé pour chaque contenu suggéré, qui passe en « rédigé ».
func (s *Studio) TriagePending(ctx context.Context, account string, onlyAuto, draft bool) []TriageResult {
	d := s.store.Snapshot()
	var res []TriageResult
	for _, a := range d.Accounts {
		if account != "" && !strings.EqualFold(a.Name, account) {
			continue
		}
		if onlyAuto && !a.AutoTriage {
			continue
		}
		var pending []*Item
		for _, it := range d.Items {
			if it.Account == a.Name && it.Status == ItemPending && it.TriagedAt == nil {
				pending = append(pending, it)
			}
		}
		if max := d.Settings.MaxItemsPerRun; max > 0 && len(pending) > max {
			pending = pending[:max]
		}
		r := TriageResult{Account: a.Name}
		for start := 0; start < len(pending); start += 20 {
			batch := pending[start:min(start+20, len(pending))]
			outs, err := Triage(ctx, s.aiFor(a), a, d, batch)
			if err != nil {
				r.Error = err.Error()
				break
			}
			var kept []string
			_ = s.store.Update(func(d *Data) error {
				for _, o := range outs {
					it := d.Item(o.ID)
					if it == nil || it.Status != ItemPending || it.TriagedAt != nil {
						continue
					}
					now := s.now().UTC()
					it.Score, it.Reason, it.Angle, it.TriagedAt = o.Score, o.Reason, o.Angle, &now
					if o.Keep && o.Score >= d.Settings.TriageMinScore {
						it.Suggestion = SuggestKeep
						kept = append(kept, it.ID)
					} else {
						it.Suggestion = SuggestSkip
					}
					r.Triaged++
				}
				return nil
			})
			r.Kept += len(kept)
			if draft || a.AutoDraft {
				for _, id := range kept {
					if _, err := s.GenerateDrafts(ctx, a.Name, "", id, 1, ""); err == nil {
						r.Drafted++
					}
				}
			}
		}
		res = append(res, r)
	}
	return res
}

// ---------------------------------------------------------------- statistiques

func (s *Studio) RefreshMetrics(ctx context.Context, maxAge time.Duration) (int, error) {
	d := s.store.Snapshot()
	updated := 0
	var lastErr error
	for _, p := range d.Posts {
		if p.Status != StatusPublished || p.TweetID == "" || strings.HasPrefix(p.TweetID, "dry-") || p.PublishedAt == nil {
			continue
		}
		if s.now().Sub(*p.PublishedAt) > 14*24*time.Hour {
			continue
		}
		if p.Metrics != nil && s.now().Sub(p.Metrics.UpdatedAt) < maxAge {
			continue
		}
		a := d.Account(p.Account)
		if a == nil {
			continue
		}
		be, err := s.backendFor(a)
		if err != nil {
			lastErr = err
			continue
		}
		t, err := be.Tweet(ctx, p.TweetID)
		if err != nil {
			lastErr = err
			continue
		}
		m := &Metrics{Likes: t.Metrics.Likes, Retweets: t.Metrics.Retweets, Replies: t.Metrics.Replies,
			Quotes: t.Metrics.Quotes, Views: t.Metrics.Views, Bookmarks: t.Metrics.Bookmarks, UpdatedAt: s.now().UTC()}
		if m.Views > 0 {
			eng := m.Likes + m.Retweets + m.Replies + m.Quotes + m.Bookmarks
			m.EngagementRate = float64(eng) / float64(m.Views) * 100
		}
		id := p.ID
		_ = s.store.Update(func(d *Data) error {
			if x := d.Post(id); x != nil {
				x.Metrics = m
			}
			return nil
		})
		updated++
	}
	return updated, lastErr
}

// Tick est un passage du planificateur : publication, veille, tri, statistiques.
func (s *Studio) Tick(ctx context.Context) map[string]any {
	out := map[string]any{"at": s.now().UTC()}
	if r := s.PublishDue(ctx); len(r) > 0 {
		out["published"] = r
	}
	if r := s.PollWatch(ctx, "", false); len(r) > 0 {
		out["watch"] = r
	}
	if r := s.TriagePending(ctx, "", true, false); len(r) > 0 {
		out["triage"] = r
	}
	if r := s.PollSources(ctx, "", false); len(r) > 0 {
		out["sources"] = r
	}
	if r := s.DraftNews(ctx); len(r) > 0 {
		out["news"] = r
	}
	if n, _ := s.RefreshMetrics(ctx, 6*time.Hour); n > 0 {
		out["metrics"] = n
	}
	if r := s.RunCampaigns(ctx); len(r) > 0 {
		out["campaigns"] = r
	}
	if synthesisDue(s.store.Snapshot(), s.now()) {
		if r, err := s.Synthesize(ctx, ""); err == nil {
			out["synthesis"] = r
		} else {
			_ = s.store.Update(func(d *Data) error { d.AddLog("error", "synthèse des retours : %v", err); return nil })
		}
	}
	if due := perfDueAccounts(s.store.Snapshot(), s.now()); len(due) > 0 {
		r, err := s.analyzeAccounts(ctx, due)
		var pe *perfError
		switch {
		case err == nil:
			// rien à montrer quand aucun compte n'a eu de quoi appeler l'IA
			for _, a := range r.Accounts {
				if a.Skipped == "" {
					out["perf"] = r
					break
				}
			}
		case errors.Is(err, errPerfBusy), errors.As(err, &pe):
			// analyse déjà en cours, ou échec déjà journalisé compte par compte
		default:
			// toute autre erreur vient du magasin (disque, data.json) : son message
			// peut citer un chemin local, le journal n'en garde que la catégorie
			_ = s.store.Update(func(d *Data) error {
				d.AddLog("error", "analyse des performances : stockage")
				return nil
			})
		}
	}
	return out
}
