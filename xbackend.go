package main

// Accès à X. Trois backends interchangeables par compte :
//   - agentx : pilote le binaire `agentx` (github.com/DezXBT/AgentX) et lit son
//     enveloppe JSON. Utilise la session navigateur (cookies) du compte.
//   - xapi   : API officielle X v2 avec un jeton OAuth 2.0 utilisateur.
//   - dryrun : ne publie rien, journalise et renvoie de faux identifiants.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Tweet est la forme commune renvoyée par tous les backends (calquée sur
// models.Tweet d'AgentX).
type Tweet struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt,omitempty"`
	Author    struct {
		ScreenName string `json:"screenName"`
		Name       string `json:"name"`
		Followers  int    `json:"followersCount,omitempty"` // 0 = inconnu
	} `json:"author"`
	Metrics struct {
		Likes     int `json:"likes"`
		Retweets  int `json:"retweets"`
		Replies   int `json:"replies"`
		Quotes    int `json:"quotes"`
		Views     int `json:"views"`
		Bookmarks int `json:"bookmarks"`
	} `json:"metrics"`
	IsRetweet bool `json:"isRetweet,omitempty"`
	IsReply   bool `json:"isReply,omitempty"`
}

type PostRequest struct {
	Text      string
	ReplyTo   string
	QuoteOf   string
	MediaPath string
	MediaAlt  string // LinkedIn : texte alternatif de l'image
	// ValidatedBy : signature de la validation. Le backend LinkedIn refuse
	// tout envoi sans elle (troisième barrière, après Validate et PublishDue).
	ValidatedBy string
}

// Profile est le compte X réellement connecté derrière un backend.
type Profile struct {
	ID             string `json:"id,omitempty"` // LinkedIn : identifiant du membre
	ScreenName     string `json:"screenName"`
	Name           string `json:"name"`
	FollowersCount int    `json:"followersCount"`
	FollowingCount int    `json:"followingCount"`
	TweetsCount    int    `json:"tweetsCount"`
}

type XBackend interface {
	Name() string
	Me(ctx context.Context) (*Profile, error)
	Post(ctx context.Context, r PostRequest) (string, error)
	Search(ctx context.Context, query string, n int) ([]Tweet, error)
	UserPosts(ctx context.Context, handle string, n int) ([]Tweet, error)
	Bookmarks(ctx context.Context, n int) ([]Tweet, error)
	Tweet(ctx context.Context, id string) (*Tweet, error)
}

func BackendFor(a *Account) (XBackend, error) {
	if isLinkedIn(a) {
		switch a.Backend {
		case "manual":
			return manualBackend{}, nil
		case "dryrun", "":
			return &dryrunBackend{linkedin: true}, nil
		case "linkedin":
			return newLinkedInBackend(a, liTokens{dir: homeDir()}, liConfigFromEnv(), time.Now), nil
		}
		return nil, fmt.Errorf("backend %q inconnu pour LinkedIn (linkedin, manual ou dryrun)", a.Backend)
	}
	switch a.Backend {
	case "agentx":
		return &agentxBackend{bin: agentxBin(), account: a.AgentxAccount}, nil
	case "xapi":
		env := a.XAPITokenEnv
		if env == "" {
			env = "X_USER_TOKEN"
		}
		tok := os.Getenv(env)
		if tok == "" {
			return nil, fmt.Errorf("compte %s : la variable %s (jeton OAuth2 utilisateur X) est vide", a.Name, env)
		}
		return &xapiBackend{token: tok, hc: &http.Client{Timeout: 30 * time.Second}}, nil
	case "dryrun", "":
		return &dryrunBackend{}, nil
	}
	return nil, fmt.Errorf("backend inconnu %q (agentx, xapi ou dryrun)", a.Backend)
}

// ---------------------------------------------------------------- agentx

// agentxBin cherche agentx : $AGENTX_BIN, puis à côté du binaire xgrowth
// (même dossier ou bin/), puis dans le PATH.
func agentxBin() string {
	if b := os.Getenv("AGENTX_BIN"); b != "" {
		return b
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, c := range []string{filepath.Join(dir, "agentx"), filepath.Join(dir, "bin", "agentx")} {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c
			}
		}
	}
	return "agentx"
}

