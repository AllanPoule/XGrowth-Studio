package main

// Écriture, deuxième vague de réglages inspirés de Timon Growth Studio :
// retours sur les corrections manuelles (catégorie, « ne pas apprendre »),
// repères [À COMPLÉTER : …] au lieu de vécu inventé, profil du compte (rôle,
// compte automatisé), IA par compte, banque d'idées et programmation « humaine »
// sur une plage horaire avec des écarts tirés au hasard.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------- retours

// Origine d'un retour (Learning.Source). Vide = leçon d'avant ce champ.
const (
	FeedbackEdit    = "edit"    // correction manuelle d'un texte écrit par l'IA
	FeedbackRewrite = "rewrite" // demande de réécriture
	FeedbackReject  = "reject"  // motif de rejet
	FeedbackManual  = "manual"  // leçon notée à la main
)

// FeedbackCategories : catégories d'un retour, comme dans Timon.
var FeedbackCategories = map[string]string{
	"fond":     "Fond / angle",
	"ton":      "Ton / voix",
	"longueur": "Longueur",
	"fait":     "Erreur factuelle",
	"timing":   "Timing",
	"visuel":   "Visuel",
	"autre":    "Autre",
}

var feedbackLabels = map[string]string{FeedbackEdit: "Correction", FeedbackRewrite: "Réécriture", FeedbackReject: "Rejet", FeedbackManual: "Leçon", "": "Leçon"}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// learningLine : un retour tel que l'IA le lit (corrections : avant → après).
func learningLine(l Learning) string {
	head := feedbackLabels[l.Source]
	if c := FeedbackCategories[l.Category]; c != "" {
		head += " · " + c
	}
	if l.Source == FeedbackEdit && l.Before != "" {
		return fmt.Sprintf("[%s] l'IA avait écrit « %s » ; le propriétaire l'a corrigé en « %s »", head, clip(l.Before, 300), clip(l.After, 300))
	}
	return fmt.Sprintf("[%s] %s", head, l.Text)
}

// recordCorrection garde la dernière correction manuelle d'un texte écrit par
// l'IA comme retour (un seul par post tant qu'il n'est pas synthétisé). Revenir
// au texte de l'IA retire le retour.
func recordCorrection(d *Data, p *Post, now time.Time) {
	if p.AIText == "" {
		return
	}
	same := strings.Join(strings.Fields(p.AIText), " ") == strings.Join(strings.Fields(p.Text), " ")
	for i := range d.Learnings {
		l := &d.Learnings[i]
		if l.Source != FeedbackEdit || l.PostID != p.ID || l.SynthesizedAt != nil {
			continue
		}
		if same {
			d.Learnings = append(d.Learnings[:i], d.Learnings[i+1:]...)
			return
		}
		l.After, l.CreatedAt = p.Text, now
		return
	}
	if same {
		return
	}
	d.Learnings = append(d.Learnings, Learning{ID: newID("l"), Account: p.Account, Source: FeedbackEdit, PostID: p.ID,
		Text: "Correction manuelle du post " + p.ID, Before: p.AIText, After: p.Text, CreatedAt: now})
}

// UpdateLearning change la catégorie d'un retour ou le retire de l'apprentissage.
func (s *Studio) UpdateLearning(id string, category *string, ignored *bool) (*Learning, error) {
	var out Learning
	err := s.store.Update(func(d *Data) error {
		for i := range d.Learnings {
			l := &d.Learnings[i]
			if l.ID != id {
				continue
			}
			if category != nil {
				c := strings.TrimSpace(*category)
				if _, ok := FeedbackCategories[c]; c != "" && !ok {
					return fmt.Errorf("catégorie %q inconnue : fond, ton, longueur, fait, timing, visuel ou autre", c)
				}
				l.Category = c
			}
			if ignored != nil {
				l.Ignored = *ignored
			}
			out = *l
			return nil
		}
		return errors.New("retour inconnu")
	})
	return &out, err
}

// ---------------------------------------------------------------- repères à compléter

