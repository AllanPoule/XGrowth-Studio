package main

// LinkedIn, lot 3 : la méthode d'écriture (aides) et le contrôle anti-tics.
//
// Méthode : consignes réécrites en français à partir de linkedin-skills
// (https://github.com/sergebulaev/linkedin-skills, commit 32083c0, licence MIT,
// voir THIRD_PARTY_NOTICES.md) : Post Writer (accroches), Humanizer, Post Audit
// et Content Planner (plan 7 jours). Aucun fichier du dépôt n'est copié, aucun
// de ses scripts n'est appelé. La voix du compte l'emporte TOUJOURS : la méthode
// est une aide, jamais un moule.
//
// Règles de sûreté, valables pour tout ce fichier :
//   - une aide ne se lance que sur demande (bouton, commande) : jamais depuis
//     Tick, une campagne, une actualité ou un brouillon automatique ;
//   - Accroches, Audit et Plan 7 jours ne modifient aucun texte ; l'Humanizer
//     ne propose qu'une version, appliquée seulement par ApplyHumanized ;
//   - le contrôle anti-tics est déterministe (aucun appel IA).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---------------------------------------------------------------- anti-tics

// Sévérités d'un tic. Seul un tic « haute » bloque la validation tant qu'il
// n'est pas accepté explicitement (ackTics).
const (
	TicHigh   = "haute"
	TicMedium = "moyenne"
)

// Tic : une formulation typique d'un texte écrit par une IA.
type Tic struct {
	Rule     string `json:"rule"`
	Excerpt  string `json:"excerpt"`
	Severity string `json:"severity"`
}

type ticPattern struct {
	rule string
	sev  string
	re   *regexp.Regexp
}

// wordBounds remplace \b, qui en Go ne connaît que l'ASCII (« à », « é » ne
// sont pas des lettres pour lui) : un \b en tête devient un début de mot, tout
// autre \b une fin de mot, au sens Unicode.
func wordBounds(expr string) string {
	const lead, trail = `(?:^|[^\p{L}\p{N}_])`, `(?:[^\p{L}\p{N}_]|$)`
	if strings.HasPrefix(expr, `\b`) {
		return lead + strings.ReplaceAll(expr[2:], `\b`, trail)
	}
	return strings.ReplaceAll(expr, `\b`, trail)
}

func ticRe(rule, sev, expr string) ticPattern {
	return ticPattern{rule: rule, sev: sev, re: regexp.MustCompile(`(?i)` + wordBounds(expr))}
}

