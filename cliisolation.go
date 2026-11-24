package main

// Isolation des CLI d'IA (Claude Code, Codex, Gemini CLI).
//
// Les posts lus sur X sont des données non fiables : ils peuvent contenir des instructions destinées à l'IA
// (injection). La défense repose sur trois couches, construites ici et testées sans lancer les vraies CLI :
//  1. aucun outil : arguments et configuration dédiés à chaque fournisseur ;
//  2. un environnement réduit à une liste blanche (pas de jetons inutiles, pas de clés des autres services) ;
//  3. un répertoire de travail vide et temporaire, et un dossier de configuration dédié (Codex, Gemini).
//
// Limites : ces garanties dépendent du respect des options par chaque CLI (versions testées : voir le README).
// Les identifiants de connexion de la CLI (abonnement) doivent rester accessibles : ils sont copiés dans le
// dossier temporaire, ou lus dans le dossier habituel pour Claude Code.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// baseEnvAllow : variables transmises à toutes les CLI. Aucune ne porte de secret.
var baseEnvAllow = []string{
	"PATH", "HOME", "USER", "LOGNAME", "LANG", "LANGUAGE", "TZ", "TERM", "TMPDIR",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
}

// providerEnvAllow : variables d'authentification propres à chaque CLI (et seulement celles-là).
var providerEnvAllow = map[string][]string{
	// ANTHROPIC_API_KEY et ANTHROPIC_AUTH_TOKEN sont volontairement absents : Claude Code utilise l'abonnement.
	"claude-code": {"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CONFIG_DIR", "XDG_CONFIG_HOME"},
	"codex":       {},
	"gemini":      {"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION"},
}