// placeholderRe repère « [À COMPLÉTER : …] » (accents et casse indifférents).
var placeholderRe = regexp.MustCompile(`(?i)\[\s*(À|A)\s+COMPL(É|E)TER\b[^\]]*\]`)

// Placeholders renvoie les repères restés dans un texte.
func Placeholders(text string) []string { return placeholderRe.FindAllString(text, -1) }

const placeholderRule = "Ne raconte jamais un vécu, une anecdote, un client, un chiffre ou un résultat que tu ne tiens pas des éléments fournis. " +
	"Si le post en a besoin, écris à la place un repère entre crochets que le propriétaire remplira, par exemple « [À COMPLÉTER : situation réelle vécue côté créateur] » ou « [À COMPLÉTER : chiffre réel] ». " +
	"Un brouillon avec un repère ne peut pas être validé tant qu'il n'est pas rempli : n'en mets que lorsque c'est nécessaire.\n"

// ---------------------------------------------------------------- profil

// Rôles d'un compte : ils changent la façon d'écrire.
var roles = map[string]string{
	"personnel":  "Compte personnel : écris à la première personne du singulier (je), comme son auteur parlerait.",
	"marque":     "Compte de marque ou d'équipe : écris au « nous », sans te faire passer pour une personne en particulier.",
	"automatise": "Compte automatisé, ouvertement identifié comme tel : style factuel et neutre, jamais de « je » qui raconte un vécu.",
}

func roleOf(a *Account) string {
	if _, ok := roles[a.Role]; ok {
		return a.Role
	}
	return "personnel"
}

func profileBlock(a *Account) string {
	var b strings.Builder
	if a.DisplayName != "" {
		fmt.Fprintf(&b, "Nom affiché du compte : %s.\n", a.DisplayName)
	}
	b.WriteString(roles[roleOf(a)] + "\n")
	if a.Automated {
		b.WriteString("Ce compte est ouvertement automatisé : ne prétends jamais être un humain ni avoir vécu quelque chose.\n")
	}
	return b.String()
}

// ---------------------------------------------------------------- IA par compte

// aiFor : l'IA d'un compte (fournisseur et modèle propres, sinon les réglages globaux).
func (s *Studio) aiFor(a *Account) AI {
	st := s.aiSettings()
	if a != nil && a.AIProvider != "" {
		st.AIProvider, st.Model = a.AIProvider, a.AIModel
	} else if a != nil && a.AIModel != "" {
		st.Model = a.AIModel
	}
	return guardedAI{AI: s.ai(st), sem: s.aiSem(), timeout: s.aiTimeout()}
}

