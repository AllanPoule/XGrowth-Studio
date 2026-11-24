package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// Statistiques de la page Performances : calcul seul, sans appel IA. Règle de
// fond : une donnée absente est « indisponible » (pointeur nil, jamais 0) ; un
// zéro n'apparaît que s'il a été mesuré. Les comparaisons décrivent ce qui a
// été observé, elles n'affirment aucune cause.

const followersKeepDays = 400

// followersRetryGap : au plus une tentative de relevé des abonnés par heure et par compte.
const followersRetryGap = time.Hour

// StatsFilter : compte (vide = tous) et période en jours (0 = tout l'historique).
type StatsFilter struct {
	Account string
	Days    int
}

// StatPost : une ligne de l'historique.
type StatPost struct {
	ID            string     `json:"id"`
	Account       string     `json:"account"`
	Text          string     `json:"text"`
	PublishedAt   time.Time  `json:"publishedAt"`
	Kind          string     `json:"kind"`
	Length        string     `json:"length"` // court | moyen | long
	HasMedia      bool       `json:"hasMedia"`
	Origin        string     `json:"origin"` // campagne | idée | veille | manuel
	Measured      bool       `json:"measured"`
	MeasuredAt    *time.Time `json:"measuredAt"` // fraîcheur : date du dernier relevé (nil = jamais relevé)
	Late          bool       `json:"late"`       // le relevé est assez tardif pour l'analyse
	Views         *int       `json:"views"`
	Likes         *int       `json:"likes"`
	Retweets      *int       `json:"retweets"`
	Replies       *int       `json:"replies"`
	EngagementPct *float64   `json:"engagementPct"`
	Unavailable   string     `json:"unavailable,omitempty"` // raison, quand rien n'est mesuré
}

// StatGroup : un groupe d'une comparaison (format ou origine).
type StatGroup struct {
	Key              string   `json:"key"`
	Label            string   `json:"label"`
	Posts            int      `json:"posts"`
	WithEngagement   int      `json:"withEngagement"`   // posts dont le taux est connu
	MedianEngagement *float64 `json:"medianEngagement"` // nil = indisponible
	Views            *int     `json:"views"`            // somme des vues connues, nil si aucune
	ViewsPosts       int      `json:"viewsPosts"`       // posts dont les vues sont connues
}

type StatTotals struct {
	Posts        int  `json:"posts"`
	Measured     int  `json:"measured"`
	ViewsPosts   int  `json:"viewsPosts"` // posts dont les vues sont connues
	Views        *int `json:"views"`
	Likes        *int `json:"likes"`
	Retweets     *int `json:"retweets"`
	Replies      *int `json:"replies"`
	NeverMeasure int  `json:"neverMeasured"`
}

// FollowerPoint : un jour de suivi des abonnés.
type FollowerPoint struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type FollowerSeries struct {
	Account     string          `json:"account"`
	Available   bool            `json:"available"`
	Unavailable string          `json:"unavailable,omitempty"`
	Points      []FollowerPoint `json:"points"`
	Change      *int            `json:"change"` // dernier - premier relevé de la période, nil si < 2 relevés
}

type StatsView struct {
	Account     string                 `json:"account"`
	Days        int                    `json:"days"`
	Totals      StatTotals             `json:"totals"`
	LastMeasure *time.Time             `json:"lastMeasure"` // relevé le plus récent
	Posts       []StatPost             `json:"posts"`
	Compare     map[string][]StatGroup `json:"compare"` // kind | length | media | origin
	Followers   []FollowerSeries       `json:"followers"`
	Note        string                 `json:"note"`
}

const statsNote = "Comparaison descriptive : elle montre ce qui a été observé sur vos posts, pas pourquoi un groupe fait mieux. " +
	"« Indisponible » veut dire que la mesure n'existe pas, ce n'est pas un zéro."

func postOrigin(p *Post) string {
	switch {
	case p.CampaignID != "":
		return "campagne"
	case p.IdeaID != "":
		return "idée"
	case p.SourceItem != "":
		return "veille"
	}
	return "manuel"
}

func ip(n int) *int             { return &n }
func fp(f float64) *float64     { return &f }
func tp(t time.Time) *time.Time { return &t }

