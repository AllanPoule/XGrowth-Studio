package main

// Correctifs de l'audit contradictoire final de la PR (19/11/2026). Chaque test
// échoue sans le correctif qu'il nomme. Reprend, en les adaptant, les tests de
// démonstration de l'audit (X1 à X8) et le scénario S11 (pas de double envoi).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const auditAge = 20 * 24 * time.Hour // hors fenêtre de RefreshMetrics

// auditAnalyzable : 8 posts mesurés dont 3 « utile » nettement au-dessus.
func auditAnalyzable(t *testing.T, e *env) {
	t.Helper()
	for i := 0; i < 3; i++ {
		e.seed(t, pseed{typ: "utile", rate: 6, text: strings.Repeat("b", 200), age: auditAge})
	}
	for i := 0; i < 5; i++ {
		e.seed(t, pseed{typ: "fil", rate: 2, text: strings.Repeat("a", 200), age: auditAge})
	}
}

// auditBreakWrites : data.json.tmp devient un dossier, l'écriture échoue.
func auditBreakWrites(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, "data.json.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func auditReopen(t *testing.T, e *env, dir string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, "data.json.tmp")); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.st = NewStudio(store)
	e.st.ai = func(Settings) AI { return e.ai }
	e.st.backendFor = func(*Account) (XBackend, error) { return e.x, nil }
	e.st.now = func() time.Time { return e.now }
}

// C1 : la catégorie vient du code HTTP du SDK, ou d'un motif ancré, jamais d'un
// nombre qui traîne dans un Request-ID.
func TestAuditC1ErrorCategoryIgnoresRequestID(t *testing.T) {
	cases := map[string]string{
		`API Claude : HTTP 529 : POST "https://api.anthropic.com/v1/messages?beta=true": 529  (Request-ID: req_011CU4013mQ9k) {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`:                                                             "fournisseur indisponible",
		`API Claude : HTTP 429 : POST "https://api.anthropic.com/v1/messages?beta=true": 429 Too Many Requests (Request-ID: req_01403kZt7p) {"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your organization's rate limit"}}`: "quota dépassé",
		"API Claude : HTTP 401 : unauthorized": "authentification",
		"Codex : status 403":                   "authentification",
		"Claude Code : HTTP 401":               "authentification",
	}
	for msg, want := range cases {
		if got := aiErrorCategory(&perfAIError{errors.New(msg)}); got != want {
			t.Errorf("catégorie %q au lieu de %q pour %q", got, want, msg)
		}
	}
}

// C1 : quand l'erreur du SDK est connue, son StatusCode fait foi, quel que soit
// le texte (Request-ID compris).
func TestAuditC1ErrorCategoryUsesTheSDKStatusCode(t *testing.T) {
	cases := map[int]string{401: "authentification", 403: "authentification", 429: "quota dépassé", 529: "fournisseur indisponible", 500: "fournisseur indisponible", 504: "délai dépassé"}
	for code, want := range cases {
		// Request-ID piégé : il contient « 401 » et « limit »
		sdk := &anthropic.Error{StatusCode: code, RequestID: "req_0401limit"}
		err := &perfAIError{fmt.Errorf("API Claude : HTTP %d : %w", sdk.StatusCode, sdk)}
		if got := aiErrorCategory(err); got != want {
			t.Errorf("HTTP %d : catégorie %q au lieu de %q", code, got, want)
		}
	}
}

// C2 : l'API ne renvoie jamais le message de l'erreur de stockage (chemin local).
func TestAuditC2APIAnalyzeReturnsFixedStorageCategory(t *testing.T) {
	srv, e := tuningServer(t)
	auditAnalyzable(t, e)
	dir := filepath.Dir(e.st.store.path)
	auditBreakWrites(t, dir)
	st, m := call(t, &http.Client{}, "POST", srv.URL+"/api/rules/analyze", `{"account":"main"}`)
	if st == 200 || m["ok"] == true {
		t.Fatalf("une panne d'écriture doit être une erreur : %v", m)
	}
	msg := m["error"].(map[string]any)["message"].(string)
	if strings.Contains(msg, dir) || strings.Contains(msg, "data.json") || !strings.Contains(msg, "stockage") {
		t.Fatalf("message d'erreur de l'API : %q (attendu : la catégorie « stockage » seule)", msg)
	}
}