func knownProvider(id string) bool {
	for _, p := range providerCatalog {
		if p.ID == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- banque d'idées

// IdeaSettings : la banque d'idées d'un compte (onglet « Idées » de Timon).
type IdeaSettings struct {
	Enabled    bool     `json:"enabled,omitempty"`    // proposer des idées de posts originaux
	Themes     []string `json:"themes,omitempty"`     // sujets des réflexions (vide = sujets du compte)
	PerRequest int      `json:"perRequest,omitempty"` // idées proposées par demande (défaut 5)
	AutoDraft  bool     `json:"autoDraft,omitempty"`  // rédiger directement les idées proposées
}

// Statuts d'une idée.
const (
	IdeaProposed  = "proposed"
	IdeaKept      = "kept"
	IdeaDismissed = "dismissed"
	IdeaDrafted   = "drafted"
)

type Idea struct {
	ID        string     `json:"id"`
	Account   string     `json:"account"`
	Title     string     `json:"title"`
	Angle     string     `json:"angle"`
	Why       string     `json:"why"`
	Status    string     `json:"status"`
	PostID    string     `json:"postId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
}

func (d *Data) Idea(id string) *Idea {
	for _, i := range d.Ideas {
		if i.ID == id {
			return i
		}
	}
	return nil
}

func perRequest(a *Account) int {
	n := a.Ideas.PerRequest
	if n <= 0 {
		n = 5
	}
	return min(n, 12)
}

var ideasSchema = obj(map[string]any{
	"ideas": map[string]any{"type": "array", "items": obj(map[string]any{"title": str, "angle": str, "why": str}, "title", "angle", "why")},
}, "ideas")

// SuggestIdeas fait proposer des idées de posts originaux à l'IA. Avec
// « rédiger directement », chaque idée devient tout de suite un brouillon.
func (s *Studio) SuggestIdeas(ctx context.Context, account string) ([]*Idea, error) {
	a, d, err := s.accountAndData(account)
	if err != nil {
		return nil, err
	}
	if !a.Ideas.Enabled {
		return nil, fmt.Errorf("la banque d'idées est désactivée pour @%s : activez « Proposer des idées de posts originaux » dans sa fiche", a.Handle)
	}
	n := perRequest(a)
	themes := a.Ideas.Themes
	if len(themes) == 0 {
		themes = a.Topics
	}
	var u strings.Builder
	fmt.Fprintf(&u, "Propose %d idées de posts originaux (pas des posts rédigés) : réflexions, constats, analyses ou leçons.\n", n)
	fmt.Fprintf(&u, "\n<consigne>\n%s\n</consigne>\n", PromptFor(a, d.Settings, "ideas"))
	if len(themes) > 0 {
		u.WriteString("\n<sujets>\n" + strings.Join(themes, "\n") + "\n</sujets>\n")
	}
	var known, dismissed []string
	for _, i := range d.Ideas {
		if !strings.EqualFold(i.Account, a.Name) {
			continue
		}
		if i.Status == IdeaDismissed {
			dismissed = append(dismissed, i.Title)
		} else {
			known = append(known, i.Title)
		}
	}
	if len(known) > 0 {
		u.WriteString("\nIdées déjà dans la banque (n'en propose pas de variante) :\n- " + strings.Join(lastN(known, 40), "\n- ") + "\n")
	}
	if len(dismissed) > 0 {
		u.WriteString("\nIdées écartées par le propriétaire (ni elles ni leurs variantes) :\n- " + strings.Join(lastN(dismissed, 40), "\n- ") + "\n")
	}
	u.WriteString("\nPour chaque idée : un titre court (title), l'angle précis à prendre (angle) et pourquoi elle intéressera l'audience du compte (why).")
	var out struct {
		Ideas []struct {
			Title string `json:"title"`
			Angle string `json:"angle"`
			Why   string `json:"why"`
		} `json:"ideas"`
	}
	if err := s.aiFor(a).JSON(ctx, systemPrompt(a, d), u.String(), ideasSchema, &out); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var ideas []*Idea
	for _, o := range out.Ideas {
		if t := strings.TrimSpace(o.Title); t != "" {
			ideas = append(ideas, &Idea{ID: newID("i"), Account: a.Name, Title: t, Angle: strings.TrimSpace(o.Angle), Why: strings.TrimSpace(o.Why), Status: IdeaProposed, CreatedAt: now})
		}
	}
	if len(ideas) > n {
		ideas = ideas[:n]
	}
	if err := s.store.Update(func(d *Data) error { d.Ideas = append(d.Ideas, ideas...); return nil }); err != nil {
		return nil, err
	}
	if a.Ideas.AutoDraft {
		for _, i := range ideas {
			if p, err := s.DraftIdea(ctx, i.ID); err == nil {
				i.Status, i.PostID = IdeaDrafted, p.ID
			}
		}
	}
	return ideas, nil
}

func lastN(s []string, n int) []string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// DecideIdea retient ou écarte une idée proposée.
func (s *Studio) DecideIdea(id, decision string) (*Idea, error) {
	var out *Idea
	err := s.store.Update(func(d *Data) error {
		i := d.Idea(id)
		if i == nil {
			return fmt.Errorf("idée %q inconnue", id)
		}
		if i.Status == IdeaDrafted {
			return errors.New("cette idée est déjà rédigée")
		}
		switch decision {
		case "keep":
			i.Status = IdeaKept
		case "dismiss":
			i.Status = IdeaDismissed
		default:
			return fmt.Errorf("décision %q inconnue : keep ou dismiss", decision)
		}
		now := s.now().UTC()
		i.DecidedAt, out = &now, i
		return nil
	})
	return out, err
}

// DraftIdea rédige le post d'une idée retenue (ou proposée) ; il arrive « à valider ».
func (s *Studio) DraftIdea(ctx context.Context, id string) (*Post, error) {
	d := s.store.Snapshot()
	i := d.Idea(id)
	if i == nil {
		return nil, fmt.Errorf("idée %q inconnue", id)
	}
	if i.Status == IdeaDrafted || i.Status == IdeaDismissed {
		return nil, fmt.Errorf("idée %s : déjà %s", id, map[string]string{IdeaDrafted: "rédigée", IdeaDismissed: "écartée"}[i.Status])
	}
	a := d.Account(i.Account)
	if a == nil {
		return nil, fmt.Errorf("compte %q inconnu", i.Account)
	}
	outs, err := GenerateDrafts(ctx, s.aiFor(a), a, d, DraftRequest{Idea: i, N: 1, Now: s.now()})
	if err != nil {
		return nil, err
	}
	if len(outs) == 0 {
		return nil, errors.New("l'IA n'a rien rédigé")
	}
	o := outs[0]
	p := s.newPost(a, "post", o.Text, o.Rationale)
	p.PostType, p.VisualBrief, p.AIText, p.IdeaID = o.Type, strings.TrimSpace(o.Visual), p.Text, i.ID
	err = s.store.Update(func(d *Data) error {
		x := d.Idea(id)
		if x == nil {
			return fmt.Errorf("idée %q inconnue", id)
		}
		now := s.now().UTC()
		x.Status, x.PostID, x.DecidedAt = IdeaDrafted, p.ID, &now
		d.Posts = append(d.Posts, p)
		return nil
	})
	return p, err
}

// ---------------------------------------------------------------- programmation humaine

// Timing : publication sur une plage horaire, avec des écarts tirés au hasard
// (mode « window »). Vide ou « slots » : les créneaux fixes du compte.
type Timing struct {
	Mode   string `json:"mode,omitempty"`   // slots | window
	Start  string `json:"start,omitempty"`  // début de la plage (défaut 08:00)
	End    string `json:"end,omitempty"`    // fin de la plage (défaut 22:00)
	GapMin int    `json:"gapMin,omitempty"` // écart minimum entre deux posts (défaut 30 min)
	GapMax int    `json:"gapMax,omitempty"` // écart maximum (défaut 60 min)
	Jitter *int   `json:"jitter,omitempty"` // aléa du premier post du jour, ± minutes (vide = 15, 0 = aucun)
}

func normalizeTiming(t *Timing) error {
	switch t.Mode {
	case "", "slots":
		return nil
	case "window":
	default:
		return fmt.Errorf("programmation %q inconnue : slots ou window", t.Mode)
	}
	if t.Start == "" {
		t.Start = "08:00"
	}
	if t.End == "" {
		t.End = "22:00"
	}
	if t.GapMin <= 0 {
		t.GapMin = 30
	}
	if t.GapMax <= 0 {
		t.GapMax = max(60, t.GapMin)
	}
	if t.Jitter == nil {
		j := 15
		t.Jitter = &j
	}
	if *t.Jitter < 0 || *t.Jitter > 120 {
		return errors.New("aléa du premier post : entre 0 et 120 minutes")
	}
	if !slotRe.MatchString(t.Start) || !slotRe.MatchString(t.End) {
		return errors.New("plage de publication : format HH:MM")
	}
	if t.End <= t.Start {
		return errors.New("plage de publication : la fin doit suivre le début")
	}
	if t.GapMax < t.GapMin {
		return errors.New("écart maximum inférieur à l'écart minimum")
	}
	return nil
}

// randIntn est remplacé dans les tests pour rendre les tirages prévisibles.
var randIntn = func(n int) int { return rand.IntN(n) }

func randBetween(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + randIntn(hi-lo+1)
}

func atHM(day time.Time, hm string, loc *time.Location) time.Time {
	var h, m int
	fmt.Sscanf(hm, "%d:%d", &h, &m)
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc)
}

// nextWindowSlot : un post validé sans date se cale après le dernier post du
// jour avec un écart tiré au hasard ; sans post ce jour-là, tout de suite si
// on est dans la plage, sinon au début de la plage à ± l'aléa.
func nextWindowSlot(d *Data, a *Account, after time.Time, skipID string) (time.Time, error) {
	t := a.Timing
	if err := normalizeTiming(&t); err != nil {
		return time.Time{}, err
	}
	loc := location(a)
	var taken []time.Time
	for _, p := range d.Posts {
		// les réponses de campagne ont leur propre rythme
		if p.Account != a.Name || p.ID == skipID || p.CampaignID != "" {
			continue
		}
		switch {
		case (p.Status == StatusScheduled || p.Status == StatusValidated || p.Status == StatusPublishing || p.Status == StatusManual) && p.ScheduledAt != nil:
			taken = append(taken, *p.ScheduledAt)
		case p.Status == StatusPublished && p.PublishedAt != nil:
			taken = append(taken, *p.PublishedAt)
		}
	}
	capTimes := weekPostTimes(d, a, skipID)
	soon := after.Add(time.Minute)
	base := after.In(loc)
	for day := 0; day < 60; day++ {
		dd := base.AddDate(0, 0, day)
		start, end := atHM(dd, t.Start, loc), atHM(dd, t.End, loc)
		dayStart := time.Date(dd.Year(), dd.Month(), dd.Day(), 0, 0, 0, 0, loc)
		var last time.Time
		n := 0
		for _, x := range taken {
			if !x.Before(dayStart) && x.Before(dayStart.AddDate(0, 0, 1)) {
				n++
				if x.After(last) {
					last = x
				}
			}
		}
		if a.DailyLimit > 0 && n >= a.DailyLimit {
			continue
		}
		if weekFull(a, capTimes, dd) {
			continue // plafond hebdomadaire atteint : semaine suivante
		}
		var cand time.Time
		switch {
		case n > 0:
			cand = last.Add(time.Duration(randBetween(t.GapMin, t.GapMax)) * time.Minute)
		case !soon.Before(start) && soon.Before(end):
			cand = soon
		default:
			off := randBetween(-*t.Jitter, *t.Jitter)
			if off < 0 {
				off = -off // la plage reste respectée : l'aléa se reporte après son début
			}
			cand = start.Add(time.Duration(off) * time.Minute)
		}
		if cand.Before(soon) {
			cand = soon
		}
		if cand.Before(start) {
			cand = start
		}
		if !cand.After(end) {
			return cand.UTC(), nil
		}
	}
	return time.Time{}, errors.New("aucune place dans la plage de publication des 60 prochains jours")
}

// normalizeWriting valide le profil, l'IA, la banque d'idées et la programmation d'un compte.
func normalizeWriting(a *Account) error {
	switch a.Role = strings.TrimSpace(a.Role); a.Role {
	case "", "personnel", "marque", "automatise":
	default:
		return fmt.Errorf("rôle %q inconnu : personnel, marque ou automatise", a.Role)
	}
	if a.Role == "automatise" {
		a.Automated = true
	}
	a.AIProvider, a.AIModel = strings.TrimSpace(a.AIProvider), strings.TrimSpace(a.AIModel)
	if a.AIProvider != "" && !knownProvider(a.AIProvider) {
		return fmt.Errorf("fournisseur d'IA %q inconnu", a.AIProvider)
	}
	var themes []string
	for _, t := range a.Ideas.Themes {
		themes = append(themes, SplitLines(t)...)
	}
	a.Ideas.Themes = themes
	if a.Ideas.PerRequest < 0 || a.Ideas.PerRequest > 12 {
		return errors.New("idées proposées par demande : entre 1 et 12")
	}
	return normalizeTiming(&a.Timing)
}