// statPostOf construit la ligne d'un post publié.
func statPostOf(d *Data, p *Post) StatPost {
	sp := StatPost{ID: p.ID, Account: p.Account, Text: oneLine(p.Text), PublishedAt: *p.PublishedAt, Kind: p.Kind,
		Length: perfLength(p.Text), HasMedia: hasMedia(p), Origin: postOrigin(p)}
	if r := []rune(sp.Text); len(r) > 140 {
		sp.Text = string(r[:140]) + "…"
	}
	m := p.Metrics
	switch {
	case isLinkedIn(d.Account(p.Account)):
		sp.Unavailable = "LinkedIn : aucune mesure disponible"
	case strings.HasPrefix(p.TweetID, "dry-"):
		sp.Unavailable = "publication simulée : aucune mesure"
	case m == nil:
		sp.Unavailable = "pas encore relevé"
	default:
		sp.Measured = true
		if !m.UpdatedAt.IsZero() {
			sp.MeasuredAt = tp(m.UpdatedAt)
		}
		sp.Late = hasLateMetrics(p)
		sp.Likes, sp.Retweets, sp.Replies = ip(m.Likes), ip(m.Retweets), ip(m.Replies)
		if m.ViewsAvailable() {
			sp.Views = ip(m.Views)
			if m.Views > 0 {
				sp.EngagementPct = fp(m.EngagementRate)
			}
		}
	}
	return sp
}

func groupOf(key, label string, ps []StatPost) StatGroup {
	g := StatGroup{Key: key, Label: label, Posts: len(ps)}
	var rates []float64
	views := 0
	for _, p := range ps {
		if p.EngagementPct != nil {
			rates = append(rates, *p.EngagementPct)
		}
		if p.Views != nil {
			views += *p.Views
			g.ViewsPosts++
		}
	}
	g.WithEngagement = len(rates)
	if len(rates) > 0 {
		g.MedianEngagement = fp(median(rates))
	}
	if g.ViewsPosts > 0 {
		g.Views = ip(views)
	}
	return g
}

// compare groupe les posts par une dimension, dans l'ordre fixé par order.
func compare(ps []StatPost, key func(StatPost) string, order []string, labels map[string]string) []StatGroup {
	by := map[string][]StatPost{}
	for _, p := range ps {
		k := key(p)
		by[k] = append(by[k], p)
	}
	var out []StatGroup
	for _, k := range order {
		if len(by[k]) > 0 {
			l := labels[k]
			if l == "" {
				l = k
			}
			out = append(out, groupOf(k, l, by[k]))
		}
	}
	return out
}

// followersSupported : le backend du compte renvoie le nombre d'abonnés.
func followersSupported(a *Account) bool {
	return a != nil && !isLinkedIn(a) && (a.Backend == "agentx" || a.Backend == "xapi")
}

// ComputeStats calcule la vue Performances selon le filtre.
func ComputeStats(d *Data, f StatsFilter, now time.Time) StatsView {
	v := StatsView{Account: f.Account, Days: f.Days, Posts: []StatPost{}, Compare: map[string][]StatGroup{}, Note: statsNote}
	since := time.Time{}
	if f.Days > 0 {
		since = now.Add(-time.Duration(f.Days) * 24 * time.Hour)
	}
	for _, p := range d.Posts {
		if p.Status != StatusPublished || p.PublishedAt == nil || (f.Account != "" && p.Account != f.Account) || p.PublishedAt.Before(since) {
			continue
		}
		v.Posts = append(v.Posts, statPostOf(d, p))
	}
	sort.SliceStable(v.Posts, func(i, j int) bool { return v.Posts[i].PublishedAt.After(v.Posts[j].PublishedAt) })

	var views, likes, rts, reps int
	var nl int
	t := &v.Totals
	t.Posts = len(v.Posts)
	for _, p := range v.Posts {
		if !p.Measured {
			if p.MeasuredAt == nil && strings.HasPrefix(p.Unavailable, "pas encore") {
				t.NeverMeasure++
			}
			continue
		}
		t.Measured++
		if p.MeasuredAt != nil && (v.LastMeasure == nil || p.MeasuredAt.After(*v.LastMeasure)) {
			v.LastMeasure = p.MeasuredAt
		}
		likes += *p.Likes
		rts += *p.Retweets
		reps += *p.Replies
		nl++
		if p.Views != nil {
			views += *p.Views
			t.ViewsPosts++
		}
	}
	if t.ViewsPosts > 0 {
		t.Views = ip(views)
	}
	if nl > 0 {
		t.Likes, t.Retweets, t.Replies = ip(likes), ip(rts), ip(reps)
	}

	v.Compare["kind"] = compare(v.Posts, func(p StatPost) string { return p.Kind }, []string{"post", "reply", "quote", "comment"},
		map[string]string{"post": "Post", "reply": "Réponse", "quote": "Citation", "comment": "Commentaire"})
	v.Compare["length"] = compare(v.Posts, func(p StatPost) string { return p.Length }, []string{"court", "moyen", "long"},
		map[string]string{"court": "Court (≤ 140)", "moyen": "Moyen (≤ 280)", "long": "Long (> 280)"})
	v.Compare["media"] = compare(v.Posts, func(p StatPost) string {
		if p.HasMedia {
			return "avec"
		}
		return "sans"
	}, []string{"avec", "sans"}, map[string]string{"avec": "Avec visuel", "sans": "Sans visuel"})
	v.Compare["origin"] = compare(v.Posts, func(p StatPost) string { return p.Origin }, []string{"manuel", "veille", "idée", "campagne"},
		map[string]string{"manuel": "Écrit à la main", "veille": "Tiré de la veille", "idée": "Banque d'idées", "campagne": "Campagne de réponses"})

	v.Followers = []FollowerSeries{}
	for _, a := range d.Accounts {
		if f.Account != "" && a.Name != f.Account {
			continue
		}
		v.Followers = append(v.Followers, followerSeries(d, a, since))
	}
	return v
}