type agentxBackend struct {
	bin     string
	account string
}

func (b *agentxBackend) Name() string { return "agentx" }

type agentxEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (b *agentxBackend) run(ctx context.Context, out any, args ...string) error {
	return b.runEnv(ctx, out, nil, args...)
}

func (b *agentxBackend) runEnv(ctx context.Context, out any, env []string, args ...string) error {
	sub := args[0]
	if b.account != "" {
		args = append([]string{"-a", b.account}, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.bin, args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	// le processus a tourné (délai dépassé, tué, sortie sans réponse) : un envoi
	// a pu partir. Un échec au lancement n'a rien envoyé.
	var exitErr *exec.ExitError
	ran := ctx.Err() != nil || errors.As(runErr, &exitErr) || runErr == nil
	if errors.Is(runErr, exec.ErrNotFound) {
		return fmt.Errorf("binaire agentx introuvable (%s) : compilez github.com/DezXBT/AgentX ou définissez AGENTX_BIN", b.bin)
	}
	var resp agentxEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		var e error
		if runErr != nil {
			e = fmt.Errorf("agentx %s : %v %s", sub, runErr, strings.TrimSpace(stderr.String()))
		} else {
			e = fmt.Errorf("agentx %s : réponse non JSON", sub)
		}
		if ran {
			return markUncertain(e)
		}
		return e
	}
	if !resp.OK {
		if resp.Error != nil {
			return fmt.Errorf("agentx %s : %s (%s)", sub, resp.Error.Message, resp.Error.Code)
		}
		return fmt.Errorf("agentx %s : échec", sub)
	}
	if out != nil {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

func (b *agentxBackend) Post(ctx context.Context, r PostRequest) (string, error) {
	var args []string
	switch {
	case r.ReplyTo != "":
		args = []string{"reply", r.ReplyTo, r.Text}
	case r.QuoteOf != "":
		args = []string{"quote", r.QuoteOf, r.Text}
	default:
		args = []string{"post", r.Text}
	}
	if r.MediaPath != "" {
		args = append(args, "--media", r.MediaPath)
	}
	var t Tweet
	if err := b.run(ctx, &t, args...); err != nil {
		return "", err
	}
	if t.ID == "" {
		return "", markUncertain(errors.New("agentx n'a pas renvoyé d'identifiant de post"))
	}
	return t.ID, nil
}

func (b *agentxBackend) Me(ctx context.Context) (*Profile, error) {
	var p Profile
	if err := b.run(ctx, &p, "me"); err != nil {
		return nil, err
	}
	return &p, nil
}

func (b *agentxBackend) Search(ctx context.Context, q string, n int) ([]Tweet, error) {
	var ts []Tweet
	return ts, b.run(ctx, &ts, "search", "--type", "Latest", "-n", fmt.Sprint(n), q)
}

func (b *agentxBackend) UserPosts(ctx context.Context, handle string, n int) ([]Tweet, error) {
	var ts []Tweet
	return ts, b.run(ctx, &ts, "posts", strings.TrimPrefix(handle, "@"), "-n", fmt.Sprint(n))
}

func (b *agentxBackend) Bookmarks(ctx context.Context, n int) ([]Tweet, error) {
	var ts []Tweet
	return ts, b.run(ctx, &ts, "bookmarks", "-n", fmt.Sprint(n))
}

func (b *agentxBackend) Tweet(ctx context.Context, id string) (*Tweet, error) {
	var t Tweet
	if err := b.run(ctx, &t, "tweet", id); err != nil {
		return nil, err
	}
	return &t, nil
}

// ---------------------------------------------------------------- API officielle X v2

type xapiBackend struct {
	token string
	hc    *http.Client
	me    string
}

func (b *xapiBackend) Name() string { return "xapi" }

const xapiBase = "https://api.x.com/2"

func (b *xapiBackend) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, xapiBase+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &xapiHTTPError{status: resp.StatusCode, msg: fmt.Sprintf("API X %s %s : HTTP %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type xapiTweet struct {
	ID            string `json:"id"`
	Text          string `json:"text"`
	AuthorID      string `json:"author_id"`
	CreatedAt     string `json:"created_at"`
	PublicMetrics struct {
		Like       int `json:"like_count"`
		Retweet    int `json:"retweet_count"`
		Reply      int `json:"reply_count"`
		Quote      int `json:"quote_count"`
		Impression int `json:"impression_count"`
		Bookmark   int `json:"bookmark_count"`
	} `json:"public_metrics"`
}

type xapiList struct {
	Data     []xapiTweet `json:"data"`
	Includes struct {
		Users []struct {
			ID            string `json:"id"`
			Username      string `json:"username"`
			Name          string `json:"name"`
			PublicMetrics struct {
				Followers int `json:"followers_count"`
			} `json:"public_metrics"`
		} `json:"users"`
	} `json:"includes"`
}

const xapiFields = "tweet.fields=created_at,public_metrics,author_id&expansions=author_id&user.fields=username,name,public_metrics"

func (l xapiList) tweets() []Tweet {
	users := map[string][2]string{}
	followers := map[string]int{}
	for _, u := range l.Includes.Users {
		users[u.ID] = [2]string{u.Username, u.Name}
		followers[u.ID] = u.PublicMetrics.Followers
	}
	out := make([]Tweet, 0, len(l.Data))
	for _, x := range l.Data {
		t := x.toTweet(users[x.AuthorID])
		t.Author.Followers = followers[x.AuthorID]
		out = append(out, t)
	}
	return out
}

func (x xapiTweet) toTweet(user [2]string) Tweet {
	var t Tweet
	t.ID, t.Text, t.CreatedAt = x.ID, x.Text, x.CreatedAt
	t.Author.ScreenName, t.Author.Name = user[0], user[1]
	t.Metrics.Likes = x.PublicMetrics.Like
	t.Metrics.Retweets = x.PublicMetrics.Retweet
	t.Metrics.Replies = x.PublicMetrics.Reply
	t.Metrics.Quotes = x.PublicMetrics.Quote
	t.Metrics.Views = x.PublicMetrics.Impression
	t.Metrics.Bookmarks = x.PublicMetrics.Bookmark
	return t
}

func (b *xapiBackend) Post(ctx context.Context, r PostRequest) (string, error) {
	if r.MediaPath != "" {
		return "", errors.New("backend xapi : l'envoi de visuels n'est pas pris en charge, utilisez agentx ou publiez sans visuel")
	}
	body := map[string]any{"text": r.Text}
	if r.ReplyTo != "" {
		body["reply"] = map[string]any{"in_reply_to_tweet_id": r.ReplyTo}
	}
	if r.QuoteOf != "" {
		body["quote_tweet_id"] = r.QuoteOf
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := b.do(ctx, "POST", "/tweets", body, &out); err != nil {
		if xapiPostMaybeSent(err) {
			return "", markUncertain(err)
		}
		return "", err
	}
	return out.Data.ID, nil
}

func (b *xapiBackend) Me(ctx context.Context) (*Profile, error) {
	var out struct {
		Data struct {
			Username      string `json:"username"`
			Name          string `json:"name"`
			PublicMetrics struct {
				Followers int `json:"followers_count"`
				Following int `json:"following_count"`
				Tweets    int `json:"tweet_count"`
			} `json:"public_metrics"`
		} `json:"data"`
	}
	if err := b.do(ctx, "GET", "/users/me?user.fields=public_metrics", nil, &out); err != nil {
		return nil, err
	}
	m := out.Data.PublicMetrics
	return &Profile{ScreenName: out.Data.Username, Name: out.Data.Name, FollowersCount: m.Followers, FollowingCount: m.Following, TweetsCount: m.Tweets}, nil
}

func clampXAPI(n int) int {
	if n < 10 {
		return 10
	}
	if n > 100 {
		return 100
	}
	return n
}

func (b *xapiBackend) Search(ctx context.Context, q string, n int) ([]Tweet, error) {
	var l xapiList
	err := b.do(ctx, "GET", fmt.Sprintf("/tweets/search/recent?query=%s&max_results=%d&%s", url.QueryEscape(q), clampXAPI(n), xapiFields), nil, &l)
	return l.tweets(), err
}

func (b *xapiBackend) userID(ctx context.Context, handle string) (string, error) {
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	path := "/users/by/username/" + url.PathEscape(strings.TrimPrefix(handle, "@"))
	if handle == "" {
		path = "/users/me"
	}
	err := b.do(ctx, "GET", path, nil, &out)
	return out.Data.ID, err
}

func (b *xapiBackend) UserPosts(ctx context.Context, handle string, n int) ([]Tweet, error) {
	id, err := b.userID(ctx, handle)
	if err != nil {
		return nil, err
	}
	var l xapiList
	err = b.do(ctx, "GET", fmt.Sprintf("/users/%s/tweets?max_results=%d&%s", id, clampXAPI(n), xapiFields), nil, &l)
	return l.tweets(), err
}

func (b *xapiBackend) Bookmarks(ctx context.Context, n int) ([]Tweet, error) {
	if b.me == "" {
		id, err := b.userID(ctx, "")
		if err != nil {
			return nil, err
		}
		b.me = id
	}
	var l xapiList
	err := b.do(ctx, "GET", fmt.Sprintf("/users/%s/bookmarks?max_results=%d&%s", b.me, clampXAPI(n), xapiFields), nil, &l)
	return l.tweets(), err
}

func (b *xapiBackend) Tweet(ctx context.Context, id string) (*Tweet, error) {
	var out struct {
		Data xapiTweet `json:"data"`
	}
	if err := b.do(ctx, "GET", "/tweets/"+url.PathEscape(id)+"?tweet.fields=created_at,public_metrics,author_id", nil, &out); err != nil {
		return nil, err
	}
	t := out.Data.toTweet([2]string{})
	return &t, nil
}

// ---------------------------------------------------------------- dryrun

var dryCounter atomic.Int64

type dryrunBackend struct{ linkedin bool }

func (dryrunBackend) Name() string { return "dryrun" }
func (dryrunBackend) Me(context.Context) (*Profile, error) {
	return &Profile{ScreenName: "", Name: "simulation"}, nil
}
func (b dryrunBackend) Post(_ context.Context, r PostRequest) (string, error) {
	n := dryCounter.Add(1)
	if b.linkedin {
		return fmt.Sprintf("urn:li:share:dryrun-%d", n), nil
	}
	return fmt.Sprintf("dry-%d-%d", time.Now().Unix(), n), nil
}
func (dryrunBackend) Search(context.Context, string, int) ([]Tweet, error)    { return nil, nil }
func (dryrunBackend) UserPosts(context.Context, string, int) ([]Tweet, error) { return nil, nil }
func (dryrunBackend) Bookmarks(context.Context, int) ([]Tweet, error)         { return nil, nil }
func (dryrunBackend) Tweet(_ context.Context, id string) (*Tweet, error) {
	return &Tweet{ID: id}, nil
}

// ---------------------------------------------------------------- manual (LinkedIn)

// errManualPublish : le backend manuel n'envoie rien ; PublishDue range le post
// en « manual » (à publier à la main), jamais en « failed ».
var errManualPublish = errors.New("à publier à la main sur LinkedIn")

// manualBackend ne contacte aucun service : il sert de repli (jeton expiré) et
// de mode d'essai sûr. Seul Post est utile, et il renvoie toujours errManualPublish.
type manualBackend struct{}

func (manualBackend) Name() string { return "manual" }
func (manualBackend) Me(context.Context) (*Profile, error) {
	return &Profile{Name: "publication manuelle"}, nil
}
func (manualBackend) Post(context.Context, PostRequest) (string, error) {
	return "", errManualPublish
}
func (manualBackend) Search(context.Context, string, int) ([]Tweet, error)    { return nil, nil }
func (manualBackend) UserPosts(context.Context, string, int) ([]Tweet, error) { return nil, nil }
func (manualBackend) Bookmarks(context.Context, int) ([]Tweet, error)         { return nil, nil }
func (manualBackend) Tweet(_ context.Context, id string) (*Tweet, error) {
	return nil, errors.New("LinkedIn : lecture indisponible")
}

// parseTweetID accepte un identifiant ou une URL x.com/twitter.com.
func parseTweetID(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/status/"); i >= 0 {
		s = s[i+len("/status/"):]
		if j := strings.IndexAny(s, "/?#"); j >= 0 {
			s = s[:j]
		}
	}
	return s
}
