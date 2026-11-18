package main

// Campagnes de réponses (prospection), d'après les campagnes de commentaires
// de Timon Growth Studio. Une campagne appartient à un compte et décrit des
// « douleurs » de son audience : le planificateur cherche sur X les posts qui
// les expriment, l'IA qualifie chaque candidat et rédige une réponse qui aide
// d'abord. Les réponses arrivent dans l'onglet « Commentaires » et, comme tout
// le reste, ne partent qu'après une validation signée.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultCommentsPrompt : « Consigne des commentaires » par défaut (réglable).
const DefaultCommentsPrompt = `Qualifie un post seulement si son auteur vit l'une des douleurs de la campagne à la première personne, maintenant, et cherche une issue ou s'en plaint.
Écarte-le si la douleur est déjà résolue dans le fil, si c'est une blague, une publicité, une actualité ou une opinion générale, ou s'il ne correspond à aucune douleur.
Au moindre indice que l'auteur appartient à l'équipe d'un concurrent (bio, « on a livré », « notre équipe », compte officiel), écarte-le. Dans le doute, écarte.
La réponse aide d'abord : un conseil concret, utile même sans le produit. Puis une ligne courte et transparente sur la solution, en disant que tu y travailles.
Écris-la dans la voix et la langue du compte, courte et humaine, comme une personne qui répond à une autre. Pas d'URL, pas de hashtag, jamais le nom d'un concurrent, aucune pression à l'achat.`

// Pain : une douleur de l'audience, avec les recherches qui la trouvent et la
// solution vérifiée que la réponse apporte.
type Pain struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`                 // la douleur, avec les mots du lecteur
	Searches   []string `json:"searches"`             // une recherche X par élément
	Matches    []string `json:"matches,omitempty"`    // exemples qui correspondent
	NonMatches []string `json:"nonMatches,omitempty"` // exemples qui ne correspondent pas
	Solution   string   `json:"solution"`             // l'issue concrète que donne la réponse
	VerifiedOn string   `json:"verifiedOn"`           // où la solution a été vérifiée
	Proof      string   `json:"proof,omitempty"`
	Active     bool     `json:"active"`
}

// Campaign : une campagne nommée d'un compte.
type Campaign struct {
	ID              string    `json:"id"`
	Account         string    `json:"account"`
	Name            string    `json:"name"`
	Active          bool      `json:"active"`
	RepliesPerDay   int       `json:"repliesPerDay"`   // défaut 10
	GapMinutes      int       `json:"gapMinutes"`      // minutes entre deux réponses, défaut 15
	AuthorCooldown  int       `json:"authorCooldown"`  // jours avant de répondre de nouveau au même auteur, défaut 7
	SearchesPerDay  int       `json:"searchesPerDay"`  // budget de recherche : recherches X par jour, défaut 12
	Hours           string    `json:"hours,omitempty"` // fenêtre horaire « HH-HH » (vide = toute la journée)
	MaxFollowers    int       `json:"maxFollowers,omitempty"`
	Notes           string    `json:"notes,omitempty"`
	Pains           []Pain    `json:"pains"`
	CreatedAt       time.Time `json:"createdAt"`
	LastRunAt       time.Time `json:"lastRunAt,omitzero"`
	SearchDay       string    `json:"searchDay,omitempty"` // jour (fuseau du compte) du compteur
	SearchesDone    int       `json:"searchesDone,omitempty"`
	LastRunError    string    `json:"lastRunError,omitempty"`
	LastRunSearched int       `json:"lastRunSearched,omitempty"`
}

// Statuts d'un candidat.
const (
	CandNew       = "new"       // trouvé, pas encore qualifié
	CandDrafted   = "drafted"   // qualifié : une réponse attend la validation
	CandDismissed = "dismissed" // écarté par l'IA
)

// Candidate : un post trouvé par une campagne.
type Candidate struct {
	ID         string     `json:"id"`
	CampaignID string     `json:"campaignId"`
	Account    string     `json:"account"`
	PainID     string     `json:"painId,omitempty"`
	TweetID    string     `json:"tweetId"`
	URL        string     `json:"url"`
	Author     string     `json:"author"`
	Followers  int        `json:"followers,omitempty"`
	Text       string     `json:"text"`
	Query      string     `json:"query,omitempty"`
	Status     string     `json:"status"`
	Score      float64    `json:"score,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	PostID     string     `json:"postId,omitempty"`
	FoundAt    time.Time  `json:"foundAt"`
	DraftedAt  *time.Time `json:"draftedAt,omitempty"`
}

