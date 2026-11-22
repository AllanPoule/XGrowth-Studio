package main

// LinkedIn, lot 2 : connexion OAuth, publication par l'API officielle
// (« Share on LinkedIn », scope w_member_social) et état du jeton.
//
// Règles de sûreté, valables pour tout ce fichier :
//   - rien ne part sans ValidatedBy (contrôlé ici, en plus de Validate et PublishDue) ;
//   - un envoi n'est JAMAIS relancé automatiquement ;
//   - le jeton vit dans linkedin.json (0600), jamais dans data.json, ni dans
//     /api/state, les journaux, les messages d'erreur ou la CLI ;
//   - le client_id / client_secret ne sont lus que dans l'environnement ;
//   - aucune méthode de commentaire n'existe : les commentaires restent des brouillons.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

const (
	linkedinDefaultAPIVersion = "202611"
	linkedinScope             = "openid profile w_member_social"
	linkedinTokenLifetime     = 60 * 24 * time.Hour
	linkedinExpiringBefore    = 10 * 24 * time.Hour
	linkedinStateTTL          = 10 * time.Minute
	linkedinLateAfter         = 24 * time.Hour
	linkedinMaxImageBytes     = 10 << 20
	linkedinMaxPixels         = 36152320
)

// linkedinAlertDays : seuils d'alerte avant l'expiration du jeton (0 = expiré).
var linkedinAlertDays = []int{14, 7, 3, 1, 0}

var (
	errNotSupported = errors.New("LinkedIn : opération non prise en charge")
	// jeton absent ou expiré : le post reste en attente, il n'est pas en échec
	errLinkedInExpired      = errors.New("jeton LinkedIn expiré : reconnectez le compte")
	errLinkedInDisconnected = errors.New("LinkedIn non connecté : connectez le compte")
)

// ---------------------------------------------------------------- configuration

// liConfig : l'app LinkedIn d'Allan. Les secrets viennent de l'environnement
// (injectés par le coffre) ; les adresses ne changent que dans les tests.
type liConfig struct {
	ClientID, ClientSecret, RedirectURI string
	APIVersion                          string
	AuthURL, TokenURL, APIBase          string
	insecure                            bool // tests : autorise http:// vers le faux serveur
}

func liConfigFromEnv() liConfig {
	c := liConfig{
		ClientID:     os.Getenv("LINKEDIN_CLIENT_ID"),
		ClientSecret: os.Getenv("LINKEDIN_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("LINKEDIN_REDIRECT_URI"),
		APIVersion:   os.Getenv("LINKEDIN_API_VERSION"),
	}
	c.defaults()
	return c
}

func (c *liConfig) defaults() {
	if c.APIVersion == "" {
		c.APIVersion = linkedinDefaultAPIVersion
	}
	if c.AuthURL == "" {
		c.AuthURL = "https://www.linkedin.com/oauth/v2/authorization"
	}
	if c.TokenURL == "" {
		c.TokenURL = "https://www.linkedin.com/oauth/v2/accessToken"
	}
	if c.APIBase == "" {
		c.APIBase = "https://api.linkedin.com"
	}
}

func (c liConfig) configured() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURI != ""
}

var errLinkedInNotConfigured = errors.New("app LinkedIn non configurée : LINKEDIN_CLIENT_ID, LINKEDIN_CLIENT_SECRET et LINKEDIN_REDIRECT_URI doivent être définis")

// ---------------------------------------------------------------- jetons (linkedin.json)

type liToken struct {
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
	PersonID    string    `json:"personID"`
	DisplayName string    `json:"displayName,omitempty"`
	ConnectedAt time.Time `json:"connectedAt"`
	// AlertLevel : nombre de seuils d'alerte déjà signalés (0 = aucun).
	// Une reconnexion le remet à zéro.
	AlertLevel int `json:"alertLevel,omitempty"`
}

type liFile struct {
	Accounts map[string]*liToken `json:"accounts"`
}

// liTokens : le fichier linkedin.json d'un dossier de données. Les écritures
// sont sérialisées entre processus par un verrou de fichier.
type liTokens struct{ dir string }

var liTokensMu sync.Mutex

func liKey(account string) string { return strings.ToLower(strings.TrimSpace(account)) }

func (t liTokens) path() string { return filepath.Join(t.dir, "linkedin.json") }

