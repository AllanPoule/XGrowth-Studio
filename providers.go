package main

// Fournisseurs d'IA. XGrowth peut écrire avec :
//   - l'API Anthropic (clé ANTHROPIC_API_KEY ou profil `ant auth login`) ;
//   - Claude Code (`claude -p`), avec votre abonnement Claude ;
//   - Codex CLI (`codex exec`), avec votre compte ChatGPT ;
//   - Gemini CLI (`gemini -p`), expérimental.
//
// Les CLI sont lancées sans aucun outil (ni shell, ni fichiers, ni web), dans
// un dossier temporaire vide : les posts lus sur X ne peuvent rien leur faire
// exécuter. Leur réponse est validée contre le même JSON Schema que l'API.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Vendor      string `json:"vendor"`
	Kind        string `json:"kind"` // api | cli
	Available   bool   `json:"available"`
	Version     string `json:"version,omitempty"`
	Path        string `json:"path,omitempty"`
	Hint        string `json:"hint"`
	Experiment  bool   `json:"experimental,omitempty"`
	DefaultNote string `json:"defaultModel"`
}

var providerCatalog = []ProviderInfo{
	{ID: "claude-code", Name: "Claude Code", Vendor: "Anthropic", Kind: "cli", DefaultNote: "modèle par défaut de Claude Code",
		Hint: "Utilise votre abonnement Claude. Installez Claude Code puis lancez `claude` une fois pour vous connecter."},
	{ID: "codex", Name: "Codex (ChatGPT)", Vendor: "OpenAI", Kind: "cli", DefaultNote: "modèle par défaut de Codex",
		Hint: "Utilise votre compte ChatGPT. Installez Codex CLI puis lancez `codex login`."},
	{ID: "gemini", Name: "Gemini CLI", Vendor: "Google", Kind: "cli", Experiment: true, DefaultNote: "modèle par défaut de Gemini CLI",
		Hint: "Utilise votre compte Google. Installez Gemini CLI puis lancez `gemini` une fois pour vous connecter."},
	{ID: "anthropic", Name: "API Claude", Vendor: "Anthropic", Kind: "api", DefaultNote: "claude-opus-5-5",
		Hint: "Facturé à l'usage. Définissez ANTHROPIC_API_KEY dans l'environnement du serveur."},
}

var cliBinary = map[string]string{"claude-code": "claude", "codex": "codex", "gemini": "gemini"}

// lookPath cherche un binaire dans le PATH puis dans les emplacements
// d'installation habituels (un service systemd a souvent un PATH réduit).
func lookPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".bun", "bin"), "/usr/local/bin", "/opt/homebrew/bin", "/usr/bin"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

var (
	detectMu    sync.Mutex
	detectCache []ProviderInfo
	detectAt    time.Time
)

// DetectProviders indique quelles IA sont utilisables sur cette machine.
func DetectProviders(force bool) []ProviderInfo {
	detectMu.Lock()
	defer detectMu.Unlock()
	if !force && detectCache != nil && time.Since(detectAt) < time.Minute {
		return detectCache
	}
	out := make([]ProviderInfo, len(providerCatalog))
	var wg sync.WaitGroup
	for i, p := range providerCatalog {
		out[i] = p
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := &out[i]
			if p.ID == "anthropic" {
				p.Available = os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("ANTHROPIC_AUTH_TOKEN") != "" || anthropicProfileExists()
				return
			}
			path := lookPath(cliBinary[p.ID])
			if path == "" {
				return
			}
			p.Path, p.Available = path, true
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if b, err := exec.CommandContext(ctx, path, "--version").Output(); err == nil {
				p.Version = strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
			}
		}(i)
	}
	wg.Wait()
	detectCache, detectAt = out, time.Now()
	return out
}

func anthropicProfileExists() bool {
	home, _ := os.UserHomeDir()
	_, err := os.Stat(filepath.Join(home, ".config", "anthropic"))
	return err == nil
}

// DefaultProvider choisit la première IA disponible, abonnements d'abord.
func DefaultProvider() string {
	for _, p := range DetectProviders(false) {
		if p.Available && !p.Experiment {
			return p.ID
		}
	}
	return "anthropic"
}

// NewAI construit le client correspondant au réglage.
func NewAI(s Settings) AI {
	switch s.AIProvider {
	case "claude-code":
		return &claudeCodeAI{model: s.Model}
	case "codex":
		return &codexAI{model: s.Model}
	case "gemini":
		return &geminiAI{model: s.Model}
	default:
		return NewClaudeAI(s.Model, s.Effort)
	}
}

// ---------------------------------------------------------------- exécution commune

const cliTimeout = 5 * time.Minute

