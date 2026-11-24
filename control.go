package main

// Centre de contrôle des publications : « Est-ce que tout tourne, et qu'est-ce
// qui demande mon intervention ? » Une pause par compte, l'heure du dernier
// passage de chaque boucle du planificateur, les publications en retard, les
// connexions à renouveler et les envois au résultat incertain. Aucune
// notification externe : tout se lit dans le studio.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// publishLateAfter : au-delà, un post dont l'heure est passée est « en retard ».
	publishLateAfter = 5 * time.Minute
	// publishingStuckAfter : un envoi « en cours » plus ancien n'a pas abouti
	// proprement (arrêt du processus) : son résultat est inconnu.
	publishingStuckAfter = 10 * time.Minute
	// loopStaleFloor : seuil minimal d'alerte sur un passage ancien.
	loopStaleFloor = 5 * time.Minute
	defaultEvery   = 60 * time.Second
)

// ---------------------------------------------------------------- envoi incertain

// uncertainError : l'envoi est parti (ou a pu partir) mais on ignore s'il a
// abouti : erreur réseau ou délai dépassé après l'envoi, réponse illisible,
// 5xx. Le post n'est jamais relancé tout seul.
type uncertainError struct{ err error }

func (e *uncertainError) Error() string { return e.err.Error() }
func (e *uncertainError) Unwrap() error { return e.err }

func markUncertain(err error) error {
	if err == nil || isUncertain(err) {
		return err
	}
	return &uncertainError{err}
}

func isUncertain(err error) bool {
	var u *uncertainError
	return errors.As(err, &u)
}

// xapiHTTPError : réponse HTTP d'erreur de l'API X (le message reste celui d'avant).
type xapiHTTPError struct {
	status int
	msg    string
}

func (e *xapiHTTPError) Error() string { return e.msg }

// xapiPostMaybeSent : après une erreur sur POST /tweets, le post a-t-il pu
// partir ? Une réponse HTTP d'erreur est un refus net ; un échec de connexion
// avant l'envoi aussi. Le reste (délai, coupure, réponse illisible) est incertain.
func xapiPostMaybeSent(err error) bool {
	var he *xapiHTTPError
	if errors.As(err, &he) {
		return false
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return false
	}
	return true
}

// ---------------------------------------------------------------- pause