func (t liTokens) read() (*liFile, error) {
	f := &liFile{Accounts: map[string]*liToken{}}
	b, err := os.ReadFile(t.path())
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, f); err != nil {
		return nil, errors.New("linkedin.json illisible")
	}
	if f.Accounts == nil {
		f.Accounts = map[string]*liToken{}
	}
	return f, nil
}

// get renvoie une copie du jeton d'un compte (nil si absent).
func (t liTokens) get(account string) *liToken {
	f, err := t.read()
	if err != nil {
		return nil
	}
	if tok := f.Accounts[liKey(account)]; tok != nil {
		c := *tok
		return &c
	}
	return nil
}

// update lit, modifie et réécrit linkedin.json (atomique, 0600) sous verrou.
func (t liTokens) update(fn func(f *liFile)) error {
	liTokensMu.Lock()
	defer liTokensMu.Unlock()
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(t.dir, ".linkedin.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	f, err := t.read()
	if err != nil {
		return err
	}
	fn(f)
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(t.dir, ".linkedin-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), t.path())
}

// ---------------------------------------------------------------- état de connexion

// LinkedInState est ce que l'interface et /api/state voient d'une connexion :
// jamais le jeton.
type LinkedInState struct {
	State      string     `json:"state"` // connected | expiring | expired | disconnected
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	DaysLeft   *int       `json:"daysLeft,omitempty"`
	PersonName string     `json:"personName,omitempty"`
}

func liStateOf(tok *liToken, now time.Time) LinkedInState {
	if tok == nil || tok.AccessToken == "" {
		return LinkedInState{State: "disconnected"}
	}
	exp := tok.ExpiresAt
	st := LinkedInState{ExpiresAt: &exp, PersonName: tok.DisplayName}
	left := exp.Sub(now)
	days := 0
	if left > 0 {
		days = int(left / (24 * time.Hour))
	}
	st.DaysLeft = &days
	switch {
	case left <= 0:
		st.State = "expired"
	case left < linkedinExpiringBefore:
		st.State = "expiring"
	default:
		st.State = "connected"
	}
	return st
}

func (s *Studio) liTokens() liTokens { return liTokens{dir: filepath.Dir(s.store.path)} }

// linkedinStates : l'état de connexion de chaque compte LinkedIn.
func (s *Studio) linkedinStates(d *Data) map[string]LinkedInState {
	out := map[string]LinkedInState{}
	for _, a := range d.Accounts {
		if isLinkedIn(a) {
			out[a.Name] = liStateOf(s.liTokens().get(a.Name), s.now())
		}
	}
	return out
}

// liAlertLevel : nombre de seuils franchis (0 à 5) pour un temps restant donné.
func liAlertLevel(left time.Duration) int {
	n := 0
	for i, days := range linkedinAlertDays {
		if left <= time.Duration(days)*24*time.Hour {
			n = i + 1
		}
	}
	return n
}

// LinkedInExpiryWatch écrit une entrée de journal « warn » à J-14, J-7, J-3,
// J-1 et à l'expiration, une seule fois par seuil. Rien n'est envoyé ailleurs.
func (s *Studio) LinkedInExpiryWatch() int {
	var names []string
	s.store.View(func(d *Data) {
		for _, a := range d.Accounts {
			if isLinkedIn(a) && a.Backend == "linkedin" {
				names = append(names, a.Name)
			}
		}
	})
	logged := 0
	for _, name := range names {
		tok := s.liTokens().get(name)
		if tok == nil || tok.AccessToken == "" {
			continue
		}
		left := tok.ExpiresAt.Sub(s.now())
		level := liAlertLevel(left)
		if level <= tok.AlertLevel {
			continue
		}
		// on note le seuil d'abord : si l'écriture échoue, rien n'est journalisé
		// (pas de doublon à chaque passage)
		if err := s.liTokens().update(func(f *liFile) {
			if t := f.Accounts[liKey(name)]; t != nil && t.AlertLevel < level {
				t.AlertLevel = level
			}
		}); err != nil {
			s.logf("alerte d'expiration LinkedIn : échec de l'enregistrement de linkedin.json")
			continue
		}
		msg := "le jeton LinkedIn du compte %s a expiré : reconnectez-le dans le studio"
		if left > 0 {
			msg = fmt.Sprintf("le jeton LinkedIn du compte %%s expire dans %d jour(s) : reconnectez-le dans le studio", int(left/(24*time.Hour)))
		}
		_ = s.store.Update(func(d *Data) error { d.AddLog("warn", msg, name); return nil })
		logged++
	}
	return logged
}

// ---------------------------------------------------------------- échappement du texte

// escapeLittle échappe le « little text format » de LinkedIn : un caractère
// réservé non échappé est mal rendu ou tronque le post. Un « # » suivi d'un
// mot reste tel quel (c'est un hashtag) ; « @ » est toujours échappé.
func escapeLittle(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case '#':
			if i+1 < len(rs) && isHashtagRune(rs[i+1]) {
				b.WriteRune(r)
				for i+1 < len(rs) && isHashtagRune(rs[i+1]) {
					i++
					b.WriteRune(rs[i])
				}
				continue
			}
			b.WriteString(`\#`)
		case '\\', '|', '{', '}', '@', '[', ']', '(', ')', '<', '>', '*', '_', '~':
			b.WriteRune('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isHashtagRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// ---------------------------------------------------------------- backend de publication

type linkedinBackend struct {
	cfg     liConfig
	hc      *http.Client
	now     func() time.Time
	tok     *liToken // nil : compte non connecté
	author  string   // urn:li:person:… enregistré dans le compte
	account string
}

func (s *Studio) newLinkedInBackend(a *Account) *linkedinBackend {
	return newLinkedInBackend(a, s.liTokens(), s.liCfg(), s.now)
}

func newLinkedInBackend(a *Account, tokens liTokens, cfg liConfig, now func() time.Time) *linkedinBackend {
	cfg.defaults()
	return &linkedinBackend{cfg: cfg, hc: &http.Client{Timeout: 30 * time.Second}, now: now,
		tok: tokens.get(a.Name), author: a.AuthorURN, account: a.Name}
}

func (b *linkedinBackend) Name() string { return "linkedin" }

// usable renvoie l'erreur de jeton (absent, expiré) avant tout appel réseau.
func (b *linkedinBackend) usable() error {
	if b.tok == nil || b.tok.AccessToken == "" {
		return errLinkedInDisconnected
	}
	if !b.tok.ExpiresAt.After(b.now()) {
		return errLinkedInExpired
	}
	return nil
}

// liAPIError : erreur LinkedIn sans en-tête ni secret.
func (b *linkedinBackend) apiError(what string, status int, raw []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	msg := strings.TrimSpace(e.Message)
	if b.tok != nil && b.tok.AccessToken != "" {
		msg = strings.ReplaceAll(msg, b.tok.AccessToken, "[jeton]")
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	if msg == "" {
		return fmt.Errorf("LinkedIn %s : HTTP %d", what, status)
	}
	return fmt.Errorf("LinkedIn %s : HTTP %d %s", what, status, msg)
}

var errLinkedInNetwork = errors.New("LinkedIn injoignable ou délai dépassé : vérifiez sur LinkedIn si le post est parti avant de le revalider")

func (b *linkedinBackend) request(ctx context.Context, method, url string, body io.Reader, contentType string, rest bool) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, nil, errors.New("LinkedIn : requête invalide")
	}
	req.Header.Set("Authorization", "Bearer "+b.tok.AccessToken)
	if rest {
		req.Header.Set("Linkedin-Version", b.cfg.APIVersion)
		req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return nil, nil, errLinkedInNetwork
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp, raw, nil
}

// Me lit l'identité du membre derrière le jeton et la compare à celle
// enregistrée (jeton, compte) : un écart annule l'envoi.
func (b *linkedinBackend) Me(ctx context.Context) (*Profile, error) {
	if err := b.usable(); err != nil {
		return nil, err
	}
	resp, raw, err := b.request(ctx, "GET", b.cfg.APIBase+"/v2/userinfo", nil, "", false)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, b.apiError("identité", resp.StatusCode, raw)
	}
	var u struct {
		Sub  string `json:"sub"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &u) != nil || u.Sub == "" {
		return nil, errors.New("LinkedIn : identité du membre illisible")
	}
	if u.Sub != b.tok.PersonID || (b.author != "" && b.author != "urn:li:person:"+u.Sub) {
		return nil, errors.New("envoi annulé : le jeton correspond à un autre membre LinkedIn que celui du compte")
	}
	return &Profile{ID: u.Sub, Name: u.Name}, nil
}

func (b *linkedinBackend) Post(ctx context.Context, r PostRequest) (string, error) {
	if strings.TrimSpace(r.ValidatedBy) == "" {
		return "", errors.New("envoi LinkedIn refusé : post non validé")
	}
	if r.ReplyTo != "" || r.QuoteOf != "" {
		return "", errors.New("envoi LinkedIn refusé : les réponses et commentaires se postent à la main")
	}
	if err := b.usable(); err != nil {
		return "", err
	}
	if !strings.HasPrefix(b.author, "urn:li:person:") {
		return "", errors.New("LinkedIn : compte non connecté (auteur inconnu)")
	}
	var imageURN string
	if r.MediaPath != "" {
		urn, err := b.uploadImage(ctx, r.MediaPath)
		if err != nil {
			return "", err
		}
		imageURN = urn
	}
	body := map[string]any{
		"author":                    b.author,
		"commentary":                escapeLittle(r.Text),
		"visibility":                "PUBLIC",
		"distribution":              map[string]any{"feedDistribution": "MAIN_FEED"},
		"lifecycleState":            "PUBLISHED",
		"isReshareDisabledByAuthor": false,
	}
	if imageURN != "" {
		media := map[string]any{"id": imageURN}
		if r.MediaAlt != "" {
			media["altText"] = r.MediaAlt
		}
		body["content"] = map[string]any{"media": media}
	}
	buf, _ := json.Marshal(body)
	resp, raw, err := b.request(ctx, "POST", b.cfg.APIBase+"/rest/posts", bytes.NewReader(buf), "application/json", true)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusCreated {
		return "", b.apiError("publication", resp.StatusCode, raw)
	}
	// URN absent d'une réponse 201 : le post est parti, on ne le met pas en échec
	return resp.Header.Get("x-restli-id"), nil
}

// readLinkedInImage lit un visuel et vérifie ce que LinkedIn accepte : JPG,
// PNG ou GIF, 10 Mo au plus (garde-fou du studio) et moins de 36 152 320 pixels.
// Utilisé à l'enregistrement du visuel et juste avant l'envoi.
func readLinkedInImage(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return nil, errors.New("visuel illisible : le fichier est introuvable sur le serveur")
	}
	if st.Size() > linkedinMaxImageBytes {
		return nil, errors.New("visuel trop lourd (10 Mo maximum)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("visuel illisible")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "gif") {
		return nil, errors.New("visuel refusé : JPG, PNG ou GIF attendu")
	}
	if cfg.Width*cfg.Height >= linkedinMaxPixels {
		return nil, errors.New("visuel refusé : image trop grande pour LinkedIn")
	}
	return data, nil
}

// uploadImage : initializeUpload, envoi binaire, puis l'URN de l'image.
func (b *linkedinBackend) uploadImage(ctx context.Context, path string) (string, error) {
	data, err := readLinkedInImage(path)
	if err != nil {
		return "", err
	}
	init, _ := json.Marshal(map[string]any{"initializeUploadRequest": map[string]any{"owner": b.author}})
	resp, raw, err := b.request(ctx, "POST", b.cfg.APIBase+"/rest/images?action=initializeUpload", bytes.NewReader(init), "application/json", true)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", b.apiError("préparation du visuel", resp.StatusCode, raw)
	}
	var out struct {
		Value struct {
			UploadURL string `json:"uploadUrl"`
			Image     string `json:"image"`
		} `json:"value"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Value.UploadURL == "" || out.Value.Image == "" {
		return "", errors.New("LinkedIn : réponse de préparation du visuel illisible")
	}
	if !b.uploadURLOK(out.Value.UploadURL) {
		return "", errors.New("LinkedIn : adresse d'envoi du visuel refusée")
	}
	resp, raw, err = b.request(ctx, "PUT", out.Value.UploadURL, bytes.NewReader(data), "application/octet-stream", false)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", b.apiError("envoi du visuel", resp.StatusCode, raw)
	}
	return out.Value.Image, nil
}

// uploadURLOK : le jeton ne part que vers un hôte LinkedIn en HTTPS (le faux
// serveur des tests est admis par liConfig.insecure).
func (b *linkedinBackend) uploadURLOK(raw string) bool {
	u, err := neturl.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	if b.cfg.insecure {
		return true
	}
	h := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (strings.HasSuffix(h, ".linkedin.com") || strings.HasSuffix(h, ".licdn.com") || h == "linkedin.com")
}

// Lecture : rien n'est lisible avec w_member_social seul.
func (b *linkedinBackend) Search(context.Context, string, int) ([]Tweet, error) {
	return nil, errNotSupported
}
func (b *linkedinBackend) UserPosts(context.Context, string, int) ([]Tweet, error) {
	return nil, errNotSupported
}
func (b *linkedinBackend) Bookmarks(context.Context, int) ([]Tweet, error) {
	return nil, errNotSupported
}
func (b *linkedinBackend) Tweet(context.Context, string) (*Tweet, error) {
	return nil, errNotSupported
}

// ---------------------------------------------------------------- OAuth

type liPending struct {
	account, session string
	expires          time.Time
}

// liOAuth garde en mémoire les « state » en cours (usage unique, 10 minutes).
type liOAuth struct {
	mu     sync.Mutex
	states map[string]liPending
}

func newState() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (o *liOAuth) issue(account, session string, now time.Time) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.states == nil {
		o.states = map[string]liPending{}
	}
	for k, p := range o.states {
		if !p.expires.After(now) {
			delete(o.states, k)
		}
	}
	st := newState()
	o.states[st] = liPending{account: account, session: session, expires: now.Add(linkedinStateTTL)}
	return st
}

// consume : usage unique ; vide si inconnu, expiré ou lié à une autre session.
func (o *liOAuth) consume(state, session string, now time.Time) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	p, ok := o.states[state]
	if !ok {
		return "", false
	}
	delete(o.states, state)
	if !p.expires.After(now) || p.session != session {
		return "", false
	}
	return p.account, true
}

// ConnectURL : l'adresse d'autorisation LinkedIn pour un compte.
func (s *Studio) linkedinConnectURL(oa *liOAuth, account, session string) (string, error) {
	cfg := s.liCfg()
	if !cfg.configured() {
		return "", errLinkedInNotConfigured
	}
	a, _, err := s.accountAndData(account)
	if err != nil {
		return "", err
	}
	if !isLinkedIn(a) {
		return "", errors.New("ce compte n'est pas un compte LinkedIn")
	}
	q := neturl.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", cfg.RedirectURI)
	q.Set("state", oa.issue(a.Name, session, s.now()))
	q.Set("scope", linkedinScope)
	return cfg.AuthURL + "?" + q.Encode(), nil
}

// linkedinExchange échange le code, lit l'identité et enregistre le jeton.
// Les erreurs sont des catégories fixes : jamais de corps de réponse brut.
func (s *Studio) linkedinExchange(ctx context.Context, account, code string) error {
	cfg := s.liCfg()
	if !cfg.configured() {
		return errLinkedInNotConfigured
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	form := neturl.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {cfg.ClientID},
		"client_secret": {cfg.ClientSecret}, "redirect_uri": {cfg.RedirectURI}}
	req, _ := http.NewRequestWithContext(ctx, "POST", cfg.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return errors.New("échange du code impossible : LinkedIn injoignable")
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	var tk struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if resp.StatusCode >= 300 || json.Unmarshal(raw, &tk) != nil || tk.AccessToken == "" {
		return errors.New("échange du code refusé par LinkedIn")
	}
	if tk.ExpiresIn <= 0 {
		tk.ExpiresIn = int64(linkedinTokenLifetime / time.Second)
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", cfg.APIBase+"/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tk.AccessToken)
	resp, err = hc.Do(req)
	if err != nil {
		return errors.New("lecture du profil impossible : LinkedIn injoignable")
	}
	raw, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	var u struct {
		Sub        string `json:"sub"`
		Name       string `json:"name"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
	}
	if resp.StatusCode >= 300 || json.Unmarshal(raw, &u) != nil || u.Sub == "" {
		return errors.New("lecture du profil refusée par LinkedIn")
	}
	name := strings.TrimSpace(u.Name)
	if name == "" {
		name = strings.TrimSpace(u.GivenName + " " + u.FamilyName)
	}
	author := "urn:li:person:" + u.Sub
	a, _, err := s.accountAndData(account)
	if err != nil {
		return err
	}
	if a.AuthorURN != "" && a.AuthorURN != author {
		return errLinkedInIdentity
	}
	now := s.now().UTC()
	exp := now.Add(time.Duration(tk.ExpiresIn) * time.Second)
	if err := s.liTokens().update(func(f *liFile) {
		f.Accounts[liKey(account)] = &liToken{AccessToken: tk.AccessToken, ExpiresAt: exp, PersonID: u.Sub, DisplayName: name, ConnectedAt: now}
	}); err != nil {
		return errors.New("stockage : enregistrement du jeton impossible")
	}
	err = s.store.Update(func(d *Data) error {
		acc := d.Account(account)
		if acc == nil {
			return fmt.Errorf("compte %q inconnu", account)
		}
		acc.AuthorURN, acc.Backend = author, "linkedin"
		d.AddLog("info", "compte LinkedIn %s connecté (jeton valable jusqu'au %s)", acc.Name, exp.Format("02/01/2006"))
		return nil
	})
	if err != nil {
		return errors.New("stockage : enregistrement du compte impossible")
	}
	return nil
}

var errLinkedInIdentity = errors.New("ce compte est déjà lié à un autre membre LinkedIn")

// registerLinkedIn branche les routes de connexion : /api/linkedin/connect,
// /api/linkedin/status et /auth/linkedin/callback.
func registerLinkedIn(mux *http.ServeMux, st *Studio, gate *authGate) {
	oa := &liOAuth{}
	session := func(r *http.Request) string {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			return sha(c.Value)
		}
		return ""
	}
	mux.HandleFunc("GET /api/linkedin/connect", func(w http.ResponseWriter, r *http.Request) {
		u, err := st.linkedinConnectURL(oa, r.URL.Query().Get("account"), session(r))
		if err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, map[string]string{"url": u}, nil)
	})
	mux.HandleFunc("GET /api/linkedin/status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"configured": st.liCfg().configured(), "accounts": st.linkedinStates(st.store.Snapshot())}, nil)
	})
	mux.HandleFunc("GET /auth/linkedin/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		q := r.URL.Query()
		if !gate.authenticated(r) {
			// Le cookie de session est SameSite=Strict : il n'accompagne pas la
			// redirection venue de linkedin.com. Une page intermédiaire du studio
			// refait la navigation depuis notre propre site, cookie compris.
			if q.Get("hop") != "1" {
				q.Set("hop", "1")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=%s"><title>Connexion LinkedIn</title><p>Connexion en cours…</p>`,
					html.EscapeString("/auth/linkedin/callback?"+q.Encode()))
				return
			}
			http.Error(w, "connexion requise", http.StatusUnauthorized)
			return
		}
		account, ok := oa.consume(q.Get("state"), session(r), st.now())
		if !ok {
			http.Error(w, "état de connexion invalide ou expiré", http.StatusUnauthorized)
			return
		}
		fail := func(kind string, msg string) {
			_ = st.store.Update(func(d *Data) error {
				d.AddLog("error", "connexion LinkedIn du compte %s : %s", account, msg)
				return nil
			})
			http.Redirect(w, r, "/?linkedin="+kind, http.StatusFound)
		}
		if e := q.Get("error"); e != "" {
			if strings.HasPrefix(e, "user_cancelled") {
				http.Redirect(w, r, "/?linkedin=cancelled", http.StatusFound)
				return
			}
			fail("refused", "autorisation refusée par LinkedIn")
			return
		}
		code := q.Get("code")
		if code == "" {
			fail("error", "réponse de LinkedIn sans code")
			return
		}
		if err := st.linkedinExchange(r.Context(), account, code); err != nil {
			kind := "error"
			if errors.Is(err, errLinkedInIdentity) {
				kind = "identity"
			}
			fail(kind, err.Error())
			return
		}
		http.Redirect(w, r, "/?linkedin=connected", http.StatusFound)
	})
}

// ---------------------------------------------------------------- publication à la main

// SendToManual : « Publier à la main » : un post LinkedIn déjà validé (signé)
// passe en « à publier à la main », par exemple quand le jeton a expiré.
// Rien n'est envoyé et aucune validation n'est créée.
func (s *Studio) SendToManual(id string) (*Post, error) {
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		a := d.Account(p.Account)
		if !isLinkedIn(a) {
			return errors.New("seul un post LinkedIn peut être publié à la main")
		}
		if p.Status != StatusScheduled && p.Status != StatusValidated {
			return fmt.Errorf("le post %s est %s : seul un post validé peut être publié à la main", id, p.Status)
		}
		if p.ValidatedBy == "" || p.ValidatedAt == nil {
			return fmt.Errorf("le post %s n'a pas été validé", id)
		}
		p.Status, p.Error, p.UpdatedAt = StatusManual, errManualPublish.Error(), s.now().UTC()
		d.AddLog("info", "post %s (LinkedIn, %s) passé en publication à la main", id, p.Line)
		out = p
		return nil
	})
	return out, err
}
