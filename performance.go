package main

// Apprendre des statistiques (lot 1) : résumé déterministe des performances
// d'un compte. Le code calcule les chiffres (médianes du taux d'engagement par
// groupe de posts) ; l'IA ne fera que proposer des règles appuyées sur ces
// chiffres, et le propriétaire décide. Ce fichier ne fait aucun appel IA et
// aucune écriture.

import (
	"sort"
	"strings"
	"time"
)

// Seuils du lot 1, tous regroupés ici (décision 3 d'Allan).
const (
	// Un relevé fait moins de 48 h après la publication ne reflète pas la
	// carrière du post : l'essentiel des vues et réactions arrive dans ce délai.
	perfMinAge = 48 * time.Hour
	// Sous 50 vues, une seule réaction déplace le taux de 2 points ou plus :
	// le taux d'engagement n'a pas de sens.
	perfMinViews = 50
	// Sous 8 posts mesurés, la médiane du compte, qui sert de référence à tous
	// les signaux, est trop instable pour lancer une analyse.
	perfMinEligible = 8
	// Un groupe doit compter au moins 3 posts : c'est le plus petit échantillon
	// où la médiane écarte un post isolé (un succès viral ou un raté).
	perfMinBucket = 3
	// Un écart de moins de 50 % entre médianes reste dans le bruit d'un petit
	// échantillon : seul un groupe à 1,5 fois la médiane du compte est « mieux ».
	perfRatioHigh = 1.5
	// Seuil symétrique du précédent (1 / 1,5 ≈ 0,67) : un groupe « en retrait »
	// s'écarte autant de la médiane du compte, dans l'autre sens.
	perfRatioLow = 1.0 / 1.5
	// Au plus 3 règles par compte et par analyse : le propriétaire les juge une
	// à une, et une analyse ne s'appuie que sur quelques signaux nets.
	perfMaxProposals = 3
	// Au plus 20 règles de performance en place par compte : au-delà, le bloc
	// <regles> du prompt s'allonge et les règles se diluent ou se contredisent.
	perfMaxAccepted = 20
	// Une règle tient en une phrase applicable à tout brouillon : 240 runes
	// suffisent et bornent la place prise dans le prompt.
	perfMaxRuleChars = 240
)

// PerfSignal : un groupe de posts dont le taux d'engagement médian s'écarte
// nettement de celui du compte. Tous les chiffres sont calculés par le code.
type PerfSignal struct {
	Key      string   `json:"key"`      // "type:utile", "len:long", "kind:reply", "source:veille"
	Label    string   `json:"label"`    // lisible : « type Utile (à enregistrer) »
	N        int      `json:"n"`        // nombre de posts du groupe
	Median   float64  `json:"median"`   // taux d'engagement médian du groupe, en %
	Baseline float64  `json:"baseline"` // médiane du compte, en %
	Ratio    float64  `json:"ratio"`    // Median / Baseline
	PostIDs  []string `json:"postIds"`  // posts du groupe, meilleur taux d'abord
}

// PerfDigest : résumé des performances mesurées d'un compte.
type PerfDigest struct {
	Account  string       `json:"account"`
	Eligible int          `json:"eligible"` // posts publiés et mesurés retenus
	Baseline float64      `json:"baseline"` // taux d'engagement médian du compte, en %
	Signals  []PerfSignal `json:"signals"`  // seulement ceux qui passent les seuils, par clé
	Top      []string     `json:"top"`      // 3 identifiants de posts, meilleur taux d'abord
	Bottom   []string     `json:"bottom"`   // 3 identifiants, pire taux d'abord
}

// perfEligible : post publié pour de vrai, avec un relevé mûr et assez de vues.
func perfEligible(p *Post) bool {
	return p.Status == StatusPublished && !strings.HasPrefix(p.TweetID, "dry-") &&
		p.PublishedAt != nil && p.Metrics != nil && p.Metrics.Views >= perfMinViews &&
		!p.Metrics.UpdatedAt.Before(p.PublishedAt.Add(perfMinAge))
}

// perfLength : classe de longueur du texte publié, en runes.
func perfLength(text string) string {
	switch n := len([]rune(text)); {
	case n <= 140:
		return "court"
	case n <= 280:
		return "moyen"
	}
	return "long"
}

// median : médiane d'une liste non vide (moyenne des deux valeurs centrales
// quand la liste est paire). La liste est triée sur place.
func median(xs []float64) float64 {
	sort.Float64s(xs)
	n := len(xs)
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}