// adapté de linkedin-skills : skills/linkedin-humanizer/references/audit-ai-tells.md@32083c0
// Motifs à une occurrence. Liste de départ du studio, complétée par les motifs
// d'IA du dépôt linkedin-skills (ponts de révélation, parallélisme négatif,
// annonces d'enthousiasme, clôtures-appâts, candeur annoncée), en français.
var ticPatterns = []ticPattern{
	ticRe("contraste", TicHigh, `ce n['’]est pas [^.!?\n]{1,80}?[,;:.!?—–-]\s*(?:mais\s+)?c['’]est\b`),
	ticRe("contraste", TicHigh, `il ne s['’]agit pas (?:de|d['’]|du|des)[^.!?\n]{1,80}?[,;]\s*mais\b`),
	ticRe("contraste", TicHigh, `\bnon pas [^.!?\n]{1,60}?,?\s+mais\b`),
	ticRe("ouverture-creuse", TicHigh, `\bdans un monde où\b`),
	ticRe("ouverture-creuse", TicHigh, `\bdans le monde d['’]aujourd['’]hui\b`),
	ticRe("ouverture-creuse", TicHigh, `\bà l['’](?:heure|ère) (?:où|de l['’]ia)\b`),
	ticRe("ouverture-creuse", TicHigh, `\bvoici (?:pourquoi|comment|ce que|la chose)\b`),
	ticRe("ouverture-creuse", TicHigh, `\bplongeons\b`),
	ticRe("ouverture-creuse", TicHigh, `\bla plupart des gens (?:ne )?(?:réalisent|comprennent|savent|voient)\b`),
	ticRe("annonce", TicHigh, `\bspoiler\b`),
	ticRe("annonce", TicHigh, `\blisez jusqu['’]au bout\b`),
	ticRe("annonce", TicHigh, `\brestez jusqu['’]à la fin\b`),
	ticRe("annonce", TicHigh, `\bplot twist\b`),
	ticRe("annonce", TicHigh, `🧵`),
	ticRe("annonce", TicHigh, `(?:^|[.!?\n]\s*)(?:le résultat|le piège|le hic|la suite|le secret|la raison)\s*\?`),
	ticRe("enthousiasme-creux", TicHigh, `\b(?:ravi|ravie|fier|fière|heureux|heureuse|enchanté|enchantée|excité|excitée) (?:de partager|d['’]annoncer|de vous annoncer|de vous présenter)\b`),
	ticRe("enthousiasme-creux", TicHigh, `\bj['’]ai le plaisir d['’]annoncer\b`),
	ticRe("enthousiasme-creux", TicHigh, `\bje suis (?:ravi|ravie|super excité|super excitée|tellement excité|tellement excitée)\b`),
	ticRe("sincerite-annoncee", TicMedium, `\b(?:soyons honnêtes|je vais être honnête|honnêtement ?\?|franchement ?\?|opinion impopulaire|la vérité qui dérange|la dure réalité|pour être direct|sans filtre :)`),
	ticRe("formule-ia", TicMedium, `\b(?:game[- ]changer|changer la donne|plongée en profondeur|tirer parti de|libérer le potentiel|à l['’]ère de l['’]ia)\b`),
	ticRe("x-lecons", TicMedium, `\b\d+\s+(?:leçons|conseils|erreurs|astuces|règles|secrets|clés)\s+(?:que|qui|pour|à|sur|de|d['’])`),
}

// Clôtures-appâts : cherchées dans la fin du texte seulement.
var ticCloseRe = regexp.MustCompile(`(?i)(?:et vous ?\?|qu['’]en pensez-vous ?\?|vous en pensez quoi ?\?|ça vous parle ?\?|dites-moi (?:en commentaire|ce que vous en pensez)|commentez|taguez|abonnez-vous|likez|partagez (?:si|ce post)|thoughts ?\?|vos retours ?\?|d['’]accord ou pas ?\?)`)

var (
	ticHashtagRe = regexp.MustCompile(`#\p{L}[\p{L}\p{N}_]*`)
	ticBulletRe  = regexp.MustCompile(`^\s*(?:[-•*–▪◦●]|\d{1,2}[.)])\s+\S`)
	ticSplitRe   = regexp.MustCompile(`[.!?…\n]+`)
	// grappes de vocabulaire corporate : 3 dans un même paragraphe
	ticVocabRe = regexp.MustCompile(`(?i)` + wordBounds(`\b(?:crucial(?:e|es|aux)?|incontournables?|robustes?|holistiques?|écosystèmes?|synergies?|levier|leviers|booster|dynamiser|optimiser|transformationnel(?:le)?s?|multifacettes?|nuancé(?:e|es|s)?|en profondeur|paysage|potentiel)\b`))
)

func ticExcerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:80]) + "…"
	}
	return s
}

func isEmojiRune(r rune) bool {
	return r >= 0x1F000 || (r >= 0x2190 && r <= 0x21FF) || (r >= 0x2300 && r <= 0x23FF) || (r >= 0x2600 && r <= 0x27BF) || (r >= 0x2B00 && r <= 0x2BFF) || r == 0x2705
}