// C3 : si l'écriture échoue, mémoire et disque restent égaux à l'état d'avant.
func TestAuditC3StoreUpdateKeepsMemoryOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	auditBreakWrites(t, dir)
	err = store.Update(func(d *Data) error {
		d.Settings.Rules = append(d.Settings.Rules, "REGLE-FANTOME")
		return nil
	})
	if err == nil {
		t.Fatal("l'écriture devait échouer")
	}
	if got := strings.Join(store.Snapshot().Settings.Rules, ","); got != "" {
		t.Fatalf("la mémoire garde un état jamais écrit : %q", got)
	}
	if err := os.Remove(filepath.Join(dir, "data.json.tmp")); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(d *Data) error { d.Settings.Rules = append(d.Settings.Rules, "R-OK"); return nil }); err != nil {
		t.Fatal(err)
	}
	again, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(again.Snapshot().Settings.Rules, ","); got != "R-OK" {
		t.Fatalf("après réparation, le disque dit %q (attendu R-OK seul)", got)
	}
}

// C3 (conséquence) : un disque en panne ne retient plus l'état de l'analyse en
// mémoire ; sans garde, chaque Tick rappellerait l'IA. Un appel par processus,
// comme avant le correctif.
func TestAuditC3DiskFailureDoesNotLoopOnTheAI(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{ai: &fakeAI{}, x: &fakeX{}, now: time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)}
	e.st = NewStudio(store)
	e.st.ai = func(Settings) AI { return e.ai }
	e.st.backendFor = func(*Account) (XBackend, error) { return e.x, nil }
	e.st.now = func() time.Time { return e.now }
	if err := e.st.SaveAccount(&Account{Name: "main", Handle: "@moi", Backend: "dryrun", Timezone: "UTC", Slots: []string{"09:00"}, DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	auditAnalyzable(t, e)
	e.tune(t, nil, func(s *Settings) { s.AutoPerfAnalysis = true })
	auditBreakWrites(t, dir)
	ctx := context.Background()
	for i := 0; i < 48; i++ {
		e.now = e.now.Add(time.Hour)
		e.st.Tick(ctx)
	}
	if e.ai.calls != 1 {
		t.Fatalf("appels IA avec un disque en panne pendant 48 Ticks : %d (attendu 1)", e.ai.calls)
	}
	auditReopen(t, e, dir)
	e.now = e.now.Add(time.Hour)
	e.st.Tick(ctx)
	if e.ai.calls != 2 {
		t.Fatalf("appels IA après réparation et redémarrage : %d (attendu 2)", e.ai.calls)
	}
}

// S11 : avec une panne d'écriture, aucune publication n'est lancée ; une fois le
// disque réparé et le studio redémarré, le post part une seule fois. Garde-fou
// du correctif C3, qui touche tout le studio.
func TestAuditS11NoDoublePublishOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{ai: &fakeAI{}, x: &fakeX{}, now: time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)}
	e.st = NewStudio(store)
	e.st.ai = func(Settings) AI { return e.ai }
	e.st.backendFor = func(*Account) (XBackend, error) { return e.x, nil }
	e.st.now = func() time.Time { return e.now }
	if err := e.st.SaveAccount(&Account{Name: "main", Handle: "@moi", Backend: "dryrun", Timezone: "UTC", Slots: []string{"09:00"}, DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	p := e.draft(t, "un post à publier")
	if _, err := e.st.Validate(p.ID, "Allan", nil, false); err != nil {
		t.Fatal(err)
	}
	auditBreakWrites(t, dir)
	ctx := context.Background()
	e.now = e.now.Add(2 * time.Hour)
	e.st.Tick(ctx)
	e.st.Tick(ctx)
	broken := len(e.x.posted)
	auditReopen(t, e, dir)
	e.st.Tick(ctx)
	e.st.Tick(ctx)
	if broken != 0 || len(e.x.posted) != 1 {
		t.Fatalf("publications : %d pendant la panne, %d au total après redémarrage (attendu 0 puis 1)", broken, len(e.x.posted))
	}
}

// C4 : une analyse déjà en cours dans un autre processus (verrou de fichier
// pris) est refusée sans appel IA ; le verrou est rendu à la fin.
func TestAuditC4FileLockRefusesASecondProcess(t *testing.T) {
	e := newEnv(t)
	auditAnalyzable(t, e)
	dir := filepath.Dir(e.st.store.path)
	unlock, err := lockAnalysis(dir) // l'« autre processus »
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AnalyzePerformance(context.Background(), "main"); !errors.Is(err, errPerfBusy) || err.Error() != "analyse déjà en cours" {
		t.Fatalf("erreur : %v (attendu « analyse déjà en cours »)", err)
	}
	e.tune(t, nil, func(s *Settings) { s.AutoPerfAnalysis = true })
	e.st.Tick(context.Background())
	if e.ai.calls != 0 {
		t.Fatalf("appels IA pendant que l'autre processus analyse : %d", e.ai.calls)
	}
	unlock()
	if _, err := e.st.AnalyzePerformance(context.Background(), "main"); err != nil || e.ai.calls != 1 {
		t.Fatalf("après libération du verrou : err=%v appels=%d", err, e.ai.calls)
	}
}

// C4 : deux processus (ici deux CLI, le même verrou vaut pour serve) lancent
// l'analyse ensemble : un seul appel au fournisseur. Faux « claude » sur le PATH
// qui compte ses lancements.
func TestAuditC4TwoProcessesCallTheProviderOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("compile le binaire")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "xgrowth")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("compilation : %v\n%s", err, out)
	}
	fakeDir := filepath.Join(tmp, "fakebin")
	counter := filepath.Join(tmp, "calls.txt")
	if err := os.MkdirAll(fakeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat >/dev/null\necho x >> " + counter + "\nsleep 2\n" +
		`printf '%s' '{"is_error":false,"subtype":"success","result":"","structured_output":{"proposals":[]}}'` + "\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(tmp, "home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{ai: &fakeAI{}, x: &fakeX{}, now: time.Now().UTC()}
	e.st = NewStudio(store)
	e.st.ai = func(Settings) AI { return e.ai }
	e.st.backendFor = func(*Account) (XBackend, error) { return e.x, nil }
	e.st.now = func() time.Time { return e.now }
	if err := e.st.SaveAccount(&Account{Name: "main", Handle: "@moi", Backend: "dryrun", Timezone: "UTC", Slots: []string{"09:00"}, DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	auditAnalyzable(t, e)
	e.tune(t, nil, func(s *Settings) { s.AIProvider = "claude-code" })
	env := append(os.Environ(), "XGROWTH_HOME="+home, "PATH="+fakeDir+":"+os.Getenv("PATH"))
	var wg sync.WaitGroup
	outs := make([][]byte, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(bin, "rules", "analyze", "--account", "main")
			cmd.Env = env
			outs[i], _ = cmd.CombinedOutput()
		}(i)
	}
	wg.Wait()
	b, _ := os.ReadFile(counter)
	if calls := strings.Count(string(b), "x"); calls != 1 {
		t.Fatalf("deux processus simultanés : %d lancements du fournisseur IA (attendu 1)\n%s\n%s", calls, outs[0], outs[1])
	}
}

// C5 : un compte qui a déjà 3 propositions de performance en attente est sauté,
// sans appel IA. 10 clics : un seul appel (30 règles proposées avant).
func TestAuditC5PendingProposalsSkipTheAccount(t *testing.T) {
	e := newEnv(t)
	auditAnalyzable(t, e)
	n := 0
	e.ai.perf = func(string) []map[string]any {
		n++
		var out []map[string]any
		for i := 0; i < 3; i++ {
			out = append(out, map[string]any{"text": fmt.Sprintf("Privilégie l'angle numéro %s, variante %s", strings.Repeat("i", n), strings.Repeat("v", i+1)),
				"rationale": "test", "signals": []string{"type:utile"}})
		}
		return out
	}
	ctx := context.Background()
	var last *PerfResult
	for i := 0; i < 10; i++ {
		r, err := e.st.AnalyzePerformance(ctx, "main")
		if err != nil {
			t.Fatal(err)
		}
		last = r
	}
	proposed := 0
	var first string
	for _, r := range e.st.store.Snapshot().RuleProposals {
		if r.Status == RuleProposed && r.Account == "main" {
			proposed++
			first = r.ID
		}
	}
	if e.ai.calls != 1 || proposed != perfMaxProposals {
		t.Fatalf("10 clics : %d appels IA, %d règles en attente (attendu 1 appel, %d règles)", e.ai.calls, proposed, perfMaxProposals)
	}
	if got := last.Accounts[0].Skipped; got != "décidez d'abord des propositions en attente" {
		t.Fatalf("motif de saut : %q", got)
	}
	// une décision libère la place : la proposition acceptée n'est plus « en attente »
	if _, err := e.st.DecideRule(first, "accept", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AnalyzePerformance(ctx, "main"); err != nil || e.ai.calls != 2 {
		t.Fatalf("après une décision : err=%v appels=%d (attendu 2)", err, e.ai.calls)
	}
}

// C7 : supprimer un compte supprime son état d'analyse (API) ; un compte recréé
// repart de zéro.
func TestAuditC7RemovedAccountLosesItsAnalysisState(t *testing.T) {
	srv, e := tuningServer(t)
	auditAnalyzable(t, e)
	if _, err := e.st.AnalyzePerformance(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	if e.st.store.Snapshot().PerfStates["main"] == nil {
		t.Fatal("l'état devait exister après l'analyse")
	}
	if st, m := call(t, &http.Client{}, "POST", srv.URL+"/api/accounts/main/remove", `{}`); st != 200 {
		t.Fatalf("suppression refusée : %v", m)
	}
	if st := e.st.store.Snapshot().PerfStates["main"]; st != nil {
		t.Fatalf("état conservé après suppression du compte : %+v", st)
	}
}

// C7 : le chemin de la CLI (account remove) passe par la même fonction, et la
// casse du nom ne change rien.
func TestAuditC7RemoveAccountIgnoresCaseAndKeepsOtherStates(t *testing.T) {
	e := newEnv(t)
	err := e.st.store.Update(func(d *Data) error {
		d.perfState("main").Failures = 2
		d.perfState("autre").Failures = 1
		return d.RemoveAccount("MAIN")
	})
	if err != nil {
		t.Fatal(err)
	}
	d := e.st.store.Snapshot()
	if d.PerfStates["main"] != nil || d.PerfStates["autre"] == nil || d.Account("main") != nil {
		t.Fatalf("états après suppression : %+v", d.PerfStates)
	}
	if err := e.st.store.Update(func(d *Data) error { return d.RemoveAccount("main") }); err == nil {
		t.Fatal("un compte inconnu doit être une erreur")
	}
}

// C12 : la synthèse des retours ne journalise que la catégorie fixe.
func TestAuditC12SynthesisLogsOnlyTheErrorCategory(t *testing.T) {
	e := newEnv(t)
	for _, text := range []string{"un", "deux", "trois"} {
		p := e.draft(t, text)
		if _, err := e.st.Reject(p.ID, "trop de jargon"); err != nil {
			t.Fatal(err)
		}
	}
	e.tune(t, nil, func(s *Settings) { s.AutoSynthesis = true })
	e.ai.err = func(string) error {
		return errors.New("Claude Code : exit status 1 Error: cannot read /home/user/.claude/settings.json (Request-ID: req_SECRET)")
	}
	e.st.Tick(context.Background())
	found := false
	for _, l := range e.st.store.Snapshot().Log {
		if strings.Contains(l.Message, "/home/user") || strings.Contains(l.Message, "req_SECRET") {
			t.Fatalf("le journal garde le message brut du fournisseur : %q", l.Message)
		}
		if l.Message == "synthèse des retours : fournisseur indisponible" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ligne de journal attendue « synthèse des retours : <catégorie> » : %+v", e.st.store.Snapshot().Log)
	}
}

// Relecture : tant que le disque refuse l'écriture, aucune étape du Tick qui
// appelle l'IA ne boucle. Le garde est commun (storageDown), un test par étape.
func auditTicksWithBrokenDisk(t *testing.T, e *env, n int) {
	t.Helper()
	auditBreakWrites(t, filepath.Dir(e.st.store.path))
	for i := 0; i < n; i++ {
		e.now = e.now.Add(time.Minute)
		e.st.Tick(context.Background())
	}
}

func TestAuditStorageDownSynthesisCallsAIOnce(t *testing.T) {
	e := newEnv(t)
	for _, text := range []string{"un", "deux", "trois"} {
		p := e.draft(t, text)
		if _, err := e.st.Reject(p.ID, "trop de jargon"); err != nil {
			t.Fatal(err)
		}
	}
	e.tune(t, nil, func(s *Settings) { s.AutoSynthesis = true })
	e.ai.calls = 0
	auditTicksWithBrokenDisk(t, e, 60)
	if e.ai.calls != 1 {
		t.Fatalf("appels IA de la synthèse, disque en panne, 60 Ticks : %d (attendu 1)", e.ai.calls)
	}
	// le disque revient : la sonde rouvre les étapes IA, la synthèse se refait
	dir := filepath.Dir(e.st.store.path)
	if err := os.Remove(filepath.Join(dir, "data.json.tmp")); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Minute)
	e.st.Tick(context.Background())
	if e.ai.calls != 2 {
		t.Fatalf("appels IA après le retour du disque : %d (attendu 2)", e.ai.calls)
	}
}

func TestAuditStorageDownTriageCallsAIOnce(t *testing.T) {
	e := newEnv(t)
	if _, err := e.st.AddWatch("main", "user", "@source", 30); err != nil {
		t.Fatal(err)
	}
	_ = e.st.store.Update(func(d *Data) error {
		d.Account("main").AutoTriage = true
		return nil
	})
	e.x.feed = []Tweet{tw("1", "a", "Les agents IA en production")}
	e.st.PollWatch(context.Background(), "", true)
	e.ai.calls = 0
	auditTicksWithBrokenDisk(t, e, 30)
	if e.ai.calls != 1 {
		t.Fatalf("appels IA du tri, disque en panne, 30 Ticks : %d (attendu 1)", e.ai.calls)
	}
}

// C1 : emballage de l'erreur du SDK dans claudeAI.JSON (%w) : le code HTTP reste
// lisible dans la chaîne.
func TestAuditC1WrapAPIErrorKeepsTheSDKError(t *testing.T) {
	err := wrapAPIError(&anthropic.Error{StatusCode: 429})
	var sdk *anthropic.Error
	if !errors.As(err, &sdk) || sdk.StatusCode != 429 {
		t.Fatalf("l'erreur du SDK n'est plus dans la chaîne : %v", err)
	}
	if got := aiErrorCategory(&perfAIError{err}); got != "quota dépassé" {
		t.Fatalf("catégorie %q (attendu « quota dépassé »)", got)
	}
}

// C1 : le texte ne contient aucun motif « HTTP nnn » : seul le StatusCode du SDK
// décide, même quand le texte pointe une autre catégorie.
func TestAuditC1StatusCodeDecidesWhenTextHasNoPattern(t *testing.T) {
	cases := []struct {
		code int
		text string
		want string
	}{
		{429, "unauthorized", "quota dépassé"},
		{401, "boom", "authentification"},
		{500, "quota", "fournisseur indisponible"},
	}
	for _, c := range cases {
		err := &perfAIError{fmt.Errorf("%s : %w", c.text, &anthropic.Error{StatusCode: c.code})}
		if got := aiErrorCategory(err); got != c.want {
			t.Errorf("code %d, texte %q : catégorie %q (attendu %q)", c.code, c.text, got, c.want)
		}
	}
}
