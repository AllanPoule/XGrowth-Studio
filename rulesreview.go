package main

// Révision des règles apprises : échéance de réexamen, exemples conservés,
// doublons et contradictions signalés, comparaison avant/après. Tout ce qui est
// calculé ici est une PROPOSITION ou un constat descriptif : rien ne désactive,
// ne fusionne ni ne réécrit une règle sans une décision du propriétaire.

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	defaultRuleReviewDays = 90  // échéance par défaut (réglage RuleReviewDays)
	maxRuleReviewDays     = 730 // plafond d'une échéance
	maxRuleExamples       = 5   // exemples gardés par règle
	ruleCompareDays       = 30  // fenêtre de comparaison avant / après

	ruleDupThreshold      = 0.6 // similarité minimale pour proposer une fusion
	ruleConflictThreshold = 0.4 // recouvrement minimal (interdit / demandé) pour signaler une contradiction
	ruleAntonymThreshold  = 0.3 // recouvrement minimal quand deux mots s'opposent (court / long…)
)

// RuleExample : un exemple qui justifie une règle, copié au moment où elle est
// proposée pour rester lisible même si le retour ou le post disparaît ensuite.
type RuleExample struct {
	Kind   string `json:"kind"` // retour | post
	Ref    string `json:"ref"`  // identifiant d'origine
	Text   string `json:"text"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// ruleReviewDays : échéance par défaut d'une règle, selon les réglages.
func ruleReviewDays(d *Data) int {
	if n := d.Settings.RuleReviewDays; n > 0 {
		return min(n, maxRuleReviewDays)
	}
	return defaultRuleReviewDays
}

// ruleExamples reconstitue les exemples d'une règle depuis ses preuves
// (retours, puis posts publiés) encore présentes.
func ruleExamples(d *Data, r *RuleProposal) []RuleExample {
	var out []RuleExample
	for _, id := range r.Evidence {
		if len(out) >= maxRuleExamples {
			break
		}
		found := false
		for _, l := range d.Learnings {
			if l.ID == id {
				out = append(out, RuleExample{Kind: "retour", Ref: id, Text: clip(learningLine(l), 400),
					Before: clip(l.Before, 300), After: clip(l.After, 300)})
				found = true
				break
			}
		}
		if found {
			continue
		}
		if p := d.Post(id); p != nil {
			out = append(out, RuleExample{Kind: "post", Ref: id, Text: clip(oneLine(p.Text), 280)})
		}
	}
	return out
}

// ruleBaseAt : date de départ de l'échéance d'une règle en place.
func ruleBaseAt(r *RuleProposal) time.Time {
	if r.DecidedAt != nil {
		return *r.DecidedAt
	}
	return r.CreatedAt
}

// setRuleReview (ré)arme l'échéance d'une règle : days <= 0 reprend l'intervalle
// propre à la règle, sinon celui des réglages.
func setRuleReview(d *Data, r *RuleProposal, from time.Time) {
	days := r.ReviewDays
	if days <= 0 {
		days = ruleReviewDays(d)
	}
	t := from.AddDate(0, 0, days)
	r.ReviewAt = &t
}

// migrateRules : les règles d'avant la version 3 gardent tout ; une règle en
// place reçoit une échéance calculée depuis sa date d'acceptation (à défaut,
// de création) et des exemples reconstitués depuis ses preuves.
func migrateRules(d *Data) {
	for _, r := range d.RuleProposals {
		if r.Status == RuleAccepted && r.ReviewAt == nil {
			setRuleReview(d, r, ruleBaseAt(r))
		}
		if len(r.Examples) == 0 {
			r.Examples = ruleExamples(d, r)
		}
	}
}

// RuleDue : une règle en place dont l'échéance est passée. Signalement seul.
func RuleDue(r *RuleProposal, now time.Time) bool {
	return r.Status == RuleAccepted && r.ReviewAt != nil && !now.Before(*r.ReviewAt)
}

// SetRuleReview règle l'intervalle de réexamen d'une règle (days > 0) ou
// confirme qu'elle a été réexaminée (days == 0 : l'intervalle actuel repart de
// maintenant). Ne touche ni au texte ni au statut.
func (s *Studio) SetRuleReview(id string, days int) (*RuleProposal, error) {
	if days < 0 || days > maxRuleReviewDays {
		return nil, fmt.Errorf("échéance de 1 à %d jours", maxRuleReviewDays)
	}
	var out *RuleProposal
	err := s.store.Update(func(d *Data) error {
		r := d.RuleProposal(id)
		if r == nil {
			return fmt.Errorf("règle %q inconnue", id)
		}
		if r.Status != RuleAccepted {
			return errors.New("seule une règle en place a une échéance de réexamen")
		}
		if days > 0 {
			r.ReviewDays = days
		}
		setRuleReview(d, r, s.now().UTC())
		out = r
		return nil
	})
	return out, err
}

// ---- doublons et contradictions

var ruleStop = map[string]bool{"dans": true, "pour": true, "avec": true, "sans": true, "plus": true, "moins": true, "tous": true, "toutes": true,
	"vous": true, "nous": true, "leur": true, "leurs": true, "cette": true, "ces": true, "chaque": true, "quand": true, "mais": true, "comme": true,
	"être": true, "avoir": true, "faire": true, "jamais": true, "toujours": true, "évite": true, "évitez": true, "éviter": true, "pas": true,
	"posts": true, "post": true, "tweet": true, "tweets": true, "brouillon": true, "brouillons": true}

// ruleTokens : mots significatifs d'une règle, ramenés à un radical grossier.
func ruleTokens(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) < 4 || ruleStop[w] {
			continue
		}
		if r := []rune(w); len(r) > 6 {
			w = string(r[:6])
		}
		out[w] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	if union := len(a) + len(b) - inter; union > 0 {
		return float64(inter) / float64(union)
	}
	return 0
}

// ruleNegative : la règle interdit ou limite (« ne … jamais », « évite »…).
func ruleNegative(text string) bool {
	t := " " + strings.ToLower(text) + " "
	for _, m := range []string{" ne ", " n'", " jamais ", " évite", " eviter ", " sans ", " pas ", " interdit", " arrête", " arrete", " supprime", " retire "} {
		if strings.Contains(t, m) {
			return true
		}
	}
	return false
}

// ruleAntonyms : paires de mots qui s'opposent dans une consigne d'écriture.
var ruleAntonyms = [][2]string{{"court", "long"}, {"courts", "longs"}, {"courte", "longue"}, {"courtes", "longues"}, {"tutoie", "vouvoie"},
	{"tutoiement", "vouvoiement"}, {"formel", "familier"}, {"sérieux", "humoristique"}, {"toujours", "jamais"}, {"ajoute", "supprime"},
	{"ajoute", "retire"}, {"inclus", "exclus"}, {"avec", "sans"}}

func hasWord(text, w string) bool {
	for _, f := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if f == w {
			return true
		}
	}
	return false
}

func antonymSplit(a, b string) bool {
	for _, p := range ruleAntonyms {
		if (hasWord(a, p[0]) && hasWord(b, p[1]) && !hasWord(a, p[1]) && !hasWord(b, p[0])) ||
			(hasWord(a, p[1]) && hasWord(b, p[0]) && !hasWord(a, p[0]) && !hasWord(b, p[1])) {
			return true
		}
	}
	return false
}

// ruleScopesOverlap : les deux règles peuvent valoir pour un même compte.
func ruleScopesOverlap(a, b *RuleProposal) bool {
	return a.Account == "" || b.Account == "" || strings.EqualFold(a.Account, b.Account)
}

// RuleMergeProposal : doublon probable ; la fusion n'a lieu que sur décision.
type RuleMergeProposal struct {
	IDs        []string `json:"ids"`  // la première est conservée
	Text       string   `json:"text"` // texte suggéré (celui de la règle conservée)
	Similarity float64  `json:"similarity"`
}

// RuleConflict : deux règles qui semblent s'opposer. Signalement seul.
type RuleConflict struct {
	IDs    [2]string `json:"ids"`
	Reason string    `json:"reason"`
}

// RuleCompare : résultats des posts avant et après l'acceptation d'une règle.
type RuleCompare struct {
	At        time.Time `json:"at"`
	Days      int       `json:"days"`      // fenêtre avant (et plafond de la fenêtre après)
	AfterDays int       `json:"afterDays"` // jours réellement couverts depuis l'acceptation (au plus Days)
	Before    StatGroup `json:"before"`
	After     StatGroup `json:"after"`
	Note      string    `json:"note"`
}

// RulesReview : tout ce que l'interface affiche pour réviser les règles.
type RulesReview struct {
	Due            []string               `json:"due"` // règles échues
	Duplicates     []RuleMergeProposal    `json:"duplicates"`
	Contradictions []RuleConflict         `json:"contradictions"`
	Compare        map[string]RuleCompare `json:"compare"`
}

const ruleCompareNote = "Comparaison descriptive des posts publiés avant et après l'acceptation de la règle : " +
	"elle ne prouve pas que la règle a un effet (le sujet, la saison, l'audience et le nombre de posts ont pu changer aussi). « Indisponible » n'est pas un zéro."

// ComputeRulesReview : calcul seul, sans appel IA ni écriture.
func ComputeRulesReview(d *Data, now time.Time) RulesReview {
	rv := RulesReview{Due: []string{}, Duplicates: []RuleMergeProposal{}, Contradictions: []RuleConflict{}, Compare: map[string]RuleCompare{}}
	var live []*RuleProposal
	for _, r := range d.RuleProposals {
		if r.Status == RuleAccepted {
			live = append(live, r)
			if RuleDue(r, now) {
				rv.Due = append(rv.Due, r.ID)
			}
			if c, ok := compareRule(d, r, now); ok {
				rv.Compare[r.ID] = c
			}
		}
	}
	toks := make([]map[string]bool, len(live))
	for i, r := range live {
		toks[i] = ruleTokens(r.Text)
	}
	for i := range live {
		for j := i + 1; j < len(live); j++ {
			a, b := live[i], live[j]
			sim := jaccard(toks[i], toks[j])
			switch {
			case strings.EqualFold(a.Account, b.Account) && (sim >= ruleDupThreshold || strings.EqualFold(a.Text, b.Text)):
				keep, other := a, b
				if b.CreatedAt.Before(a.CreatedAt) {
					keep, other = b, a
				}
				rv.Duplicates = append(rv.Duplicates, RuleMergeProposal{IDs: []string{keep.ID, other.ID}, Text: keep.Text, Similarity: sim})
			case ruleScopesOverlap(a, b) && sim >= ruleAntonymThreshold && antonymSplit(a.Text, b.Text):
				rv.Contradictions = append(rv.Contradictions, RuleConflict{IDs: [2]string{a.ID, b.ID}, Reason: "consignes opposées sur le même sujet"})
			case ruleScopesOverlap(a, b) && sim >= ruleConflictThreshold && ruleNegative(a.Text) != ruleNegative(b.Text):
				rv.Contradictions = append(rv.Contradictions, RuleConflict{IDs: [2]string{a.ID, b.ID}, Reason: "l'une interdit ce que l'autre demande"})
			}
		}
	}
	return rv
}

// compareRule décrit les posts publiés dans les ruleCompareDays jours avant et
// après l'acceptation d'une règle, pour les comptes qu'elle concerne.
func compareRule(d *Data, r *RuleProposal, now time.Time) (RuleCompare, bool) {
	if r.DecidedAt == nil {
		return RuleCompare{}, false
	}
	at := *r.DecidedAt
	span := time.Duration(ruleCompareDays) * 24 * time.Hour
	var before, after []StatPost
	for _, p := range d.Posts {
		if p.Status != StatusPublished || p.PublishedAt == nil || (r.Account != "" && !strings.EqualFold(p.Account, r.Account)) {
			continue
		}
		switch t := *p.PublishedAt; {
		case t.Before(at) && !t.Before(at.Add(-span)):
			before = append(before, statPostOf(d, p))
		case !t.Before(at) && t.Before(at.Add(span)) && !t.After(now):
			after = append(after, statPostOf(d, p))
		}
	}
	afterDays := int(min(span, max(0, now.Sub(at))) / (24 * time.Hour))
	return RuleCompare{At: at, Days: ruleCompareDays, AfterDays: afterDays, Before: groupOf("before", "Avant", before), After: groupOf("after", "Après", after), Note: ruleCompareNote}, true
}

// MergeRules fusionne des doublons que le propriétaire a validés : la première
// règle est conservée (texte éventuellement retouché), les autres sont retirées.
// Seules des règles en place d'un même compte sont fusionnées.
func (s *Studio) MergeRules(ids []string, text string) (*RuleProposal, error) {
	if len(ids) < 2 {
		return nil, errors.New("une fusion demande deux règles au moins")
	}
	if text != "" {
		if err := ruleTextOK(text); err != nil {
			return nil, fmt.Errorf("texte de la règle refusé : %v", err)
		}
	}
	var out *RuleProposal
	err := s.store.Update(func(d *Data) error {
		seen := map[string]bool{}
		var rs []*RuleProposal
		for _, id := range ids {
			r := d.RuleProposal(id)
			switch {
			case r == nil:
				return fmt.Errorf("règle %q inconnue", id)
			case seen[id]:
				return fmt.Errorf("règle %q en double dans la demande", id)
			case r.Status != RuleAccepted:
				return errors.New("seules des règles en place se fusionnent")
			case len(rs) > 0 && !strings.EqualFold(r.Account, rs[0].Account):
				return errors.New("des règles de comptes différents ne se fusionnent pas")
			}
			seen[id] = true
			rs = append(rs, r)
		}
		now := s.now().UTC()
		keep := rs[0]
		if t := strings.TrimSpace(text); t != "" {
			keep.Text = t
		}
		have := map[string]bool{}
		for _, id := range keep.Evidence {
			have[id] = true
		}
		for _, o := range rs[1:] {
			for _, id := range o.Evidence {
				if !have[id] {
					have[id] = true
					keep.Evidence = append(keep.Evidence, id)
				}
			}
			keep.Examples = append(keep.Examples, o.Examples...)
			o.Status, o.DecidedAt, o.ReviewAt = RuleRevoked, &now, nil
		}
		if len(keep.Examples) > maxRuleExamples {
			keep.Examples = keep.Examples[:maxRuleExamples]
		}
		setRuleReview(d, keep, now)
		d.AddLog("info", "règles fusionnées sur décision du propriétaire : %d règle(s) en une", len(rs))
		out = keep
		return nil
	})
	return out, err
}