// buildCLIEnv construit l'environnement d'une CLI : liste blanche de base + liste du fournisseur + `set`
// (valeurs imposées, qui l'emportent). Le résultat est trié, donc stable.
func buildCLIEnv(environ []string, provider string, set map[string]string) []string {
	allow := map[string]bool{}
	for _, k := range baseEnvAllow {
		allow[k] = true
	}
	for _, k := range providerEnvAllow[provider] {
		allow[k] = true
	}
	vals := map[string]string{}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if ok && allow[k] && v != "" {
			vals[k] = v
		}
	}
	for k, v := range set {
		vals[k] = v
	}
	out := make([]string, 0, len(vals))
	for k, v := range vals {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------- Claude Code

func claudeArgs(system, model string, schema map[string]any) []string {
	sch, _ := json.Marshal(schema)
	args := []string{"-p", "--tools", "", "--strict-mcp-config", "--setting-sources", "", "--no-session-persistence",
		"--output-format", "json", "--json-schema", string(sch), "--system-prompt", system}
	if model != "" {
		args = append(args, "--model", model)
	}
	return args
}

// ---------------------------------------------------------------- Codex

// codexConfig : réglages imposés (valeurs TOML). Ils sont écrits dans le config.toml du CODEX_HOME dédié ET
// passés par `-c` (priorité la plus haute) : un config.toml de l'utilisateur ne peut rien rouvrir.
// Clés présentes dans `codex features list` (codex-cli 0.171.0) ; une clé inconnue est ignorée par la CLI.
// Limite constatée avec `codex debug prompt-input` : ces réglages retirent le shell (exec_command, write_stdin) et
// view_image, mais pas exec (isolat JS), request_user_input, sleep ni les outils de collaboration ; features.unified_exec
// reste affiché actif. Ils restent sans effet réel : sandbox en lecture seule, aucun MCP, dossier vide, pas de réseau.
var codexConfig = [][2]string{
	{"approval_policy", `"never"`},
	{"sandbox_mode", `"read-only"`},
	{"web_search", `"disabled"`},
	{"mcp_servers", `{}`},
	{"cli_auth_credentials_store", `"file"`},
	{"features.shell_tool", "false"},
	{"features.unified_exec", "false"},
	{"features.apps", "false"},
	{"features.plugins", "false"},
	{"features.hooks", "false"},
	{"features.multi_agent", "false"},
	{"features.view_image", "false"},
	{"features.image_generation", "false"},
	{"features.computer_use", "false"},
	{"features.browser_use", "false"},
	{"features.browser_use_external", "false"},
	{"features.in_app_browser", "false"},
	{"features.skill_mcp_dependency_install", "false"},
	{"features.memories", "false"},
}

// codexConfigTOML rend le config.toml du CODEX_HOME dédié.
func codexConfigTOML() string {
	var top, feat, other strings.Builder
	for _, kv := range codexConfig {
		switch {
		case strings.HasPrefix(kv[0], "features."):
			feat.WriteString(strings.TrimPrefix(kv[0], "features.") + " = " + kv[1] + "\n")
		case kv[0] == "mcp_servers":
			other.WriteString("# aucun serveur MCP : table vide\n")
		default:
			top.WriteString(kv[0] + " = " + kv[1] + "\n")
		}
	}
	return "# Généré par XGrowth pour un seul appel : aucun outil, aucun MCP.\n" + top.String() + "\n[features]\n" + feat.String() + other.String()
}

// codexArgs : arguments de `codex exec`. cwd est le dossier de travail vide, schemaFile et lastFile sont des
// fichiers du dossier temporaire.
func codexArgs(model, cwd, schemaFile, lastFile string) []string {
	args := []string{"exec", "--skip-git-repo-check", "--ephemeral", "--ignore-rules", "--sandbox", "read-only",
		"--cd", cwd, "--color", "never", "--output-schema", schemaFile, "-o", lastFile}
	for _, kv := range codexConfig {
		args = append(args, "-c", kv[0]+"="+kv[1])
	}
	if model != "" {
		args = append(args, "-m", model)
	}
	return append(args, "-")
}

// ---------------------------------------------------------------- Gemini CLI

// geminiPolicyTOML : politique du moteur de politiques de Gemini CLI. Une règle « * » en `deny`, priorité
// maximale : tout outil (natif ou MCP) est refusé, et un refus global retire l'outil de la liste vue par le
// modèle. Le fichier est passé aussi par `--admin-policy` (niveau administrateur, qu'aucun fichier utilisateur
// ne peut surcharger).
const geminiPolicyTOML = `# Généré par XGrowth : aucun outil, aucun MCP.
[[rule]]
toolName = "*"
decision = "deny"
priority = 999

[[rule]]
toolName = "mcp_*"
decision = "deny"
priority = 999
`

// geminiSettings : settings.json du dossier dédié (MCP vide ; les outils sont interdits par la politique). authType reprend le mode de connexion de l'utilisateur.
func geminiSettings(authType string) []byte {
	s := map[string]any{
		"mcp":        map[string]any{"allowed": []string{}},
		"mcpServers": map[string]any{},
		"security":   map[string]any{"folderTrust": map[string]any{"enabled": false}},
	}
	if authType != "" {
		s["security"] = map[string]any{"folderTrust": map[string]any{"enabled": false}, "auth": map[string]any{"selectedType": authType}}
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return b
}

func geminiArgs(model, adminPolicy string) []string {
	args := []string{"--output-format", "json", "--approval-mode", "default", "--admin-policy", adminPolicy}
	if model != "" {
		args = append(args, "-m", model)
	}
	return append(args, "-p", "")
}

// ---------------------------------------------------------------- identifiants de connexion

// authCopy : un fichier d'identifiants de la CLI, copié dans le dossier dédié le temps de l'appel. Si la CLI le
// renouvelle (jeton rafraîchi), la nouvelle version est recopiée à sa place : sinon la connexion serait perdue.
// La version installée est mémorisée : on ne recopie que si la CLI a changé la copie ET si personne d'autre n'a
// modifié la source entre-temps (sinon on écraserait un jeton plus récent, p. ex. renouvelé par la CLI interactive).
type authCopy struct {
	src, dst  string
	installed []byte
}

func (a *authCopy) install() error {
	b, err := os.ReadFile(a.src)
	if err != nil {
		return nil // pas d'identifiants à cet endroit : la CLI le dira elle-même
	}
	if err := os.MkdirAll(filepath.Dir(a.dst), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(a.dst, b, 0o600); err != nil {
		return err
	}
	a.installed = b
	return nil
}

func (a *authCopy) syncBack() {
	if a.installed == nil {
		return
	}
	cur, err1 := os.ReadFile(a.src)
	now, err2 := os.ReadFile(a.dst)
	if err1 != nil || err2 != nil || len(bytes.TrimSpace(now)) == 0 {
		return
	}
	if bytes.Equal(now, a.installed) || !bytes.Equal(cur, a.installed) {
		return // la CLI n'a rien renouvelé, ou la source a changé entre-temps : on ne touche à rien
	}
	tmp := a.src + ".xgrowth-tmp"
	if os.WriteFile(tmp, now, 0o600) == nil {
		if os.Rename(tmp, a.src) != nil {
			os.Remove(tmp)
		}
	}
}

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

func codexAuthSource() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return filepath.Join(h, "auth.json")
	}
	return filepath.Join(userHome(), ".codex", "auth.json")
}

func geminiHomeSource() string {
	if h := os.Getenv("GEMINI_CLI_HOME"); h != "" {
		return filepath.Join(h, ".gemini")
	}
	return filepath.Join(userHome(), ".gemini")
}

// geminiAuthType lit le mode de connexion choisi par l'utilisateur (security.auth.selectedType), rien d'autre.
func geminiAuthType(settingsPath string) string {
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return ""
	}
	var s struct {
		Security struct {
			Auth struct {
				SelectedType string `json:"selectedType"`
			} `json:"auth"`
		} `json:"security"`
	}
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	return s.Security.Auth.SelectedType
}
