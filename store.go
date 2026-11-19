package main

// Persistance : un seul fichier JSON (mode 0600) dans $XGROWTH_HOME (~/.xgrowth
// par défaut). Toutes les mutations passent par Store.Update, qui verrouille,
// applique la fonction puis réécrit le fichier de façon atomique.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Statuts d'un post. Un post ne part JAMAIS sans être passé par "validated" ou
// "scheduled" avec un ValidatedBy renseigné.
const (
	StatusDraft      = "draft"
	StatusScheduled  = "scheduled"  // validé, avec une date de publication
	StatusValidated  = "validated"  // validé, à publier dès que possible
	StatusPublishing = "publishing" // envoi en cours : jamais relancé automatiquement
	StatusPublished  = "published"
	StatusFailed     = "failed"
	StatusRejected   = "rejected"
)

// Statuts d'un contenu de veille.
const (
	ItemPending = "pending"
	ItemKept    = "kept"
	ItemIgnored = "ignored"
	ItemDrafted = "drafted"
)

type WatchTarget struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"` // "user" | "search"
	Value       string    `json:"value"`
	IntervalMin int       `json:"intervalMin"`
	LastPolled  time.Time `json:"lastPolled,omitzero"`
	LastSeenID  string    `json:"lastSeenId,omitempty"`

	Primary bool `json:"primary,omitempty"` // source primaire : ses posts sont des actualités
}

type Account struct {
	Name          string        `json:"name"`
	Handle        string        `json:"handle"`
	DisplayName   string        `json:"displayName,omitempty"`
	Backend       string        `json:"backend"`                 // agentx | xapi | dryrun
	AgentxAccount string        `json:"agentxAccount,omitempty"` // nom passé à `agentx -a`
	XAPITokenEnv  string        `json:"xapiTokenEnv,omitempty"`  // variable d'env. du jeton OAuth2 utilisateur
	Premium       bool          `json:"premium"`                 // autorise > 280 caractères
	Language      string        `json:"language"`
	Timezone      string        `json:"timezone"`
	Slots         []string      `json:"slots"` // créneaux "HH:MM" dans le fuseau du compte
	Voice         string        `json:"voice"`
	Instructions  string        `json:"instructions"`
	Topics        []string      `json:"topics"`
	TriageRules   string        `json:"triageRules"` // critères de tri de la veille
	AutoTriage    bool          `json:"autoTriage"`
	AutoDraft     bool          `json:"autoDraft"` // rédige un brouillon pour chaque contenu gardé
	DailyLimit    int           `json:"dailyLimit"`
	Watch         []WatchTarget `json:"watch"`

	// Sources : changelog, étoiles GitHub, actualités, méthode (facultatif).
	Sources SourceSettings `json:"sources,omitzero"`

	// Affiner l'IA (tous facultatifs : un data.json plus ancien reste valide).
	Goals     string     `json:"goals,omitempty"`     // objectifs du compte
	Length    string     `json:"length,omitempty"`    // court | moyen | long (vide = court)
	Examples  []string   `json:"examples,omitempty"`  // posts de référence pour le style
	PostTypes []PostType `json:"postTypes,omitempty"` // vide = types communs des réglages
	Prompts   AIPrompts  `json:"prompts,omitzero"`    // consignes propres au compte

	// Profil, IA, idées et programmation (facultatifs eux aussi).
	Role       string       `json:"role,omitempty"`       // personnel | marque | automatise
	Automated  bool         `json:"automated,omitempty"`  // compte ouvertement automatisé
	AIProvider string       `json:"aiProvider,omitempty"` // vide = fournisseur des réglages
	AIModel    string       `json:"aiModel,omitempty"`    // vide = modèle des réglages
	Ideas      IdeaSettings `json:"ideas,omitzero"`
	Timing     Timing       `json:"timing,omitzero"`
}

type Metrics struct {
	Likes          int       `json:"likes"`
	Retweets       int       `json:"retweets"`
	Replies        int       `json:"replies"`
	Quotes         int       `json:"quotes"`
	Views          int       `json:"views"`
	Bookmarks      int       `json:"bookmarks"`
	EngagementRate float64   `json:"engagementRatePct"`
	UpdatedAt      time.Time `json:"updatedAt,omitzero"`
}