// SetPaused met un compte en pause ou le relance. Un compte en pause n'est pas
// publié par le planificateur ; l'état est dans data.json et survit au redémarrage.
func (s *Studio) SetPaused(name string, paused bool) (*Account, error) {
	var out *Account
	err := s.store.Update(func(d *Data) error {
		a := d.Account(name)
		if a == nil {
			return fmt.Errorf("compte %q inconnu", name)
		}
		if a.Paused == paused {
			out = a
			return nil
		}
		a.Paused = paused
		if paused {
			t := s.now().UTC()
			a.PausedAt = &t
			d.AddLog("warn", "publication du compte %s mise en pause", a.Name)
		} else {
			a.PausedAt = nil
			d.AddLog("info", "publication du compte %s relancée", a.Name)
		}
		out = a
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------- passages des boucles

// Boucles du planificateur suivies : la publication d'abord (ce qui compte
// pour ne rien rater), puis le reste d'un passage (veille, tri, statistiques).
// Les noms viennent de loops.go (schedulerLoopPublish, schedulerLoopAI).
var loopLabels = map[string]string{schedulerLoopPublish: "Publication", schedulerLoopAI: "Veille, tri et statistiques (IA)"}
var loopOrder = []string{schedulerLoopPublish, schedulerLoopAI}

type loopPass struct {
	At       time.Time `json:"at"`
	EverySec int       `json:"everySec"`
}

type loopFile struct {
	Loops map[string]loopPass `json:"loops"`
}

func (s *Studio) loopsPath() string {
	return filepath.Join(filepath.Dir(s.store.path), "scheduler.json")
}

func (s *Studio) readLoops() map[string]loopPass {
	var f loopFile
	if b, err := os.ReadFile(s.loopsPath()); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	if f.Loops == nil {
		f.Loops = map[string]loopPass{}
	}
	return f.Loops
}

// recordLoop note l'heure de fin d'un passage (appelé par endLoop, loops.go),
// dans un petit fichier à part
// (scheduler.json) : l'état survit au redémarrage et se partage entre le
// processus du planificateur et celui du studio, sans réécrire data.json à
// chaque passage. Un échec d'écriture ne bloque jamais le planificateur.
func (s *Studio) recordLoop(name string, at time.Time) {
	every := s.every
	if every <= 0 {
		every = defaultEvery
	}
	loops := s.readLoops()
	loops[name] = loopPass{At: at.UTC(), EverySec: int(every / time.Second)}
	b, err := json.Marshal(loopFile{Loops: loops})
	if err == nil {
		err = writeFileDurable(s.loopsPath(), b)
	}
	if err != nil {
		s.logf("dernier passage du planificateur : échec de l'enregistrement")
	}
}

// ---------------------------------------------------------------- rapport

type LoopHealth struct {
	Name     string     `json:"name"`
	Label    string     `json:"label"`
	LastAt   *time.Time `json:"lastAt,omitempty"` // absent : aucun passage connu
	EverySec int        `json:"everySec,omitempty"`
	AgeSec   int        `json:"ageSec,omitempty"`
	Stale    bool       `json:"stale"`
}

type LatePost struct {
	ID          string    `json:"id"`
	Account     string    `json:"account"`
	Status      string    `json:"status"`
	ScheduledAt time.Time `json:"scheduledAt"`
	LateMin     int       `json:"lateMin"`
	Preview     string    `json:"preview"`
	Paused      bool      `json:"paused,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type ConnectionIssue struct {
	Account  string     `json:"account"`
	Backend  string     `json:"backend"`
	State    string     `json:"state"` // expired | expiring | disconnected | missing
	ExpireAt *time.Time `json:"expiresAt,omitempty"`
	DaysLeft *int       `json:"daysLeft,omitempty"`
	Detail   string     `json:"detail"`
}

type UncertainPost struct {
	ID      string     `json:"id"`
	Account string     `json:"account"`
	Since   *time.Time `json:"since,omitempty"`
	Preview string     `json:"preview"`
	Error   string     `json:"error,omitempty"`
}

type PausedAccount struct {
	Name   string     `json:"name"`
	Handle string     `json:"handle"`
	Paused bool       `json:"paused"`
	Since  *time.Time `json:"since,omitempty"`
}

type ControlReport struct {
	At          time.Time         `json:"at"`
	OK          bool              `json:"ok"`
	Attention   int               `json:"attention"` // nombre de points qui demandent une action
	Accounts    []PausedAccount   `json:"accounts"`
	Loops       []LoopHealth      `json:"loops"`
	Late        []LatePost        `json:"late"`
	Connections []ConnectionIssue `json:"connections"`
	Uncertain   []UncertainPost   `json:"uncertain"`
	// comptes dont le backend n'expose aucune date d'expiration (agentx : cookies)
	Unmonitored []string `json:"unmonitored"`
}

func preview(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	if r := []rune(t); len(r) > 90 {
		return string(r[:90]) + "…"
	}
	return t
}

// lateAfter : les posts planifiés ou validés dont l'heure est passée depuis plus
// de publishLateAfter, ou à publier à la main (LinkedIn) et jamais marqués publiés.
func lateAfter(d *Data, now time.Time) []LatePost {
	out := []LatePost{}
	for _, p := range d.Posts {
		if p.ScheduledAt == nil {
			continue
		}
		switch p.Status {
		case StatusScheduled, StatusValidated, StatusManual:
		default:
			continue
		}
		late := now.Sub(*p.ScheduledAt)
		if late < publishLateAfter {
			continue
		}
		lp := LatePost{ID: p.ID, Account: p.Account, Status: p.Status, ScheduledAt: *p.ScheduledAt,
			LateMin: int(late / time.Minute), Preview: preview(p.Text), Error: p.Error}
		if a := d.Account(p.Account); a != nil && a.Paused {
			lp.Paused = true
		}
		out = append(out, lp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ScheduledAt.Before(out[j].ScheduledAt) })
	return out
}

func (s *Studio) loopStatuses(now time.Time) []LoopHealth {
	passes := s.readLoops()
	// l'état en mémoire de loops.go (même processus) peut être plus récent
	for name, ls := range s.SchedulerStatus() {
		if ls.LastEnd == nil {
			continue
		}
		if p, ok := passes[name]; !ok || ls.LastEnd.After(p.At) {
			passes[name] = loopPass{At: *ls.LastEnd, EverySec: int(s.every / time.Second)}
		}
	}
	out := make([]LoopHealth, 0, len(loopOrder))
	for _, name := range loopOrder {
		ls := LoopHealth{Name: name, Label: loopLabels[name]}
		p, ok := passes[name]
		if !ok || p.At.IsZero() {
			ls.Stale = true // jamais passé : le planificateur ne tourne pas
			out = append(out, ls)
			continue
		}
		at := p.At
		every := time.Duration(p.EverySec) * time.Second
		if every <= 0 {
			every = defaultEvery
		}
		age := now.Sub(at)
		ls.LastAt, ls.EverySec, ls.AgeSec = &at, int(every/time.Second), int(age/time.Second)
		limit := 3 * every
		if limit < loopStaleFloor {
			limit = loopStaleFloor
		}
		ls.Stale = age > limit
		out = append(out, ls)
	}
	return out
}

// connectionIssues : ce que chaque backend expose. LinkedIn donne la date
// d'expiration du jeton ; xapi dit seulement si le jeton est présent ;
// agentx (cookies) n'expose rien : le compte est alors listé comme non suivi.
func (s *Studio) connectionIssues(d *Data, now time.Time) ([]ConnectionIssue, []string) {
	issues := []ConnectionIssue{}
	unmon := []string{}
	for _, a := range d.Accounts {
		switch {
		case isLinkedIn(a) && a.Backend == "linkedin":
			st := liStateOf(s.liTokens().get(a.Name), now)
			switch st.State {
			case "expired":
				issues = append(issues, ConnectionIssue{Account: a.Name, Backend: a.Backend, State: "expired", ExpireAt: st.ExpiresAt, DaysLeft: st.DaysLeft, Detail: "jeton LinkedIn expiré : reconnectez le compte"})
			case "expiring":
				issues = append(issues, ConnectionIssue{Account: a.Name, Backend: a.Backend, State: "expiring", ExpireAt: st.ExpiresAt, DaysLeft: st.DaysLeft, Detail: "jeton LinkedIn bientôt expiré : reconnectez le compte"})
			case "disconnected":
				issues = append(issues, ConnectionIssue{Account: a.Name, Backend: a.Backend, State: "disconnected", Detail: "LinkedIn non connecté : connectez le compte"})
			}
		case !isLinkedIn(a) && a.Backend == "xapi":
			env := a.XAPITokenEnv
			if env == "" {
				env = "X_USER_TOKEN"
			}
			if os.Getenv(env) == "" {
				issues = append(issues, ConnectionIssue{Account: a.Name, Backend: a.Backend, State: "missing", Detail: "la variable " + env + " (jeton X) est vide"})
			} else {
				unmon = append(unmon, a.Name) // présent, mais sans date d'expiration connue
			}
		case !isLinkedIn(a) && a.Backend == "agentx":
			unmon = append(unmon, a.Name)
		}
	}
	return issues, unmon
}

// ControlReport rassemble tout ce qu'il faut voir d'un coup d'œil.
func (s *Studio) ControlReport(d *Data) ControlReport {
	now := s.now()
	r := ControlReport{At: now.UTC(), Accounts: []PausedAccount{}, Uncertain: []UncertainPost{}}
	for _, a := range d.Accounts {
		r.Accounts = append(r.Accounts, PausedAccount{Name: a.Name, Handle: a.Handle, Paused: a.Paused, Since: a.PausedAt})
	}
	r.Loops = s.loopStatuses(now)
	r.Late = lateAfter(d, now)
	r.Connections, r.Unmonitored = s.connectionIssues(d, now)
	for _, p := range d.Posts {
		if p.Status != StatusUncertain {
			continue
		}
		u := UncertainPost{ID: p.ID, Account: p.Account, Preview: preview(p.Text), Error: p.Error}
		if !p.UpdatedAt.IsZero() {
			t := p.UpdatedAt
			u.Since = &t
		}
		r.Uncertain = append(r.Uncertain, u)
	}
	for _, l := range r.Loops {
		if l.Stale {
			r.Attention++
		}
	}
	for _, l := range r.Late {
		if !l.Paused { // un retard dû à une pause voulue n'est pas une alerte
			r.Attention++
		}
	}
	r.Attention += len(r.Connections) + len(r.Uncertain)
	r.OK = r.Attention == 0
	return r
}

// ---------------------------------------------------------------- reprise des envois incertains

// recoverStuckPublishing : un post resté « publishing » trop longtemps (arrêt du
// processus pendant l'envoi) est rangé en « uncertain » : il n'est jamais relancé.
func (s *Studio) recoverStuckPublishing() {
	_ = s.store.Update(func(d *Data) error {
		now := s.now().UTC()
		changed := false
		for _, p := range d.Posts {
			if p.Status != StatusPublishing || (!p.UpdatedAt.IsZero() && now.Sub(p.UpdatedAt) < publishingStuckAfter) {
				continue
			}
			p.Status, p.UpdatedAt = StatusUncertain, now
			p.Error = "envoi interrompu avant la réponse : vérifiez sur le compte si le post est parti"
			d.AddLog("warn", "post %s : envoi interrompu, résultat incertain (à vérifier)", p.ID)
			changed = true
		}
		if !changed {
			return errors.New("rien à faire") // aucune écriture
		}
		return nil
	})
}

var xStatusRe = regexp.MustCompile(`^/(?:[A-Za-z0-9_]{1,15}|i)/status(?:es)?/(\d{5,25})(?:/.*)?$`)

// postURL contrôle un lien de publication (http ou https, sans caractère douteux).
func postURL(u string) (*neturl.URL, error) {
	pu, err := neturl.Parse(u)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Hostname() == "" {
		return nil, errors.New("url invalide : une adresse http(s) complète est attendue")
	}
	if strings.ContainsAny(u, " \t\r\n\"'<>`") {
		return nil, errors.New("url invalide : caractères interdits")
	}
	return pu, nil
}

// xTweetIDFromURL tire l'identifiant d'un lien de post X (x.com ou twitter.com).
func xTweetIDFromURL(pu *neturl.URL) (string, error) {
	h := strings.TrimPrefix(strings.ToLower(pu.Hostname()), "www.")
	h = strings.TrimPrefix(h, "mobile.")
	if h != "x.com" && h != "twitter.com" {
		return "", errors.New("lien X attendu : https://x.com/<compte>/status/<identifiant>")
	}
	m := xStatusRe.FindStringSubmatch(pu.Path)
	if m == nil {
		return "", errors.New("lien X illisible : https://x.com/<compte>/status/<identifiant> attendu")
	}
	return m[1], nil
}

// uncertainOutcome : un échec de Post qui laisse un doute sur l'envoi.
func uncertainOutcome(err error) bool {
	return isUncertain(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}