func followerSeries(d *Data, a *Account, since time.Time) FollowerSeries {
	s := FollowerSeries{Account: a.Name, Points: []FollowerPoint{}}
	if !followersSupported(a) {
		s.Unavailable = "indisponible : le backend de ce compte ne renvoie pas le nombre d'abonnés"
		return s
	}
	s.Available = true
	for _, x := range d.Followers {
		if x.Account == a.Name && !x.At.Before(since) {
			s.Points = append(s.Points, FollowerPoint{Day: x.Day, Count: x.Count})
		}
	}
	sort.Slice(s.Points, func(i, j int) bool { return s.Points[i].Day < s.Points[j].Day })
	switch len(s.Points) {
	case 0:
		s.Unavailable = "indisponible : aucun relevé pour l'instant (un relevé par jour)"
	case 1:
	default:
		s.Change = ip(s.Points[len(s.Points)-1].Count - s.Points[0].Count)
	}
	return s
}

// RecordFollowers relève une fois par jour (jour du fuseau du compte) le
// nombre d'abonnés des comptes dont le backend le renvoie. Un nombre nul ou
// une erreur ne produisent aucune ligne : jamais de faux zéro.
func (s *Studio) RecordFollowers(ctx context.Context) (int, error) {
	d := s.store.Snapshot()
	n := 0
	var lastErr error
	for _, a := range d.Accounts {
		if !followersSupported(a) {
			continue
		}
		day := s.now().In(location(a)).Format("2006-01-02")
		done := false
		for _, x := range d.Followers {
			if x.Account == a.Name && x.Day == day {
				done = true
				break
			}
		}
		if done {
			continue
		}
		if t, ok := d.FollowersTriedAt[a.Name]; ok && s.now().Sub(t) < followersRetryGap {
			continue // tentative récente (réussie ou non) : pas de nouvel appel
		}
		// la tentative est mémorisée AVANT l'appel
		name := a.Name
		_ = s.store.Update(func(d *Data) error {
			if d.FollowersTriedAt == nil {
				d.FollowersTriedAt = map[string]time.Time{}
			}
			d.FollowersTriedAt[name] = s.now().UTC()
			return nil
		})
		be, err := s.backendFor(a)
		if err != nil {
			lastErr = err
			continue
		}
		me, err := be.Me(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		if me.FollowersCount <= 0 {
			lastErr = errors.New("nombre d'abonnés non renvoyé par le backend")
			continue
		}
		count := me.FollowersCount
		_ = s.store.Update(func(d *Data) error {
			d.Followers = append(d.Followers, FollowerSample{Account: name, Day: day, Count: count, At: s.now().UTC()})
			cut := s.now().Add(-followersKeepDays * 24 * time.Hour)
			kept := d.Followers[:0]
			for _, x := range d.Followers {
				if !x.At.Before(cut) {
					kept = append(kept, x)
				}
			}
			d.Followers = kept
			return nil
		})
		n++
	}
	return n, lastErr
}
