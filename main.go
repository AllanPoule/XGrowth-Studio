// xgrowth : studio de croissance pour X piloté par l'IA.
//
// Comme agentx, chaque commande imprime UNE enveloppe JSON :
//
//	{"ok":true,"schema_version":"1","data":...}
//	{"ok":false,"schema_version":"1","error":{"code":"...","message":"..."}}
//
// ce qui permet à n'importe quel agent de piloter le studio. La publication
// reste soumise à une validation humaine signée (`validate --by`).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Une version par pull request fusionnée (voir CHANGELOG.md).
const version = "0.05"

var pretty bool

type envelope struct {
	OK            bool      `json:"ok"`
	SchemaVersion string    `json:"schema_version"`
	Data          any       `json:"data,omitempty"`
	Error         *apiError `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func emit(data any) {
	writeEnvelope(envelope{OK: true, SchemaVersion: "1", Data: data})
	os.Exit(0)
}

func fail(code string, err error) {
	writeEnvelope(envelope{OK: false, SchemaVersion: "1", Error: &apiError{Code: code, Message: err.Error()}})
	os.Exit(1)
}

func writeEnvelope(e envelope) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent("", "  ")
	}
	_ = enc.Encode(e)
}

// ---------------------------------------------------------------- analyse des arguments

var boolFlags = map[string]bool{
	"pretty": true, "now": true, "quote": true, "draft": true, "premium": true, "auto-triage": true,
	"auto-draft": true, "no-scheduler": true, "help": true, "no-auth": true,
	"rules-auto-accept": true, "auto-synthesis": true,
}

type args struct {
	pos   []string
	flags map[string]string
}

func parseArgs(in []string) args {
	a := args{flags: map[string]string{}}
	for i := 0; i < len(in); i++ {
		s := in[i]
		if s == "--" {
			a.pos = append(a.pos, in[i+1:]...)
			break
		}
		if strings.HasPrefix(s, "-") && len(s) > 1 && !isNumber(s) {
			name := strings.TrimLeft(s, "-")
			if k, v, ok := strings.Cut(name, "="); ok {
				a.flags[k] = v
				continue
			}
			if name == "n" {
				name = "count"
			}
			if name == "a" {
				name = "account"
			}
			if boolFlags[name] {
				a.flags[name] = "true"
				continue
			}
			if i+1 < len(in) {
				a.flags[name] = in[i+1]
				i++
			} else {
				a.flags[name] = ""
			}
			continue
		}
		a.pos = append(a.pos, s)
	}
	return a
}

func isNumber(s string) bool { _, err := strconv.Atoi(s); return err == nil }

func (a args) has(k string) bool     { _, ok := a.flags[k]; return ok }
func (a args) str(k string) string   { return a.flags[k] }
func (a args) boolean(k string) bool { v := a.flags[k]; return v == "true" || v == "1" || v == "yes" }
func (a args) arg(i int) string {
	if i < len(a.pos) {
		return a.pos[i]
	}
	return ""
}
func (a args) rest(i int) string {
	if i < len(a.pos) {
		return strings.Join(a.pos[i:], " ")
	}
	return ""
}
func (a args) integer(k string, def int) int {
	if v, err := strconv.Atoi(a.flags[k]); err == nil {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func need(a args, i int, usage string) string {
	v := a.arg(i)
	if v == "" {
		fail("invalid_input", fmt.Errorf("usage : xgrowth %s", usage))
	}
	return v
}

// ---------------------------------------------------------------- main

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usageText)
		os.Exit(0)
	}
	cmd := os.Args[1]
	a := parseArgs(os.Args[2:])
	pretty = a.boolean("pretty")
	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		fmt.Print(usageText)
		return
	}
	if cmd == "version" {
		emit(map[string]string{"version": version})
	}

	store, err := OpenStore(homeDir())
	if err != nil {
		fail("store", err)
	}
	st := NewStudio(store)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "init":
		emit(map[string]any{"home": homeDir(), "settings": store.Snapshot().Settings})

	case "account":
		cmdAccount(st, a)

	case "watch":
		cmdWatch(st, a)

	case "fetch":
		emit(st.PollWatch(ctx, a.str("account"), true))

	case "bookmarks":
		r, err := st.ImportBookmarks(ctx, need(a, 0, "bookmarks <compte> [-n 20]"), a.integer("count", 20))
		check(err)
		emit(r)

	case "item":
		switch a.arg(0) {
		case "add":
			it, err := st.AddItem(ctx, need(a, 1, "item add <compte> <url|id>"), need(a, 2, "item add <compte> <url|id>"))
			check(err)
			emit(it)
		case "keep", "ignore":
			status := ItemKept
			if a.arg(0) == "ignore" {
				status = ItemIgnored
			}
			it, err := st.SetItemStatus(need(a, 1, "item keep|ignore <id>"), status)
			check(err)
			emit(it)
		default:
			fail("invalid_input", fmt.Errorf("usage : xgrowth item add|keep|ignore"))
		}

	case "items":
		d := store.Snapshot()
		var out []*Item
		for i := len(d.Items) - 1; i >= 0; i-- {
			it := d.Items[i]
			if (a.str("account") == "" || strings.EqualFold(it.Account, a.str("account"))) && (a.str("status") == "" || it.Status == a.str("status")) {
				out = append(out, it)
			}
			if len(out) >= a.integer("count", 50) {
				break
			}
		}
		emit(out)

	case "triage":
		emit(st.TriagePending(ctx, a.str("account"), false, a.boolean("draft")))

	case "draft":
		acct := need(a, 0, "draft <compte> [--topic \"...\"] [--item id] [-n 3] [--type clé]")
		topic := a.str("topic")
		if topic == "" {
			topic = a.rest(1)
		}
		ps, err := st.GenerateDrafts(ctx, acct, topic, a.str("item"), a.integer("count", 3), a.str("type"))
		check(err)
		emit(ps)

	case "write":
		acct := need(a, 0, "write <compte> <texte> [--reply-to id] [--quote id] [--media fichier]")
		p, err := st.AddDraft(acct, a.rest(1), a.str("reply-to"), a.str("quote-of"), a.str("media"))
		check(err)
		emit(p)

	case "reply":
		acct := need(a, 0, "reply <compte> <url|id> [--quote]")
		p, err := st.DraftReply(ctx, acct, need(a, 1, "reply <compte> <url|id> [--quote]"), a.boolean("quote"))
		check(err)
		emit(p)

	case "posts":
		d := store.Snapshot()
		var out []*Post
		for i := len(d.Posts) - 1; i >= 0; i-- {
			p := d.Posts[i]
			if (a.str("account") == "" || strings.EqualFold(p.Account, a.str("account"))) && (a.str("status") == "" || p.Status == a.str("status")) {
				out = append(out, p)
			}
			if len(out) >= a.integer("count", 50) {
				break
			}
		}
		emit(out)

	case "show":
		d := store.Snapshot()
		p := d.Post(need(a, 0, "show <id>"))
		if p == nil {
			fail("not_found", fmt.Errorf("post inconnu"))
		}
		emit(p)

	case "edit":
		p, err := st.EditPost(need(a, 0, "edit <id> <texte>"), a.rest(1))
		check(err)
		emit(p)

	case "media":
		path := a.arg(1)
		if path == "none" {
			path = ""
		}
		p, err := st.SetMedia(need(a, 0, "media <id> <fichier|none>"), path)
		check(err)
		emit(p)

	case "rewrite":
		fb := a.str("feedback")
		if fb == "" {
			fb = a.rest(1)
		}
		if fb == "" {
			fail("invalid_input", fmt.Errorf("usage : xgrowth rewrite <id> --feedback \"...\""))
		}
		p, err := st.RewritePost(ctx, need(a, 0, "rewrite <id> --feedback \"...\""), fb)
		check(err)
		emit(p)

	case "validate":
		id := need(a, 0, "validate <id> --by <nom> [--at \"2026-11-20 09:00\"] [--now]")
		var at *time.Time
		if a.str("at") != "" {
			d := store.Snapshot()
			p := d.Post(id)
			loc := time.Local
			if p != nil {
				if acc := d.Account(p.Account); acc != nil {
					loc = location(acc)
				}
			}
			t, err := parseWhen(a.str("at"), loc)
			check(err)
			at = &t
		}
		p, err := st.Validate(id, a.str("by"), at, a.boolean("now"))
		check(err)
		emit(p)

	case "reject":
		p, err := st.Reject(need(a, 0, "reject <id> [--reason \"...\"]"), a.str("reason"))
		check(err)
		emit(p)

	case "unschedule":
		p, err := st.Unschedule(need(a, 0, "unschedule <id>"))
		check(err)
		emit(p)

	case "calendar":
		emit(calendar(store.Snapshot(), a.integer("days", 14), time.Now()))

	case "publish-due":
		emit(st.PublishDue(ctx))

	case "metrics":
		n, err := st.RefreshMetrics(ctx, 0)
		res := map[string]any{"updated": n}
		if err != nil {
			res["lastError"] = err.Error()
		}
		emit(res)

	case "learn":
		text := a.rest(0)
		if text == "" {
			fail("invalid_input", fmt.Errorf("usage : xgrowth learn <leçon> [--account compte]"))
		}
		l := Learning{ID: newID("l"), Account: a.str("account"), Text: text, CreatedAt: time.Now().UTC()}
		check(store.Update(func(d *Data) error { d.Learnings = append(d.Learnings, l); return nil }))
		emit(l)

	case "learnings":
		emit(store.Snapshot().Learnings)

	case "settings":
		cmdSettings(store, a)

	case "rules":
		cmdRules(ctx, st, a)

	case "log":
		d := store.Snapshot()
		n := a.integer("count", 50)
		if len(d.Log) > n {
			d.Log = d.Log[len(d.Log)-n:]
		}
		emit(d.Log)

	case "run":
		every := time.Duration(a.integer("every", 60)) * time.Second
		fmt.Fprintf(os.Stderr, "xgrowth : planificateur actif (passage toutes les %s, Ctrl+C pour arrêter)\n", every)
		runScheduler(ctx, st, every, true)

	case "serve":
		addr := a.str("addr")
		if addr == "" {
			addr = "127.0.0.1:8787"
		}
		noAuth := a.boolean("no-auth")
		if noAuth {
			host, _, _ := strings.Cut(addr, ":")
			if host != "127.0.0.1" && host != "localhost" {
				fail("invalid_input", fmt.Errorf("--no-auth n'est permis que sur 127.0.0.1"))
			}
		} else {
			code, err := EnsureSetupCode(store)
			check(err)
			if code != "" {
				fmt.Fprintf(os.Stderr, "xgrowth : premier lancement. Code d'installation : %s (aussi dans %s/setup-code.txt)\n", code, homeDir())
			}
		}
		if !a.boolean("no-scheduler") {
			go runScheduler(ctx, st, time.Duration(a.integer("every", 60))*time.Second, false)
		}
		check(serve(ctx, st, ServeOptions{Addr: addr, Hosts: splitList(a.str("host")), NoAuth: noAuth}))

	case "providers":
		emit(map[string]any{"selected": st.aiSettings().AIProvider, "providers": DetectProviders(true)})

	case "ai-test":
		cfg := st.aiSettings()
		if p := a.str("provider"); p != "" {
			cfg.AIProvider, cfg.Model = p, a.str("model")
		}
		r := TestAI(ctx, cfg)
		if !r.OK {
			writeEnvelope(envelope{OK: false, SchemaVersion: "1", Data: r, Error: &apiError{Code: "ai_failed", Message: r.Error}})
			os.Exit(1)
		}
		emit(r)

	case "password":
		pw := os.Getenv("XGROWTH_NEW_PASSWORD")
		if pw == "" {
			fmt.Fprint(os.Stderr, "Nouveau mot de passe du studio (10 caractères min.) : ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			pw = strings.TrimRight(line, "\r\n")
		}
		check(SetPassword(store, pw))
		emit(map[string]string{"password": "mis à jour, sessions révoquées"})

	default:
		fail("unknown_command", fmt.Errorf("commande inconnue %q (xgrowth help)", cmd))
	}
}

func check(err error) {
	if err != nil {
		fail("error", err)
	}
}

func runScheduler(ctx context.Context, st *Studio, every time.Duration, verbose bool) {
	if every < 10*time.Second {
		every = 10 * time.Second
	}
	tick := func() {
		out := st.Tick(ctx)
		if verbose && len(out) > 1 {
			b, _ := json.Marshal(out)
			fmt.Println(string(b))
		}
	}
	tick()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

func parseWhen(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "+") {
		if d, err := time.ParseDuration(s[1:]); err == nil {
			return time.Now().Add(d), nil
		}
	}
	for _, f := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("date %q illisible (ex. \"2026-11-20 09:00\", RFC3339 ou +2h)", s)
}

type CalendarDay struct {
	Date  string  `json:"date"`
	Posts []*Post `json:"posts"`
}

func calendar(d *Data, days int, now time.Time) []CalendarDay {
	byDay := map[string][]*Post{}
	end := now.Add(time.Duration(days) * 24 * time.Hour)
	for _, p := range d.Posts {
		var t *time.Time
		switch p.Status {
		case StatusScheduled, StatusValidated, StatusPublishing:
			t = p.ScheduledAt
		case StatusPublished:
			t = p.PublishedAt
		}
		if t == nil || t.After(end) || t.Before(now.Add(-24*time.Hour)) {
			continue
		}
		loc := time.Local
		if a := d.Account(p.Account); a != nil {
			loc = location(a)
		}
		k := t.In(loc).Format("2006-01-02")
		byDay[k] = append(byDay[k], p)
	}
	var out []CalendarDay
	for k, ps := range byDay {
		sort.Slice(ps, func(i, j int) bool { return when(ps[i]).Before(when(ps[j])) })
		out = append(out, CalendarDay{Date: k, Posts: ps})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func when(p *Post) time.Time {
	if p.PublishedAt != nil {
		return *p.PublishedAt
	}
	if p.ScheduledAt != nil {
		return *p.ScheduledAt
	}
	return p.CreatedAt
}

// ---------------------------------------------------------------- sous-commandes

func applyAccountFlags(acc *Account, a args) {
	if a.has("handle") {
		acc.Handle = a.str("handle")
	}
	if a.has("backend") {
		acc.Backend = a.str("backend")
	}
	if a.has("agentx-account") {
		acc.AgentxAccount = a.str("agentx-account")
	}
	if a.has("token-env") {
		acc.XAPITokenEnv = a.str("token-env")
	}
	if a.has("premium") {
		acc.Premium = a.boolean("premium")
	}
	if a.has("lang") {
		acc.Language = a.str("lang")
	}
	if a.has("tz") {
		acc.Timezone = a.str("tz")
	}
	if a.has("slots") {
		acc.Slots = splitList(a.str("slots"))
	}
	if a.has("voice") {
		acc.Voice = a.str("voice")
	}
	if a.has("instructions") {
		acc.Instructions = a.str("instructions")
	}
	if a.has("topics") {
		acc.Topics = splitList(a.str("topics"))
	}
	if a.has("triage-rules") {
		acc.TriageRules = a.str("triage-rules")
	}
	if a.has("auto-triage") {
		acc.AutoTriage = a.boolean("auto-triage")
	}
	if a.has("auto-draft") {
		acc.AutoDraft = a.boolean("auto-draft")
	}
	if a.has("daily-limit") {
		acc.DailyLimit = a.integer("daily-limit", acc.DailyLimit)
	}
	if a.has("goals") {
		acc.Goals = a.str("goals")
	}
	if a.has("length") {
		acc.Length = a.str("length")
	}
	if a.has("examples-file") {
		acc.Examples = nil
		if f := a.str("examples-file"); f != "" {
			b, err := os.ReadFile(f)
			check(err)
			acc.Examples = SplitExamples(string(b))
		}
	}
	if a.has("post-types") {
		acc.PostTypes = postTypesFlag(a.str("post-types"))
	}
	applyPromptFlags(&acc.Prompts, a)
}

// postTypesFlag lit --post-types : JSON, @fichier.json, « defaut » ou vide (aucun).
func postTypesFlag(v string) []PostType {
	switch v = strings.TrimSpace(v); {
	case v == "" || v == "[]":
		return nil
	case v == "defaut" || v == "défaut":
		return append([]PostType(nil), DefaultPostTypes...)
	case strings.HasPrefix(v, "@"):
		b, err := os.ReadFile(v[1:])
		check(err)
		v = string(b)
	}
	var out []PostType
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		fail("invalid_input", fmt.Errorf("--post-types : JSON invalide : %w", err))
	}
	return out
}

// applyPromptFlags : consignes par demande (vide = hérite du niveau supérieur).
func applyPromptFlags(p *AIPrompts, a args) {
	for flag, dst := range map[string]*string{"ideas-prompt": &p.Ideas, "item-prompt": &p.Item, "reply-prompt": &p.Reply,
		"quote-prompt": &p.Quote, "rewrite-prompt": &p.Rewrite, "visual-guide": &p.Visual} {
		if a.has(flag) {
			*dst = a.str(flag)
		}
	}
}

func cmdRules(ctx context.Context, st *Studio, a args) {
	switch a.arg(0) {
	case "", "list":
		d := st.store.Snapshot()
		emit(map[string]any{"common": d.Settings.Rules, "learned": sortedRules(d.RuleProposals)})
	case "synthesize":
		r, err := st.Synthesize(ctx, a.str("account"))
		check(err)
		emit(r)
	case "accept", "refuse", "revoke":
		r, err := st.DecideRule(need(a, 1, "rules "+a.arg(0)+" <id>"), a.arg(0), a.str("text"))
		check(err)
		emit(r)
	default:
		fail("invalid_input", fmt.Errorf("usage : xgrowth rules list|synthesize|accept|refuse|revoke"))
	}
}

func cmdAccount(st *Studio, a args) {
	switch a.arg(0) {
	case "add":
		acc := &Account{Name: a.str("name"), Language: "français", Timezone: "Europe/Paris", Slots: []string{"09:00", "12:30", "18:00"}}
		if acc.Name == "" {
			acc.Name = a.arg(1)
		}
		if a.str("backend") == "agentx" && !a.has("agentx-account") {
			acc.AgentxAccount = acc.Name
		}
		applyAccountFlags(acc, a)
		check(st.SaveAccount(acc))
		emit(acc)
	case "set":
		name := need(a, 1, "account set <nom> --option valeur")
		d := st.store.Snapshot()
		acc := d.Account(name)
		if acc == nil {
			fail("not_found", fmt.Errorf("compte %q inconnu", name))
		}
		applyAccountFlags(acc, a)
		check(st.SaveAccount(acc))
		emit(acc)
	case "check":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		r, err := st.CheckAccount(ctx, need(a, 1, "account check <nom>"))
		check(err)
		if !r.OK {
			writeEnvelope(envelope{OK: false, SchemaVersion: "1", Data: r, Error: &apiError{Code: "check_failed", Message: r.Problem}})
			os.Exit(1)
		}
		emit(r)
	case "list", "":
		emit(st.store.Snapshot().Accounts)
	case "show":
		acc := st.store.Snapshot().Account(need(a, 1, "account show <nom>"))
		if acc == nil {
			fail("not_found", fmt.Errorf("compte inconnu"))
		}
		emit(acc)
	case "remove":
		name := need(a, 1, "account remove <nom>")
		check(st.store.Update(func(d *Data) error {
			for i, acc := range d.Accounts {
				if strings.EqualFold(acc.Name, name) {
					d.Accounts = append(d.Accounts[:i], d.Accounts[i+1:]...)
					return nil
				}
			}
			return fmt.Errorf("compte %q inconnu", name)
		}))
		emit(map[string]string{"removed": name})
	default:
		fail("invalid_input", fmt.Errorf("usage : xgrowth account add|set|check|list|show|remove"))
	}
}

func cmdWatch(st *Studio, a args) {
	switch a.arg(0) {
	case "add":
		acct := need(a, 1, "watch add <compte> --user handle | --search \"requête\" [--every 60]")
		kind, value := "user", a.str("user")
		if a.has("search") {
			kind, value = "search", a.str("search")
		}
		w, err := st.AddWatch(acct, kind, value, a.integer("every", 60))
		check(err)
		emit(w)
	case "remove":
		check(st.RemoveWatch(need(a, 1, "watch remove <id>")))
		emit(map[string]string{"removed": a.arg(1)})
	case "list", "":
		type row struct {
			Account string `json:"account"`
			WatchTarget
		}
		var out []row
		for _, acc := range st.store.Snapshot().Accounts {
			for _, w := range acc.Watch {
				out = append(out, row{acc.Name, w})
			}
		}
		emit(out)
	default:
		fail("invalid_input", fmt.Errorf("usage : xgrowth watch add|list|remove"))
	}
}

func cmdSettings(store *Store, a args) {
	check(store.Update(func(d *Data) error {
		s := &d.Settings
		if a.has("provider") {
			switch p := a.str("provider"); p {
			case "anthropic", "claude-code", "codex", "gemini":
				s.AIProvider, s.Model = p, ""
			default:
				return fmt.Errorf("fournisseur %q inconnu : anthropic, claude-code, codex ou gemini", p)
			}
		}
		if a.has("model") {
			s.Model = a.str("model")
		}
		if a.has("effort") {
			s.Effort = a.str("effort")
		}
		if a.has("competitors") {
			s.Competitors = splitList(a.str("competitors"))
		}
		if a.has("banned") {
			s.BannedPhrases = splitList(a.str("banned"))
		}
		if a.has("min-gap") {
			s.MinGapMinutes = a.integer("min-gap", s.MinGapMinutes)
		}
		if a.has("triage-min") {
			if f, err := strconv.ParseFloat(a.str("triage-min"), 64); err == nil {
				s.TriageMinScore = f
			}
		}
		if a.has("max-items") {
			s.MaxItemsPerRun = a.integer("max-items", s.MaxItemsPerRun)
		}
		if a.has("rules") {
			s.Rules = SplitLines(a.str("rules"))
		}
		if a.has("rules-file") {
			s.Rules = nil
			if f := a.str("rules-file"); f != "" {
				b, err := os.ReadFile(f)
				if err != nil {
					return err
				}
				s.Rules = SplitLines(string(b))
			}
		}
		if a.has("post-types") {
			s.PostTypes = postTypesFlag(a.str("post-types"))
		}
		applyPromptFlags(&s.Prompts, a)
		if a.has("rules-auto-accept") {
			s.RulesAutoAccept = a.boolean("rules-auto-accept")
		}
		if a.has("auto-synthesis") {
			s.AutoSynthesis = a.boolean("auto-synthesis")
		}
		return NormalizeSettings(s)
	}))
	emit(store.Snapshot().Settings)
}

const usageText = `xgrowth ` + version + ` — studio de croissance pour X piloté par l'IA

Chaque commande imprime une enveloppe JSON ({"ok":...,"data":...}). --pretty pour la lire.
Données : $XGROWTH_HOME (défaut ~/.xgrowth). IA : Claude Code, Codex, Gemini CLI ou API Claude.
X : agentx (session navigateur), API X officielle ou dryrun (simulation).

COMPTES
  account add <nom> [--handle h] [--backend agentx|xapi|dryrun] [--agentx-account n] [--token-env VAR]
                    [--tz Europe/Paris] [--slots 09:00,12:30,18:00] [--lang français] [--premium]
                    [--voice "..."] [--instructions "..."] [--topics a,b] [--triage-rules "..."]
                    [--auto-triage] [--auto-draft] [--daily-limit 6]
                    [--goals "..."] [--length court|moyen|long] [--examples-file posts.txt]
                    [--post-types JSON|@types.json|defaut] [--ideas-prompt "..."] [--item-prompt "..."]
                    [--reply-prompt "..."] [--quote-prompt "..."] [--rewrite-prompt "..."] [--visual-guide "..."]
  account set <nom> --option valeur      account list | show <nom> | remove <nom>
  account check <nom>                    vérifie la connexion à X et que c'est le bon compte
  settings [--provider claude-code|codex|gemini|anthropic] [--model m] [--effort medium]
           [--competitors a,b] [--banned x,y] [--min-gap 60] [--triage-min 6] [--max-items 40]
           [--rules "une règle par ligne" | --rules-file regles.txt] [--post-types JSON|@types.json|defaut]
           [--ideas-prompt …] [--item-prompt …] [--reply-prompt …] [--quote-prompt …] [--rewrite-prompt …]
           [--visual-guide …] [--rules-auto-accept[=false]] [--auto-synthesis[=false]]
  providers                              IA détectées sur cette machine (Claude Code, Codex, Gemini, API)
  ai-test [--provider p] [--model m]     vérifie qu'une IA répond

VEILLE
  watch add <compte> --user handle | --search "requête" [--every 60]
  watch list | remove <id>
  fetch [--account c]                    interroge toute la veille maintenant
  bookmarks <compte> [-n 20]             importe les signets X
  item add <compte> <url>                ajoute un post X à la veille
  items [--account c] [--status pending|kept|ignored|drafted]
  item keep|ignore <id>
  triage [--account c] [--draft]         tri par Claude (+ brouillons des contenus gardés)

RÉDACTION
  draft <compte> [--topic "..."] [--item id] [-n 3] [--type clé]   brouillons rédigés par l'IA
  reply <compte> <url|id> [--quote]                   réponse (ou citation) rédigée par Claude
  write <compte> <texte> [--reply-to id] [--quote-of id] [--media fichier]   brouillon manuel
  posts [--account c] [--status draft|scheduled|published|failed|rejected]
  show <id> | edit <id> <texte> | media <id> <fichier|none>
  rewrite <id> --feedback "..."          réécriture par Claude (le retour devient une leçon)
  learn <leçon> [--account c] | learnings
  rules [list]                           règles communes et règles apprises
  rules synthesize [--account c]         l'IA tire des règles des retours (rejets, réécritures, leçons)
  rules accept <id> [--text "..."] | refuse <id> | revoke <id>

VALIDATION ET PUBLICATION (rien ne part sans validation signée)
  validate <id> --by <nom> [--at "2026-11-20 09:00" | --at +2h | --now]
  reject <id> [--reason "..."] | unschedule <id>
  calendar [--days 14]
  publish-due                            publie ce qui est dû (un passage)
  metrics                                rafraîchit les statistiques des posts publiés
  run [--every 60]                       planificateur : publication, veille, tri, statistiques
  serve [--addr 127.0.0.1:8787] [--host studio.exemple.fr] [--no-auth] [--no-scheduler]
                                         studio web + planificateur (mot de passe exigé)
  password                               change le mot de passe du studio (stdin ou XGROWTH_NEW_PASSWORD)
  log [-n 50] | version
`