// TicCheck repère, sans appel IA, les formulations typiques d'un texte d'IA.
// Un texte court et net, sans motif, renvoie une liste vide.
func TicCheck(text string) []Tic {
	var out []Tic
	t := strings.TrimSpace(text)
	if t == "" {
		return out
	}
	seen := map[string]bool{}
	add := func(rule, sev, excerpt string) {
		key := rule + "|" + excerpt
		if !seen[key] {
			seen[key] = true
			out = append(out, Tic{Rule: rule, Excerpt: ticExcerpt(excerpt), Severity: sev})
		}
	}

	// tirets longs répétés : un seul est tolérable, deux sont une habitude
	dashes := strings.Count(t, "—") + strings.Count(t, " – ") + strings.Count(t, " -- ")
	if dashes >= 2 {
		i := strings.LastIndex(t, "—")
		if i < 0 {
			i = 0
		}
		add("tirets-longs", TicMedium, excerptAround(t, i))
	}

	for _, p := range ticPatterns {
		if m := p.re.FindString(t); m != "" {
			add(p.rule, p.sev, m)
		}
	}

	// clôture-appât : dans les 200 derniers caractères
	tail := t
	if r := []rune(t); len(r) > 200 {
		tail = string(r[len(r)-200:])
	}
	if m := ticCloseRe.FindString(tail); m != "" {
		add("cloture-appat", TicHigh, m)
	}

	if tags := ticHashtagRe.FindAllString(t, -1); len(tags) > 3 {
		add("hashtags", TicMedium, strings.Join(tags, " "))
	}

	lines := strings.Split(t, "\n")
	// listes à puces (3 lignes de suite) et emojis en puces (2 lignes)
	run, emojiRun := 0, 0
	listDone, emojiDone := false, false
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if ticBulletRe.MatchString(ln) {
			run++
		} else {
			run = 0
		}
		if run >= 3 && !listDone {
			listDone = true
			add("liste-trois", TicMedium, trim)
		}
		if r, _ := utf8.DecodeRuneInString(trim); trim != "" && isEmojiRune(r) {
			emojiRun++
		} else if trim != "" {
			emojiRun = 0
		}
		if emojiRun >= 2 && !emojiDone {
			emojiDone = true
			add("emojis-puces", TicMedium, trim)
		}
	}

	// rafale : trois phrases ou lignes très courtes à la suite
	var frag []string
	short := 0
	for _, f := range ticSplitRe.Split(t, -1) {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		w := len(strings.Fields(f))
		if w >= 1 && w <= 4 && !strings.HasPrefix(f, "#") && !ticBulletRe.MatchString(f) {
			frag = append(frag, f)
			short++
			if short == 3 {
				add("rafale-courte", TicMedium, strings.Join(frag[len(frag)-3:], ". "))
			}
			continue
		}
		short, frag = 0, nil
	}

	// accroche creuse : première ligne en question ou tout en majuscules
	first := strings.TrimSpace(lines[0])
	if strings.HasSuffix(first, "?") {
		add("accroche-creuse", TicMedium, first)
	} else if letters := countLetters(first); letters >= 12 && first == strings.ToUpper(first) {
		add("accroche-creuse", TicMedium, first)
	}

	// vocabulaire corporate en grappe : 3 marqueurs dans un même paragraphe
	for _, para := range strings.Split(t, "\n\n") {
		if hits := ticVocabRe.FindAllString(para, -1); len(hits) >= 3 {
			add("vocabulaire-ia", TicMedium, strings.Join(hits, ", "))
			break
		}
	}
	return out
}

func countLetters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

func excerptAround(t string, byteIdx int) string {
	r := []rune(t[:min(byteIdx, len(t))])
	start := max(0, len(r)-40)
	rest := []rune(t[min(byteIdx, len(t)):])
	return string(r[start:]) + string(rest[:min(40, len(rest))])
}

// highTics : les tics qui bloquent la validation sans accusé explicite.
func highTics(tics []Tic) []Tic {
	var out []Tic
	for _, t := range tics {
		if t.Severity == TicHigh {
			out = append(out, t)
		}
	}
	return out
}

// textHash : empreinte du texte courant, pour savoir si le contrôle porte
// toujours sur lui.
func textHash(text string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(h[:])
}

func ticSummary(tics []Tic) string {
	var parts []string
	for _, t := range tics {
		parts = append(parts, fmt.Sprintf("%s (« %s »)", t.Rule, t.Excerpt))
	}
	return strings.Join(parts, " ; ")
}

var errNotLinkedIn = errors.New("fonction réservée aux comptes LinkedIn")