type Revision struct {
	Text     string    `json:"text"`
	Feedback string    `json:"feedback,omitempty"`
	At       time.Time `json:"at"`
}

type Post struct {
	ID          string     `json:"id"`
	Account     string     `json:"account"`
	Kind        string     `json:"kind"` // post | reply | quote
	Text        string     `json:"text"`
	ReplyTo     string     `json:"replyTo,omitempty"`
	QuoteOf     string     `json:"quoteOf,omitempty"`
	TargetText  string     `json:"targetText,omitempty"` // texte du post auquel on répond / qu'on cite
	MediaPath   string     `json:"mediaPath,omitempty"`
	Rationale   string     `json:"rationale,omitempty"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	ValidatedBy string     `json:"validatedBy,omitempty"`
	ValidatedAt *time.Time `json:"validatedAt,omitempty"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	TweetID     string     `json:"tweetId,omitempty"`
	CampaignID  string     `json:"campaignId,omitempty"` // réponse rédigée pour une campagne
	Error       string     `json:"error,omitempty"`
	SourceItem  string     `json:"sourceItem,omitempty"`
	PostType    string     `json:"postType,omitempty"`    // type de post choisi par l'IA
	VisualBrief string     `json:"visualBrief,omitempty"` // idée de visuel proposée par l'IA
	AIText      string     `json:"aiText,omitempty"`      // dernier texte écrit par l'IA (pour apprendre des corrections)
	IdeaID      string     `json:"ideaId,omitempty"`      // idée de la banque dont le post est tiré
	History     []Revision `json:"history,omitempty"`
	Metrics     *Metrics   `json:"metrics,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type Item struct {
	ID        string    `json:"id"`
	Account   string    `json:"account"`
	Source    string    `json:"source"` // watch:user:x | watch:search:q | bookmarks | manual
	TweetID   string    `json:"tweetId"`
	URL       string    `json:"url"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	Likes     int       `json:"likes"`
	Views     int       `json:"views"`
	Status    string    `json:"status"`
	Score     float64   `json:"score"`
	Reason    string    `json:"reason,omitempty"`
	Angle     string    `json:"angle,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
	// Le tri par l'IA ne fait que suggérer : seul l'utilisateur change Status
	// (sauf les brouillons automatiques, qui passent le contenu en « drafted »).
	Suggestion string     `json:"suggestion,omitempty"` // keep | skip
	TriagedAt  *time.Time `json:"triagedAt,omitempty"`
}

const (
	SuggestKeep = "keep"
	SuggestSkip = "skip"
)

type Learning struct {
	ID            string     `json:"id"`
	Account       string     `json:"account"` // vide = tous les comptes
	Text          string     `json:"text"`
	CreatedAt     time.Time  `json:"createdAt"`
	SynthesizedAt *time.Time `json:"synthesizedAt,omitempty"` // relu par une synthèse des retours
	Source        string     `json:"source,omitempty"`        // edit | rewrite | reject | manual
	Category      string     `json:"category,omitempty"`      // fond | ton | longueur | fait | timing | visuel | autre
	Ignored       bool       `json:"ignored,omitempty"`       // « ne pas apprendre »
	PostID        string     `json:"postId,omitempty"`
	Before        string     `json:"before,omitempty"` // correction : texte de l'IA
	After         string     `json:"after,omitempty"`  // correction : texte du propriétaire
}

type Settings struct {
	AIProvider string `json:"aiProvider"` // anthropic | claude-code | codex | gemini (vide = détection)
	Model      string `json:"model"`      // vide = modèle par défaut du fournisseur
	Effort     string `json:"effort"`

	// Sources : actualités des sources primaires et relevés (vide ou 0 = défaut).
	NewsPrompt          string `json:"newsPrompt,omitempty"`          // consigne des brouillons de news
	NewsQuietMinutes    int    `json:"newsQuietMinutes,omitempty"`    // attente que le fil se calme (4)
	NewsMaxAgeHours     int    `json:"newsMaxAgeHours,omitempty"`     // âge maximum d'une actualité (12)
	NewsMaxPerDay       int    `json:"newsMaxPerDay,omitempty"`       // actualités rédigées par jour et par compte (8)
	SourcesEveryMinutes int    `json:"sourcesEveryMinutes,omitempty"` // relevé du changelog et des étoiles (60)

	Competitors    []string `json:"competitors"`   // noms jamais publiés
	BannedPhrases  []string `json:"bannedPhrases"` // expressions interdites
	MinGapMinutes  int      `json:"minGapMinutes"` // écart minimal entre deux posts d'un compte
	TriageMinScore float64  `json:"triageMinScore"`
	MaxItemsPerRun int      `json:"maxItemsPerRun"`
	Onboarded      bool     `json:"onboarded"`
	// Dernière visite de la veille : les contenus récupérés après sont « New ».
	// Stockée côté serveur pour rester juste d'un navigateur à l'autre.
	WatchSeenAt *time.Time `json:"watchSeenAt,omitempty"`

	// Affiner l'IA, pour tous les comptes.
	Rules           []string   `json:"rules,omitempty"`     // règles d'écriture communes
	PostTypes       []PostType `json:"postTypes,omitempty"` // types de posts communs
	Prompts         AIPrompts  `json:"prompts,omitzero"`    // consignes communes par demande
	RulesAutoAccept bool       `json:"rulesAutoAccept"`     // applique d'office les règles proposées
	AutoSynthesis   bool       `json:"autoSynthesis"`       // synthèse hebdomadaire des retours
	LastSynthesisAt *time.Time `json:"lastSynthesisAt,omitempty"`
	// Analyse des performances : propose des règles à partir des statistiques.
	AutoPerfAnalysis bool `json:"autoPerfAnalysis"` // analyse hebdomadaire, désactivée par défaut
	// Obsolète depuis la v3 : l'état est par compte (Data.PerfStates). Lu une
	// seule fois, par migrate, comme dernier succès de chaque compte.
	LastPerfAnalysisAt *time.Time `json:"lastPerfAnalysisAt,omitempty"`

	// Campagnes de réponses : consigne des commentaires (vide = défaut).
	CommentsPrompt string `json:"commentsPrompt,omitempty"`
}

// Auth protège le studio web : mot de passe (PBKDF2) et sessions (hachées).
type Auth struct {
	PasswordHash string    `json:"passwordHash,omitempty"`
	Salt         string    `json:"salt,omitempty"`
	Iterations   int       `json:"iterations,omitempty"`
	SetupCode    string    `json:"setupCode,omitempty"` // code à usage unique pour créer le mot de passe
	Sessions     []Session `json:"sessions,omitempty"`
}

type Session struct {
	Hash    string    `json:"hash"`
	Expires time.Time `json:"expires"`
}

type LogEntry struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type Data struct {
	Version  int        `json:"version"`
	Settings Settings   `json:"settings"`
	Accounts []*Account `json:"accounts"`
	Posts    []*Post    `json:"posts"`
	Items    []*Item    `json:"items"`
	// campagnes de réponses et posts qu'elles ont trouvés
	Campaigns  []*Campaign  `json:"campaigns,omitempty"`
	Candidates []*Candidate `json:"candidates,omitempty"`
	Learnings  []Learning   `json:"learnings"`
	// règles tirées des retours par la synthèse
	RuleProposals []*RuleProposal `json:"ruleProposals,omitempty"`
	// état de l'analyse des performances, par compte (clé : nom en minuscules)
	PerfStates map[string]*PerfState `json:"perfStates,omitempty"`
	// banque d'idées de posts originaux
	Ideas []*Idea    `json:"ideas,omitempty"`
	Log   []LogEntry `json:"log"`
	Auth  Auth       `json:"auth"`
}

func defaultSettings() Settings {
	return Settings{
		Effort:         "medium",
		MinGapMinutes:  60,
		TriageMinScore: 6,
		MaxItemsPerRun: 40,
	}
}

// Store est partagé entre processus (CLI, `run`, `serve`) : un verrou de
// fichier (flock) sérialise les écritures et chaque accès relit le fichier
// s'il a changé sur disque.
type Store struct {
	path    string
	lock    *os.File
	mu      sync.Mutex
	data    *Data
	modTime time.Time
	// saveFailing : voir SaveFailing
	saveFailing bool
}

func homeDir() string {
	if h := os.Getenv("XGROWTH_HOME"); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ".xgrowth"
	}
	return filepath.Join(h, ".xgrowth")
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "data.json")}
	lf, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	s.lock = lf
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.flock(syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer s.flock(syscall.LOCK_UN)
	if err := s.reload(); err != nil {
		return nil, err
	}
	if s.data == nil {
		s.data = &Data{Version: dataVersion, Settings: defaultSettings()}
		return s, s.save(s.data)
	}
	if b, err := os.ReadFile(s.path); err == nil {
		var head struct {
			Version int `json:"version"`
		}
		// la migration a eu lieu en mémoire : on l'écrit tout de suite
		if json.Unmarshal(b, &head) == nil && head.Version < dataVersion {
			if err := s.save(s.data); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

func (s *Store) flock(how int) error { return syscall.Flock(int(s.lock.Fd()), how) }

// reload relit data.json s'il a été modifié par un autre processus.
func (s *Store) reload() error {
	st, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.data != nil && st.ModTime().Equal(s.modTime) {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	d := &Data{}
	if err := json.Unmarshal(b, d); err != nil {
		return fmt.Errorf("data.json illisible : %w", err)
	}
	if d.Settings.MinGapMinutes == 0 && d.Settings.MaxItemsPerRun == 0 {
		d.Settings = defaultSettings()
	}
	migrate(d)
	s.data, s.modTime = d, st.ModTime()
	return nil
}

const dataVersion = 3

// migrate met à jour un data.json écrit par une version précédente.
// Renvoie true si quelque chose a changé.
func migrate(d *Data) bool {
	if d.Version >= dataVersion {
		return false
	}
	if d.Version < 2 {
		// Avant la v2, le tri automatique déplaçait seul les contenus en
		// « ignorés » (et en « gardés »). Il ne fait désormais que suggérer :
		// les contenus écartés par l'IA reviennent « à trier ».
		for _, it := range d.Items {
			if it.Score <= 0 || it.TriagedAt != nil {
				continue
			}
			t := it.FetchedAt
			it.TriagedAt = &t
			switch it.Status {
			case ItemIgnored:
				it.Status, it.Suggestion = ItemPending, SuggestSkip
			case ItemKept, ItemDrafted:
				it.Suggestion = SuggestKeep
			}
		}
	}
	if d.Version < 3 {
		// L'horodatage global de l'analyse des performances sert de dernier
		// succès à chaque compte.
		if at := d.Settings.LastPerfAnalysisAt; at != nil {
			for _, a := range d.Accounts {
				if d.PerfStates[perfKey(a.Name)] == nil {
					t := *at
					d.perfState(a.Name).LastSuccessAt = &t
				}
			}
			d.Settings.LastPerfAnalysisAt = nil
		}
	}
	d.Version = dataVersion
	return true
}

// RemoveAccount supprime le compte et son état d'analyse : un compte recréé sous
// le même nom repart de zéro.
func (d *Data) RemoveAccount(name string) error {
	for i, a := range d.Accounts {
		if strings.EqualFold(a.Name, name) {
			d.Accounts = append(d.Accounts[:i], d.Accounts[i+1:]...)
			delete(d.PerfStates, perfKey(a.Name))
			return nil
		}
	}
	return fmt.Errorf("compte %q inconnu", name)
}

// perfState renvoie l'état d'analyse du compte, créé au besoin (écriture : à
// appeler dans un Update).
func (d *Data) perfState(account string) *PerfState {
	if d.PerfStates == nil {
		d.PerfStates = map[string]*PerfState{}
	}
	st := d.PerfStates[perfKey(account)]
	if st == nil {
		st = &PerfState{}
		d.PerfStates[perfKey(account)] = st
	}
	return st
}

// save écrit data.json. Il ne touche pas à s.data : l'appelant ne remplace la
// mémoire qu'après une écriture réussie.
func (s *Store) save(d *Data) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	if st, err := os.Stat(s.path); err == nil {
		s.modTime = st.ModTime()
	}
	return nil
}

// View donne un accès en lecture sous verrou.
func (s *Store) View(fn func(d *Data)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.flock(syscall.LOCK_SH)
	defer s.flock(syscall.LOCK_UN)
	_ = s.reload()
	fn(s.data)
}

// Update applique fn et sauvegarde si fn ne renvoie pas d'erreur.
func (s *Store) Update(fn func(d *Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.flock(syscall.LOCK_EX); err != nil {
		return err
	}
	defer s.flock(syscall.LOCK_UN)
	if err := s.reload(); err != nil {
		return err
	}
	// travaille sur une copie : si fn échoue, rien n'est modifié en mémoire
	b, _ := json.Marshal(s.data)
	work := &Data{}
	_ = json.Unmarshal(b, work)
	if err := fn(work); err != nil {
		return err
	}
	// la mémoire ne bascule qu'après l'écriture : en cas d'échec, mémoire et
	// disque restent égaux à l'état d'avant
	if err := s.save(work); err != nil {
		s.saveFailing = true
		return err
	}
	s.saveFailing = false
	s.data = work
	return nil
}

// SaveFailing : la dernière écriture de ce processus a échoué (disque plein ou
// en panne). Remis à faux par la première écriture réussie.
func (s *Store) SaveFailing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveFailing
}

// Snapshot renvoie une copie profonde (via JSON) pour la lire sans verrou.
func (s *Store) Snapshot() *Data {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.flock(syscall.LOCK_SH)
	defer s.flock(syscall.LOCK_UN)
	_ = s.reload()
	b, _ := json.Marshal(s.data)
	d := &Data{}
	_ = json.Unmarshal(b, d)
	return d
}

func newID(prefix string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func (d *Data) Account(name string) *Account {
	for _, a := range d.Accounts {
		if strings.EqualFold(a.Name, name) {
			return a
		}
	}
	return nil
}

func (d *Data) Post(id string) *Post {
	for _, p := range d.Posts {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (d *Data) Item(id string) *Item {
	for _, it := range d.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (d *Data) HasItemTweet(account, tweetID string) bool {
	for _, it := range d.Items {
		if it.Account == account && it.TweetID == tweetID {
			return true
		}
	}
	return false
}

func (d *Data) AddLog(level, format string, args ...any) {
	d.Log = append(d.Log, LogEntry{At: time.Now().UTC(), Level: level, Message: fmt.Sprintf(format, args...)})
	if len(d.Log) > 500 {
		d.Log = d.Log[len(d.Log)-500:]
	}
}

// LearningsFor renvoie les derniers retours applicables à un compte.
func (d *Data) LearningsFor(account string, max int) []Learning {
	var out []Learning
	for i := len(d.Learnings) - 1; i >= 0 && len(out) < max; i-- {
		l := d.Learnings[i]
		if l.Ignored {
			continue
		}
		if l.Account == "" || strings.EqualFold(l.Account, account) {
			out = append(out, l)
		}
	}
	return out
}

// RecentPublished renvoie les derniers posts publiés d'un compte (plus récent d'abord).
func (d *Data) RecentPublished(account string, max int) []*Post {
	var ps []*Post
	for _, p := range d.Posts {
		if p.Account == account && p.Status == StatusPublished && p.PublishedAt != nil {
			ps = append(ps, p)
		}
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].PublishedAt.After(*ps[j].PublishedAt) })
	if len(ps) > max {
		ps = ps[:max]
	}
	return ps
}

// TopPerformers renvoie les posts publiés avec le meilleur engagement.
func (d *Data) TopPerformers(account string, max int) []*Post {
	var ps []*Post
	for _, p := range d.Posts {
		if p.Account == account && p.Status == StatusPublished && p.Metrics != nil && p.Metrics.Views > 0 {
			ps = append(ps, p)
		}
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].Metrics.EngagementRate > ps[j].Metrics.EngagementRate })
	if len(ps) > max {
		ps = ps[:max]
	}
	return ps
}