var perfKindLabels = map[string]string{"post": "posts", "reply": "réponses", "quote": "citations"}
var perfLengthLabels = map[string]string{"court": "posts courts (140 caractères au plus)",
	"moyen": "posts moyens (141 à 280 caractères)", "long": "posts longs (plus de 280 caractères)"}
var perfSourceLabels = map[string]string{"veille": "posts tirés de la veille", "idee": "posts sans contenu de veille"}

// ComputePerfDigest résume les performances mesurées d'un compte : médiane du
// taux d'engagement du compte, puis des groupes par type, genre, longueur et
// origine ; ne garde que les groupes assez grands et nettement au-dessus ou
// en dessous. Ne lit que d : aucun appel IA, aucune écriture. now n'entre pas
// dans les règles de calcul du §4.3.1 (l'âge du relevé se compare à la date de
// publication) ; il est gardé pour la signature prévue par la spec.
func ComputePerfDigest(d *Data, account string, now time.Time) PerfDigest {
	// Les comptes se reconnaissent sans tenir compte de la casse, comme dans
	// Data.Account : un brouillon créé pour « Main » appartient au compte « main ».
	if a := d.Account(account); a != nil {
		account = a.Name
	}
	dg := PerfDigest{Account: account, Signals: []PerfSignal{}, Top: []string{}, Bottom: []string{}}
	var posts []*Post
	for _, p := range d.Posts {
		if strings.EqualFold(p.Account, account) && perfEligible(p) {
			posts = append(posts, p)
		}
	}
	dg.Eligible = len(posts)
	if len(posts) == 0 {
		return dg
	}
	// meilleur taux d'abord ; à taux égal, ordre des identifiants (déterministe)
	sort.SliceStable(posts, func(i, j int) bool {
		if ri, rj := posts[i].Metrics.EngagementRate, posts[j].Metrics.EngagementRate; ri != rj {
			return ri > rj
		}
		return posts[i].ID < posts[j].ID
	})
	rates := make([]float64, len(posts))
	for i, p := range posts {
		rates[i] = p.Metrics.EngagementRate
	}
	dg.Baseline = median(rates)
	for i := 0; i < len(posts) && i < 3; i++ {
		dg.Top = append(dg.Top, posts[i].ID)
		dg.Bottom = append(dg.Bottom, posts[len(posts)-1-i].ID)
	}
	if dg.Baseline == 0 {
		return dg
	}

	typeLabels := map[string]string{}
	for _, t := range PostTypesFor(d.Account(account), d.Settings) {
		typeLabels[t.Key] = t.Label
	}
	groups := map[string][]*Post{}
	labels := map[string]string{}
	add := func(key, label string, p *Post) {
		groups[key] = append(groups[key], p)
		labels[key] = label
	}
	for _, p := range posts {
		if p.PostType != "" {
			l := typeLabels[p.PostType]
			if l == "" {
				l = p.PostType
			}
			add("type:"+p.PostType, "type "+l, p)
		}
		if p.Kind != "" {
			l := perfKindLabels[p.Kind]
			if l == "" {
				l = p.Kind
			}
			add("kind:"+p.Kind, l, p)
		}
		ln := perfLength(p.Text)
		add("len:"+ln, perfLengthLabels[ln], p)
		src := "idee"
		if p.SourceItem != "" {
			src = "veille"
		}
		add("source:"+src, perfSourceLabels[src], p)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g := groups[k]
		if len(g) < perfMinBucket {
			continue
		}
		sig := PerfSignal{Key: k, Label: labels[k], N: len(g), Baseline: dg.Baseline}
		rs := make([]float64, len(g))
		for i, p := range g {
			rs[i] = p.Metrics.EngagementRate
			sig.PostIDs = append(sig.PostIDs, p.ID)
		}
		sig.Median = median(rs)
		sig.Ratio = sig.Median / dg.Baseline
		if sig.Ratio >= perfRatioHigh || sig.Ratio <= perfRatioLow {
			dg.Signals = append(dg.Signals, sig)
		}
	}
	return dg
}

// perfAcceptedCount : règles de performance acceptées qui valent pour le
// compte (les siennes et celles sans compte). Les règles tirées des retours
// et celles écrites à la main ne comptent pas (décision 1 d'Allan).
func perfAcceptedCount(d *Data, account string) int {
	n := 0
	for _, r := range d.RuleProposals {
		if r.Source == RuleSourcePerformance && r.Status == RuleAccepted && (r.Account == "" || strings.EqualFold(r.Account, account)) {
			n++
		}
	}
	return n
}