// CheckTics lance le contrôle anti-tics sur le texte courant d'un brouillon
// LinkedIn et l'enregistre (empreinte du texte). Le studio le lance au moment
// de la validation et en affiche le résultat avant confirmation.
func (s *Studio) CheckTics(id string) (*Post, error) {
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if !isLinkedIn(d.Account(p.Account)) {
			return errNotLinkedIn
		}
		if p.Status != StatusDraft && p.Status != StatusFailed && p.Status != StatusRejected {
			return fmt.Errorf("le post %s est %s : le contrôle se lance avant la validation", id, p.Status)
		}
		t := s.now().UTC()
		p.Tics = TicCheck(p.Text)
		p.TicCheckedHash, p.TicCheckedAt, p.TicsAcknowledged = textHash(p.Text), &t, false
		p.UpdatedAt = t
		out = p
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------- méthode : prompts

// liMethodClause précède toute consigne de méthode : la voix du compte prime.
func liMethodClause(a *Account) string {
	return fmt.Sprintf("La voix du compte l'emporte, toujours : la voix de %s l'emporte sur ce qui suit. "+
		"Ce qui suit est une aide, jamais un moule ; si une consigne contredit la voix du compte, ignore-la. "+
		"Reste court, une seule idée, pas de structure en trois points, pas de liste à puces numérotée, "+
		"pas de « X leçons que j'ai apprises ».", a.Name)
}

// Consignes de méthode, réécrites en français (voir l'en-tête du fichier).
// adapté de linkedin-skills : linkedin-post-writer, linkedin-humanizer (+ sub-skills/post-audit.md),
// linkedin-content-planner @32083c0
const (
	liHooksMethod = `Propose 3 premières lignes alternatives pour ce brouillon (accroches).
Une bonne accroche tient seule : elle se comprend sans la ligne suivante, tient en 210 caractères au plus (le reste se cache derrière « voir plus » sur mobile) et annonce l'idée du post par une affirmation ou un fait concret.
Si le brouillon contient un chiffre ou un fait précis, tu peux ouvrir avec ; sinon n'en invente aucun.
Jamais de question en première ligne, jamais de majuscules criées, jamais « Voici comment », « Arrête de X, fais Y » ni « Le résultat ? », jamais d'annonce d'enthousiasme.
Chaque accroche remplace la première ligne du brouillon : elle doit se raccorder à la suite sans la répéter. Donne trois angles réellement différents, une seule ligne chacune.`

	liHumanizerMethod = `Réécris ce brouillon en retirant ce que les lecteurs repèrent comme écrit par une IA : plus d'un tiret long, ponts de révélation (« Le résultat ? »), contraste « ce n'est pas X, c'est Y », clôtures qui mendient une réaction, vocabulaire corporate en grappe, fragments empilés, énumérations en rafale.
Règles de prudence : ne rien ajouter qui ne soit déjà dans le brouillon (ni fait, ni chiffre, ni confession, ni hésitation) ; ne pas aplatir la voix : garde les tournures personnelles et ce qui fait la voix du compte ; des retouches proportionnées (un texte déjà net reste presque identique).
Rends le texte réécrit (text) et la liste précise de ce que tu as changé (changes), une phrase par changement. Si rien ne mérite d'être changé, rends le texte à l'identique et une liste vide.`

	liAuditMethod = `Audite ce brouillon avant validation et rends des suggestions (suggestions), sans le réécrire.
Passe en revue : l'accroche (complète, tient dans 210 caractères, pas de question en ouverture) ; la longueur (par rapport à la cible du compte) ; la clarté (une seule idée, paragraphes courts, ligne vide après l'accroche) ; les tics d'IA ; un lien externe dans le corps (à mettre en commentaire) ; plus de deux hashtags ; un ton de communiqué au lieu d'une voix personnelle.
Chaque suggestion nomme son domaine (area : accroche, longueur, clarté, tics ou autre) et dit quoi faire en une phrase. Ne signale que de vrais problèmes : un texte net a peu de suggestions, voire aucune. Ce rapport est informatif : il ne bloque rien.`

	liPlanMethod = `Propose un plan de 7 jours sous forme d'IDÉES de posts (jamais de posts rédigés, jamais de dates).
Varie les registres sur la semaine : ce que le compte sait faire ou a appris (autorité), ce qui s'est passé ou raté (récit), ce qui touche l'audience (communauté). Pas deux fois le même ressort d'accroche dans la semaine. Un jour peut rester sans post : le nombre de posts de la semaine est la cadence réglée par Allan, jamais une obligation.
Pour chaque idée : un titre court (title), l'angle précis (angle) et pourquoi elle intéressera l'audience (why), qui commence par « Jour N : » (N de 1 à 7). N'invente ni chiffre, ni vécu, ni fait que le compte ne t'a pas donné.`

	// Guide de visuel par défaut sur LinkedIn : un visuel est toujours proposé.
	liVisualGuide = "Une idée de visuel sobre qui accompagne le post sans le répéter : photo prise sur le vif, capture, schéma simple ou citation sur fond uni. Pas de visuel décoratif ni de banque d'images générique."

	// Aucune promotion croisée entre les lignes d'Allan.
	liNoCrossPromo = "Aucune promotion croisée : ne nomme jamais les autres lignes éditoriales d'Allan (%s) ni les marques ou projets qui leur sont attachés ; reste dans l'univers de la ligne %s."
)

func liCrossPromoLine(a *Account) string {
	var others []string
	for _, l := range linkedinLines {
		if l != a.Line {
			others = append(others, l)
		}
	}
	return fmt.Sprintf(liNoCrossPromo, strings.Join(others, ", "), a.Line)
}

// liMethodSystem : prompt système d'une aide (voix et règles du compte).
func liMethodSystem(a *Account, d *Data) string { return systemPrompt(a, d) }

// liMethodUser assemble la demande d'une aide : la clause d'abord, la consigne
// de méthode ensuite, puis le brouillon.
func liMethodUser(a *Account, method string, p *Post) string {
	var u strings.Builder
	u.WriteString(liMethodClause(a) + "\n\n<methode>\n" + method + "\n</methode>\n")
	if p != nil {
		u.WriteString("\n<brouillon>\n" + p.Text + "\n</brouillon>\n")
	}
	return u.String()
}

// ---------------------------------------------------------------- aides (sur demande)

var (
	hooksSchema     = obj(map[string]any{"hooks": map[string]any{"type": "array", "items": str}}, "hooks")
	humanizerSchema = obj(map[string]any{"text": str, "changes": map[string]any{"type": "array", "items": str}}, "text", "changes")
	auditSchema     = obj(map[string]any{"suggestions": map[string]any{"type": "array", "items": obj(map[string]any{"area": str, "advice": str}, "area", "advice")}}, "suggestions")
)

// linkedinPost : le brouillon et son compte, pour une aide (LinkedIn seulement).
func (s *Studio) linkedinPost(id string) (*Account, *Data, *Post, error) {
	d := s.store.Snapshot()
	p := d.Post(id)
	if p == nil {
		return nil, nil, nil, fmt.Errorf("post %q inconnu", id)
	}
	a := d.Account(p.Account)
	if !isLinkedIn(a) {
		return nil, nil, nil, errNotLinkedIn
	}
	return a, d, p, nil
}

// oneLineClean : une seule ligne, sans caractère invisible.
func oneLineClean(s string) string {
	s = strings.Map(func(r rune) rune {
		if invisibleRune(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// hookOK : une accroche venue de l'IA tient en une ligne, sans balise HTML ni
// lien (elle remplace la première ligne d'un post public).
func hookOK(h string) bool {
	return h != "" && utf8.RuneCountInString(h) <= 210 && !strings.ContainsAny(h, "<>") && !strings.Contains(strings.ToLower(h), "http")
}

// SuggestHooks propose 3 premières lignes alternatives. Rien n'est modifié.
func (s *Studio) SuggestHooks(ctx context.Context, id string) ([]string, error) {
	a, d, p, err := s.linkedinPost(id)
	if err != nil {
		return nil, err
	}
	if err := editable(p); err != nil {
		return nil, err
	}
	var out struct {
		Hooks []string `json:"hooks"`
	}
	if err := s.aiFor(a).JSON(ctx, liMethodSystem(a, d), liMethodUser(a, liHooksMethod, p), hooksSchema, &out); err != nil {
		return nil, err
	}
	var hooks []string
	for _, h := range out.Hooks {
		if h = oneLineClean(h); hookOK(h) {
			hooks = append(hooks, h)
		}
	}
	if len(hooks) > 3 {
		hooks = hooks[:3]
	}
	return hooks, nil
}

// firstLineEnd : fin de la première ligne du texte ; à défaut de saut de ligne,
// fin de la première phrase ; -1 si le texte n'en a qu'une.
func firstLineEnd(text string) int {
	if i := strings.Index(text, "\n"); i >= 0 {
		return i
	}
	for i, r := range text {
		if (r == '.' || r == '!' || r == '?') && i+1 < len(text) && text[i+1] == ' ' {
			return i + 1
		}
	}
	return -1
}

// ApplyHook remplace la première ligne du brouillon par l'accroche choisie
// (clic « Appliquer »). La validation et le contrôle anti-tics repartent à zéro.
func (s *Studio) ApplyHook(id, hook string) (*Post, error) {
	hook = oneLineClean(hook)
	if hook == "" {
		return nil, errors.New("accroche vide")
	}
	if !hookOK(hook) {
		return nil, errors.New("accroche refusée : 210 caractères au plus, sans balise ni lien")
	}
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if !isLinkedIn(d.Account(p.Account)) {
			return errNotLinkedIn
		}
		if err := editable(p); err != nil {
			return err
		}
		end := firstLineEnd(p.Text)
		if end < 0 {
			return errors.New("le post n'a qu'une ligne : réécrivez-le plutôt que d'en changer l'accroche")
		}
		now := s.now().UTC()
		p.History = append(p.History, Revision{Text: p.Text, Feedback: "accroche appliquée", At: now})
		p.Text = strings.TrimSpace(hook + p.Text[end:])
		resetValidation(p)
		resetTicCheck(p)
		p.UpdatedAt = now
		out = p
		return nil
	})
	return out, err
}

// HumanizeProposal : la version proposée par l'Humanizer. Rien n'est appliqué.
type HumanizeProposal struct {
	Text    string   `json:"text"`
	Changes []string `json:"changes"`
}

// Humanize propose une version sans tics et la liste des changements.
func (s *Studio) Humanize(ctx context.Context, id string) (*HumanizeProposal, error) {
	a, d, p, err := s.linkedinPost(id)
	if err != nil {
		return nil, err
	}
	if err := editable(p); err != nil {
		return nil, err
	}
	var out HumanizeProposal
	if err := s.aiFor(a).JSON(ctx, liMethodSystem(a, d), liMethodUser(a, liHumanizerMethod, p), humanizerSchema, &out); err != nil {
		return nil, err
	}
	out.Text = strings.TrimSpace(out.Text)
	if out.Text == "" {
		return nil, errors.New("l'IA n'a rien proposé")
	}
	if out.Changes == nil {
		out.Changes = []string{}
	}
	return &out, nil
}

// ApplyHumanized applique la version de l'Humanizer (clic « Appliquer ») : une
// révision est ajoutée à l'historique, la validation repart à zéro. Ce n'est
// pas une correction d'Allan : elle n'alimente pas l'apprentissage.
func (s *Studio) ApplyHumanized(id, text string) (*Post, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("texte vide")
	}
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if !isLinkedIn(d.Account(p.Account)) {
			return errNotLinkedIn
		}
		if err := editable(p); err != nil {
			return err
		}
		now := s.now().UTC()
		p.History = append(p.History, Revision{Text: p.Text, Feedback: "humanizer appliqué", At: now})
		p.Text = text
		resetValidation(p)
		resetTicCheck(p)
		p.UpdatedAt = now
		out = p
		return nil
	})
	return out, err
}