func (d *Data) Campaign(id string) *Campaign {
	for _, c := range d.Campaigns {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (d *Data) Candidate(id string) *Candidate {
	for _, c := range d.Candidates {
		if c.ID == id {
			return c
		}
	}
	return nil
}

var hoursRe = regexp.MustCompile(`^([01]?\d|2[0-4])\s*-\s*([01]?\d|2[0-4])$`)

func parseHours(s string) (int, int, bool) {
	m := hoursRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, 24, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return a, b, true
}

func trimList(in []string) []string {
	var out []string
	for _, s := range in {
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				out = append(out, l)
			}
		}
	}
	return out
}

// normalizeCampaign applique les valeurs par défaut et vérifie la campagne.
func normalizeCampaign(c *Campaign) error {
	c.Name, c.Notes, c.Hours = strings.TrimSpace(c.Name), strings.TrimSpace(c.Notes), strings.TrimSpace(c.Hours)
	if c.Name == "" {
		return errors.New("la campagne doit avoir un nom")
	}
	if c.RepliesPerDay <= 0 {
		c.RepliesPerDay = 10
	}
	if c.GapMinutes <= 0 {
		c.GapMinutes = 15
	}
	if c.AuthorCooldown <= 0 {
		c.AuthorCooldown = 7
	}
	if c.SearchesPerDay <= 0 {
		c.SearchesPerDay = 12
	}
	if c.MaxFollowers < 0 {
		c.MaxFollowers = 0
	}
	if c.Hours != "" {
		a, b, ok := parseHours(c.Hours)
		if !ok || a >= b {
			return fmt.Errorf("fenêtre horaire %q invalide : HH-HH, par exemple 08-22", c.Hours)
		}
	}
	for i := range c.Pains {
		p := &c.Pains[i]
		p.Text, p.Solution, p.VerifiedOn, p.Proof = strings.TrimSpace(p.Text), strings.TrimSpace(p.Solution), strings.TrimSpace(p.VerifiedOn), strings.TrimSpace(p.Proof)
		p.Searches, p.Matches, p.NonMatches = trimList(p.Searches), trimList(p.Matches), trimList(p.NonMatches)
		if p.ID == "" {
			p.ID = newID("d")
		}
		name := fmt.Sprintf("douleur n°%d", i+1)
		if p.Text == "" {
			return fmt.Errorf("%s : décrivez la douleur", name)
		}
		if p.Active {
			switch {
			case p.Solution == "":
				return fmt.Errorf("%s : une douleur active a besoin d'une solution", name)
			case p.VerifiedOn == "":
				return fmt.Errorf("%s : indiquez où la solution a été vérifiée", name)
			case len(p.Searches) == 0:
				return fmt.Errorf("%s : ajoutez au moins une recherche X", name)
			}
		}
	}
	return nil
}

// SaveCampaign crée ou met à jour une campagne (les compteurs sont conservés).
func (s *Studio) SaveCampaign(c *Campaign) (*Campaign, error) {
	if err := normalizeCampaign(c); err != nil {
		return nil, err
	}
	err := s.store.Update(func(d *Data) error {
		a := d.Account(c.Account)
		if a == nil {
			return fmt.Errorf("compte %q inconnu", c.Account)
		}
		c.Account = a.Name
		if old := d.Campaign(c.ID); c.ID != "" && old != nil {
			c.CreatedAt, c.LastRunAt, c.SearchDay, c.SearchesDone = old.CreatedAt, old.LastRunAt, old.SearchDay, old.SearchesDone
			c.LastRunError, c.LastRunSearched = old.LastRunError, old.LastRunSearched
			*old = *c
			return nil
		}
		c.ID, c.CreatedAt = newID("c"), s.now().UTC()
		d.Campaigns = append(d.Campaigns, c)
		return nil
	})
	return c, err
}