// runCLI lance une CLI d'IA. dropEnv retire des variables de l'environnement
// (ex. ANTHROPIC_API_KEY pour que Claude Code utilise l'abonnement).
func runCLI(ctx context.Context, bin string, args []string, stdin string, dropEnv []string) ([]byte, []byte, error) {
	path := lookPath(bin)
	if path == "" {
		return nil, nil, fmt.Errorf("%s n'est pas installé sur cette machine", bin)
	}
	dir, err := os.MkdirTemp("", "xgrowth-ai-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		keep := true
		for _, k := range dropEnv {
			if strings.HasPrefix(kv, k+"=") {
				keep = false
			}
		}
		if keep {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, nil, fmt.Errorf("%s n'a pas répondu en %s", bin, cliTimeout)
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func lastLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

// extractJSON récupère le premier objet JSON d'un texte (blocs ``` acceptés).
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func decodeInto(raw []byte, out any, who string) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("réponse de %s illisible : %w", who, err)
	}
	return nil
}

// ---------------------------------------------------------------- Claude Code

type claudeCodeAI struct{ model string }

func (c *claudeCodeAI) JSON(ctx context.Context, system, user string, schema map[string]any, out any) error {
	sch, _ := json.Marshal(schema)
	args := []string{"-p", "--tools", "", "--strict-mcp-config", "--setting-sources", "", "--no-session-persistence",
		"--output-format", "json", "--json-schema", string(sch), "--system-prompt", system}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}
	stdout, stderr, err := runCLI(ctx, "claude", args, user, []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"})
	var res struct {
		IsError          bool            `json:"is_error"`
		Subtype          string          `json:"subtype"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if jerr := json.Unmarshal(bytes.TrimSpace(stdout), &res); jerr != nil {
		if err != nil {
			return fmt.Errorf("Claude Code : %v %s", err, lastLines(stderr, 3))
		}
		return fmt.Errorf("Claude Code : sortie inattendue %s", lastLines(stdout, 2))
	}
	if res.IsError {
		return fmt.Errorf("Claude Code : %s", firstNonEmpty(res.Result, res.Subtype, lastLines(stderr, 3)))
	}
	if len(res.StructuredOutput) > 0 && string(res.StructuredOutput) != "null" {
		return decodeInto(res.StructuredOutput, out, "Claude Code")
	}
	return decodeInto([]byte(extractJSON(res.Result)), out, "Claude Code")
}

// ---------------------------------------------------------------- Codex (ChatGPT)

type codexAI struct{ model string }

func (c *codexAI) JSON(ctx context.Context, system, user string, schema map[string]any, out any) error {
	tmp, err := os.MkdirTemp("", "xgrowth-codex-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	schemaFile, lastFile := filepath.Join(tmp, "schema.json"), filepath.Join(tmp, "last.json")
	sch, _ := json.Marshal(schema)
	if err := os.WriteFile(schemaFile, sch, 0o600); err != nil {
		return err
	}
	args := []string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "--color", "never",
		"--output-schema", schemaFile, "-o", lastFile}
	if c.model != "" {
		args = append(args, "-m", c.model)
	}
	args = append(args, "-")
	prompt := system + "\n\nN'utilise aucun outil et ne lis aucun fichier : réponds directement.\n\n---\n\n" + user
	_, stderr, runErr := runCLI(ctx, "codex", args, prompt, nil)
	raw, rerr := os.ReadFile(lastFile)
	if rerr != nil || len(bytes.TrimSpace(raw)) == 0 {
		if runErr != nil {
			return fmt.Errorf("Codex : %s", codexError(stderr, runErr))
		}
		return errors.New("Codex n'a renvoyé aucune réponse")
	}
	return decodeInto([]byte(extractJSON(string(raw))), out, "Codex")
}

func codexError(stderr []byte, err error) string {
	for _, l := range strings.Split(string(stderr), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "ERROR:") {
			l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "ERROR:"))
			var e struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(l), &e) == nil && e.Error.Message != "" {
				return e.Error.Message
			}
			return l
		}
	}
	return fmt.Sprintf("%v %s", err, lastLines(stderr, 2))
}

// ---------------------------------------------------------------- Gemini CLI (expérimental)

type geminiAI struct{ model string }

func (g *geminiAI) JSON(ctx context.Context, system, user string, schema map[string]any, out any) error {
	sch, _ := json.MarshalIndent(schema, "", " ")
	prompt := system + "\n\nRéponds UNIQUEMENT avec un objet JSON conforme à ce schéma, sans texte autour :\n" + string(sch) + "\n\n---\n\n" + user
	args := []string{"--output-format", "json", "--approval-mode", "default"}
	if g.model != "" {
		args = append(args, "-m", g.model)
	}
	args = append(args, "-p", "")
	stdout, stderr, err := runCLI(ctx, "gemini", args, prompt, nil)
	var res struct {
		Response string `json:"response"`
		Error    *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal([]byte(extractJSON(string(stdout))), &res); jerr != nil || (res.Response == "" && res.Error == nil) {
		if err != nil {
			return fmt.Errorf("Gemini CLI : %v %s", err, lastLines(stderr, 3))
		}
		return decodeInto([]byte(extractJSON(string(stdout))), out, "Gemini CLI")
	}
	if res.Error != nil {
		return fmt.Errorf("Gemini CLI : %s", res.Error.Message)
	}
	return decodeInto([]byte(extractJSON(res.Response)), out, "Gemini CLI")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------- test interactif

type AITestResult struct {
	Provider string `json:"provider"`
	OK       bool   `json:"ok"`
	Sample   string `json:"sample,omitempty"`
	Error    string `json:"error,omitempty"`
	Millis   int64  `json:"ms"`
}

// TestAI envoie une petite demande pour vérifier qu'une IA répond.
func TestAI(ctx context.Context, s Settings) AITestResult {
	start := time.Now()
	var out DraftOut
	err := NewAI(s).JSON(ctx,
		"Tu es la plume d'un compte X francophone. Réponds dans le format demandé.",
		"Écris une phrase d'accroche de 12 mots maximum pour annoncer qu'un studio de posts X vient d'être connecté à son IA. rationale : un mot.",
		draftSchema, &out)
	r := AITestResult{Provider: s.AIProvider, Millis: time.Since(start).Milliseconds()}
	if err != nil {
		r.Error = err.Error()
	} else {
		r.OK, r.Sample = true, out.Text
	}
	return r
}