// AuditSuggestion : une suggestion du rapport d'audit.
type AuditSuggestion struct {
	Area   string `json:"area"`
	Advice string `json:"advice"`
}

// AuditReport : rapport informatif, il ne bloque rien.
type AuditReport struct {
	Chars       int               `json:"chars"`
	HookChars   int               `json:"hookChars"`
	Tics        []Tic             `json:"tics"`
	Suggestions []AuditSuggestion `json:"suggestions"`
}

// Audit rend un rapport de suggestions (accroche, longueur, clarté, tics).
func (s *Studio) Audit(ctx context.Context, id string) (*AuditReport, error) {
	a, d, p, err := s.linkedinPost(id)
	if err != nil {
		return nil, err
	}
	r := &AuditReport{Chars: utf8.RuneCountInString(p.Text), Tics: TicCheck(p.Text), Suggestions: []AuditSuggestion{}}
	if end := firstLineEnd(p.Text); end >= 0 {
		r.HookChars = utf8.RuneCountInString(p.Text[:end])
	} else {
		r.HookChars = r.Chars
	}
	user := liMethodUser(a, liAuditMethod, p)
	if len(r.Tics) > 0 {
		user += "\nTics déjà repérés par le contrôle du studio : " + ticSummary(r.Tics) + "\n"
	}
	var out struct {
		Suggestions []AuditSuggestion `json:"suggestions"`
	}
	if err := s.aiFor(a).JSON(ctx, liMethodSystem(a, d), user, auditSchema, &out); err != nil {
		return nil, err
	}
	for _, sg := range out.Suggestions {
		if sg.Advice = strings.TrimSpace(sg.Advice); sg.Advice != "" {
			sg.Area = oneLineClean(sg.Area)
			r.Suggestions = append(r.Suggestions, sg)
		}
	}
	return r, nil
}