func (s *Studio) RemoveCampaign(id string) error {
	return s.store.Update(func(d *Data) error {
		for i, c := range d.Campaigns {
			if c.ID == id {
				d.Campaigns = append(d.Campaigns[:i], d.Campaigns[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("campagne %q inconnue", id)
	})
}

// ---------------------------------------------------------------- envoi

// repliesOn : réponses d'une campagne envoyées ou programmées un jour donné.
func repliesOn(d *Data, c *Campaign, day string, loc *time.Location, skipID string) []time.Time {
	var out []time.Time
	for _, p := range d.Posts {
		if p.CampaignID != c.ID || p.ID == skipID {
			continue
		}
		var t *time.Time
		switch p.Status {
		case StatusPublished:
			t = p.PublishedAt
		case StatusScheduled, StatusValidated, StatusPublishing:
			t = p.ScheduledAt
		}
		if t != nil && (day == "" || t.In(loc).Format("2006-01-02") == day) {
			out = append(out, *t)
		}
	}
	return out
}

// SentToday : réponses envoyées aujourd'hui par une campagne.
func SentToday(d *Data, c *Campaign, now time.Time) int {
	a := d.Account(c.Account)
	if a == nil {
		return 0
	}
	loc := location(a)
	day, n := now.In(loc).Format("2006-01-02"), 0
	for _, p := range d.Posts {
		if p.CampaignID == c.ID && p.Status == StatusPublished && p.PublishedAt != nil && p.PublishedAt.In(loc).Format("2006-01-02") == day {
			n++
		}
	}
	return n
}

// nextReplySlot : une réponse validée part au plus tôt, en respectant l'écart
// entre deux réponses, le maximum par jour et la fenêtre horaire de la campagne.
func nextReplySlot(d *Data, a *Account, c *Campaign, after time.Time, skipID string) (time.Time, error) {
	loc := location(a)
	from, to, _ := parseHours(c.Hours)
	gap := time.Duration(c.GapMinutes) * time.Minute
	all := repliesOn(d, c, "", loc, skipID)
	cand := after.Add(time.Minute)
	for i := 0; i < 400; i++ {
		lt := cand.In(loc)
		dayStart := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
		open, close := dayStart.Add(time.Duration(from)*time.Hour), dayStart.Add(time.Duration(to)*time.Hour)
		switch {
		case cand.Before(open):
			cand = open
			continue
		case !cand.Before(close):
			cand = open.AddDate(0, 0, 1)
			continue
		}
		if len(repliesOn(d, c, lt.Format("2006-01-02"), loc, skipID)) >= c.RepliesPerDay {
			cand = open.AddDate(0, 0, 1)
			continue
		}
		moved := false
		for _, t := range all {
			if diff := cand.Sub(t); diff > -gap && diff < gap {
				cand, moved = t.Add(gap), true
			}
		}
		if !moved {
			return cand.UTC(), nil
		}
	}
	return time.Time{}, errors.New("aucune place pour cette réponse dans les jours qui viennent")
}

// ---------------------------------------------------------------- recherche et qualification

var qualifySchema = obj(map[string]any{
	"results": map[string]any{"type": "array", "items": obj(map[string]any{
		"id":      str,
		"qualify": map[string]any{"type": "boolean"},
		"score":   map[string]any{"type": "number"},
		"pain_id": str,
		"reason":  str,
		"reply":   str,
	}, "id", "qualify", "score", "pain_id", "reason", "reply")},
}, "results")

type qualifyOut struct {
	Results []struct {
		ID      string  `json:"id"`
		Qualify bool    `json:"qualify"`
		Score   float64 `json:"score"`
		PainID  string  `json:"pain_id"`
		Reason  string  `json:"reason"`
		Reply   string  `json:"reply"`
	} `json:"results"`
}

// CampaignRun : bilan d'un passage d'une campagne.
type CampaignRun struct {
	Campaign  string `json:"campaign"`
	Name      string `json:"name"`
	Searches  int    `json:"searches"`
	Found     int    `json:"found"`
	Qualified int    `json:"qualified"`
	Dismissed int    `json:"dismissed"`
	Skipped   string `json:"skipped,omitempty"`
	Error     string `json:"error,omitempty"`
}

// RunCampaigns : passage du planificateur sur toutes les campagnes actives.
func (s *Studio) RunCampaigns(ctx context.Context) []CampaignRun {
	var out []CampaignRun
	for _, c := range s.store.Snapshot().Campaigns {
		if !c.Active {
			continue
		}
		if r := s.RunCampaign(ctx, c.ID, false); r.Skipped == "" {
			out = append(out, r)
		}
	}
	return out
}

func authorKey(h string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h), "@"))
}

// RunCampaign cherche les posts des douleurs actives (dans la limite du budget
// du jour), puis fait qualifier les candidats et rédiger les réponses. Hors
// passage forcé, une campagne tourne au plus une fois par heure, dans sa fenêtre.
func (s *Studio) RunCampaign(ctx context.Context, id string, force bool) CampaignRun {
	d := s.store.Snapshot()
	c := d.Campaign(id)
	if c == nil {
		return CampaignRun{Campaign: id, Error: "campagne inconnue"}
	}
	r := CampaignRun{Campaign: c.ID, Name: c.Name}
	a := d.Account(c.Account)
	if a == nil {
		r.Error = fmt.Sprintf("compte %q inconnu", c.Account)
		return r
	}
	now := s.now()
	loc := location(a)
	if !force {
		from, to, _ := parseHours(c.Hours)
		h := now.In(loc).Hour()
		switch {
		case !c.Active:
			r.Skipped = "campagne en pause"
		case h < from || h >= to:
			r.Skipped = "hors de la fenêtre horaire"
		case !c.LastRunAt.IsZero() && now.Sub(c.LastRunAt) < time.Hour:
			r.Skipped = "passage récent"
		}
		if r.Skipped != "" {
			return r
		}
	}
	day := now.In(loc).Format("2006-01-02")
	done := c.SearchesDone
	if c.SearchDay != day {
		done = 0
	}

	// 1. recherche
	be, err := s.backendFor(a)
	if err != nil {
		r.Error = err.Error()
	}
	seen := map[string]bool{}
	recent := map[string]bool{} // auteurs déjà servis pendant la période de repos
	for _, x := range d.Candidates {
		if x.Account == c.Account {
			seen[x.TweetID] = true
		}
		if x.CampaignID == c.ID && x.Status == CandDrafted && x.DraftedAt != nil && now.Sub(*x.DraftedAt) < time.Duration(c.AuthorCooldown)*24*time.Hour {
			recent[authorKey(x.Author)] = true
		}
	}
	var found []*Candidate
	if err == nil {
	search:
		for _, p := range c.Pains {
			if !p.Active {
				continue
			}
			for _, q := range p.Searches {
				if done >= c.SearchesPerDay {
					break search
				}
				ts, err := be.Search(ctx, q, 20)
				done++
				r.Searches++
				if err != nil {
					r.Error = err.Error()
					continue
				}
				for _, t := range ts {
					au := authorKey(t.Author.ScreenName)
					if t.ID == "" || t.IsRetweet || seen[t.ID] || au == authorKey(a.Handle) || recent[au] ||
						(c.MaxFollowers > 0 && t.Author.Followers > c.MaxFollowers) {
						continue
					}
					seen[t.ID] = true
					found = append(found, &Candidate{ID: newID("k"), CampaignID: c.ID, Account: c.Account, PainID: p.ID, TweetID: t.ID,
						URL: fmt.Sprintf("https://x.com/%s/status/%s", t.Author.ScreenName, t.ID), Author: t.Author.ScreenName,
						Followers: t.Author.Followers, Text: t.Text, Query: q, Status: CandNew, FoundAt: now.UTC()})
				}
			}
		}
	}
	r.Found = len(found)
	_ = s.store.Update(func(d *Data) error {
		d.Candidates = append(d.Candidates, found...)
		if len(d.Candidates) > 2000 {
			d.Candidates = d.Candidates[len(d.Candidates)-2000:]
		}
		if x := d.Campaign(c.ID); x != nil {
			x.SearchDay, x.SearchesDone, x.LastRunAt, x.LastRunError, x.LastRunSearched = day, done, now.UTC(), r.Error, r.Searches
		}
		return nil
	})

	// 2. qualification et réponses, par lots de 15
	d = s.store.Snapshot()
	var pending []*Candidate
	for _, x := range d.Candidates {
		if x.CampaignID == c.ID && x.Status == CandNew {
			pending = append(pending, x)
		}
	}
	for start := 0; start < len(pending); start += 15 {
		batch := pending[start:min(start+15, len(pending))]
		q, dis, err := s.qualify(ctx, d, a, c, batch)
		r.Qualified += q
		r.Dismissed += dis
		if err != nil {
			r.Error = err.Error()
			break
		}
	}
	if r.Searches+r.Found+r.Qualified+r.Dismissed > 0 || r.Error != "" {
		_ = s.store.Update(func(d *Data) error {
			d.AddLog(map[bool]string{true: "error", false: "info"}[r.Error != ""], "campagne « %s » : %d recherche(s), %d trouvé(s), %d réponse(s) à valider, %d écarté(s)%s",
				c.Name, r.Searches, r.Found, r.Qualified, r.Dismissed, map[bool]string{true: " ; " + r.Error, false: ""}[r.Error != ""])
			return nil
		})
	}
	return r
}

func campaignBlock(c *Campaign, prompt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<campagne nom=%q>\n", c.Name)
	if c.Notes != "" {
		fmt.Fprintf(&b, "Notes : %s\n", c.Notes)
	}
	for _, p := range c.Pains {
		if !p.Active {
			continue
		}
		fmt.Fprintf(&b, "\n<douleur id=%q>\nDouleur (mots du lecteur) : %s\nSolution vérifiée : %s (vérifiée sur : %s)\n", p.ID, p.Text, p.Solution, p.VerifiedOn)
		if p.Proof != "" {
			fmt.Fprintf(&b, "Preuve : %s\n", p.Proof)
		}
		if len(p.Matches) > 0 {
			fmt.Fprintf(&b, "Correspond :\n- %s\n", strings.Join(p.Matches, "\n- "))
		}
		if len(p.NonMatches) > 0 {
			fmt.Fprintf(&b, "Ne correspond pas :\n- %s\n", strings.Join(p.NonMatches, "\n- "))
		}
		b.WriteString("</douleur>\n")
	}
	b.WriteString("</campagne>\n\n<consigne_des_commentaires>\n" + prompt + "\n</consigne_des_commentaires>\n")
	return b.String()
}

func commentsPrompt(st Settings) string {
	if p := strings.TrimSpace(st.CommentsPrompt); p != "" {
		return p
	}
	return DefaultCommentsPrompt
}

// qualify fait qualifier un lot de candidats ; une réponse est rédigée pour
// chaque candidat retenu (note de 3 sur 5 au moins et douleur reconnue).
func (s *Studio) qualify(ctx context.Context, d *Data, a *Account, c *Campaign, batch []*Candidate) (int, int, error) {
	var u strings.Builder
	u.WriteString("Campagne de réponses du compte. Pour chaque post candidat, décide s'il mérite une réponse (qualify), note de 0 à 5 la certitude qu'il exprime la douleur (score, 3 au moins pour qualifier), indique la douleur reconnue (pain_id, obligatoire pour qualifier), la raison en une ou deux phrases (reason) et, s'il est qualifié, le texte exact de la réponse (reply ; vide sinon).\n\n")
	u.WriteString(campaignBlock(c, commentsPrompt(d.Settings)))
	for _, x := range batch {
		fol := ""
		if x.Followers > 0 {
			fol = fmt.Sprintf(" abonnes=\"%d\"", x.Followers)
		}
		fmt.Fprintf(&u, "\n<contenu_x id=%q auteur=%q%s>\n%s\n</contenu_x>\n", x.ID, x.Author, fol, x.Text)
	}
	var out qualifyOut
	if err := s.aiFor(a).JSON(ctx, systemPrompt(a, d), u.String(), qualifySchema, &out); err != nil {
		return 0, 0, err
	}
	pains := map[string]bool{}
	for _, p := range c.Pains {
		pains[p.ID] = p.Active
	}
	qualified, dismissed := 0, 0
	err := s.store.Update(func(d *Data) error {
		now := s.now().UTC()
		for _, o := range out.Results {
			x := d.Candidate(o.ID)
			if x == nil || x.CampaignID != c.ID || x.Status != CandNew {
				continue
			}
			x.Score, x.Reason = o.Score, strings.TrimSpace(o.Reason)
			reply := strings.TrimSpace(o.Reply)
			if !o.Qualify || o.Score < 3 || !pains[o.PainID] || reply == "" {
				x.Status = CandDismissed
				dismissed++
				continue
			}
			p := s.newPost(a.Name, "reply", reply, x.Reason)
			p.ReplyTo, p.TargetText, p.CampaignID, p.AIText = x.TweetID, x.Text, c.ID, reply
			d.Posts = append(d.Posts, p)
			x.Status, x.PainID, x.PostID, x.DraftedAt = CandDrafted, o.PainID, p.ID, &now
			qualified++
		}
		return nil
	})
	return qualified, dismissed, err
}

// campaignStats : réponses envoyées aujourd'hui et en attente, par campagne.
func campaignStats(d *Data, now time.Time) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, c := range d.Campaigns {
		m := map[string]int{"sentToday": SentToday(d, c, now), "toValidate": 0}
		for _, p := range d.Posts {
			if p.CampaignID == c.ID && p.Status == StatusDraft {
				m["toValidate"]++
			}
		}
		out[c.ID] = m
	}
	return out
}

// sortedCandidates : les plus récents d'abord, limités pour l'interface.
func sortedCandidates(cs []*Candidate, n int) []*Candidate {
	out := append([]*Candidate(nil), cs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].FoundAt.After(out[j].FoundAt) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