// PlanWeek propose, pour 7 jours, des IDÉES (statut proposé) : jamais de post,
// jamais de date validée. Avec un plafond hebdomadaire réglé, il borne le nombre d'idées.
func (s *Studio) PlanWeek(ctx context.Context, account string) ([]*Idea, error) {
	a, d, err := s.accountAndData(account)
	if err != nil {
		return nil, err
	}
	if !isLinkedIn(a) {
		return nil, errNotLinkedIn
	}
	n := 7
	if a.WeeklyCap > 0 {
		n = min(n, a.WeeklyCap)
	}
	var u strings.Builder
	u.WriteString(liMethodClause(a) + "\n\n<methode>\n" + liPlanMethod + "\n</methode>\n")
	fmt.Fprintf(&u, "\nPropose au plus %d idées pour la semaine.\n", n)
	themes := a.Ideas.Themes
	if len(themes) == 0 {
		themes = a.Topics
	}
	if len(themes) > 0 {
		u.WriteString("\n<sujets>\n" + strings.Join(themes, "\n") + "\n</sujets>\n")
	}
	var known []string
	for _, i := range d.Ideas {
		if strings.EqualFold(i.Account, a.Name) && i.Status != IdeaDismissed {
			known = append(known, i.Title)
		}
	}
	if len(known) > 0 {
		u.WriteString("\nIdées déjà dans la banque (n'en propose pas de variante) :\n- " + strings.Join(lastN(known, 40), "\n- ") + "\n")
	}
	var out struct {
		Ideas []struct {
			Title string `json:"title"`
			Angle string `json:"angle"`
			Why   string `json:"why"`
		} `json:"ideas"`
	}
	if err := s.aiFor(a).JSON(ctx, liMethodSystem(a, d), u.String(), ideasSchema, &out); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var ideas []*Idea
	for _, o := range out.Ideas {
		if t := strings.TrimSpace(o.Title); t != "" && len(ideas) < n {
			ideas = append(ideas, &Idea{ID: newID("i"), Account: a.Name, Title: t, Angle: strings.TrimSpace(o.Angle), Why: strings.TrimSpace(o.Why), Status: IdeaProposed, CreatedAt: now})
		}
	}
	if len(ideas) == 0 {
		return nil, errors.New("l'IA n'a proposé aucune idée")
	}
	return ideas, s.store.Update(func(d *Data) error { d.Ideas = append(d.Ideas, ideas...); return nil })
}

// SetLine change l'étiquette de ligne d'un brouillon LinkedIn : la changer
// remet la validation à zéro.
func (s *Studio) SetLine(id, line string) (*Post, error) {
	line = strings.TrimSpace(line)
	if !validLinkedInLine(line) {
		return nil, fmt.Errorf("ligne %q inconnue : %s", line, strings.Join(linkedinLines, ", "))
	}
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if !isLinkedIn(d.Account(p.Account)) {
			return errNotLinkedIn
		}
		if err := editable(p); err != nil {
			return err
		}
		if p.Line != line {
			p.Line = line
			resetValidation(p)
			p.UpdatedAt = s.now().UTC()
		}
		out = p
		return nil
	})
	return out, err
}
