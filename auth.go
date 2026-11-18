package main

// Authentification du studio web.
//
// Au premier lancement, aucun mot de passe n'existe : le serveur génère un code
// à usage unique (affiché dans ses journaux et écrit dans
// $XGROWTH_HOME/setup-code.txt). L'écran d'accueil demande ce code puis le mot
// de passe à créer. Personne d'autre que celui qui a accès au serveur ne peut
// donc « réclamer » une instance fraîchement installée.

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	pbkdf2Iterations = 600_000
	sessionCookie    = "xg_session"
	sessionTTL       = 30 * 24 * time.Hour
)

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashPassword(pw string, salt []byte, iter int) (string, error) {
	k, err := pbkdf2.Key(sha256.New, pw, salt, iter, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(k), nil
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func validPassword(pw string) error {
	if len([]rune(pw)) < 10 {
		return errors.New("le mot de passe doit faire au moins 10 caractères")
	}
	return nil
}

// EnsureSetupCode crée (ou relit) le code d'installation tant qu'aucun mot de
// passe n'existe. Renvoie "" si le studio est déjà protégé.
func EnsureSetupCode(store *Store) (string, error) {
	var code string
	err := store.Update(func(d *Data) error {
		if d.Auth.PasswordHash != "" {
			d.Auth.SetupCode = ""
			return nil
		}
		if d.Auth.SetupCode == "" {
			b := make([]byte, 5)
			_, _ = rand.Read(b)
			c := strings.ToUpper(hex.EncodeToString(b))
			d.Auth.SetupCode = c[:5] + "-" + c[5:]
		}
		code = d.Auth.SetupCode
		return nil
	})
	if err == nil {
		f := filepath.Join(homeDir(), "setup-code.txt")
		if code != "" {
			_ = os.WriteFile(f, []byte(code+"\n"), 0o600)
		} else {
			_ = os.Remove(f)
		}
	}
	return code, err
}

// SetPassword remplace le mot de passe et révoque toutes les sessions.
func SetPassword(store *Store, pw string) error {
	if err := validPassword(pw); err != nil {
		return err
	}
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	h, err := hashPassword(pw, salt, pbkdf2Iterations)
	if err != nil {
		return err
	}
	err = store.Update(func(d *Data) error {
		d.Auth = Auth{PasswordHash: h, Salt: hex.EncodeToString(salt), Iterations: pbkdf2Iterations}
		return nil
	})
	_ = os.Remove(filepath.Join(homeDir(), "setup-code.txt"))
	return err
}

type authGate struct {
	store   *Store
	disable bool

	mu       sync.Mutex
	failures []time.Time
}

func (g *authGate) checkPassword(pw string) bool {
	var a Auth
	g.store.View(func(d *Data) { a = d.Auth })
	if a.PasswordHash == "" {
		return false
	}
	salt, _ := hex.DecodeString(a.Salt)
	h, err := hashPassword(pw, salt, a.Iterations)
	return err == nil && subtle.ConstantTimeCompare([]byte(h), []byte(a.PasswordHash)) == 1
}

// throttled limite les essais : 8 échecs en 15 minutes bloquent la connexion.
func (g *authGate) throttled() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	cut := time.Now().Add(-15 * time.Minute)
	kept := g.failures[:0]
	for _, t := range g.failures {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	g.failures = kept
	return len(g.failures) >= 8
}

func (g *authGate) fail() {
	g.mu.Lock()
	g.failures = append(g.failures, time.Now())
	g.mu.Unlock()
	time.Sleep(700 * time.Millisecond)
}

func (g *authGate) newSession(w http.ResponseWriter, r *http.Request) error {
	tok := randomToken(32)
	err := g.store.Update(func(d *Data) error {
		now := time.Now()
		kept := d.Auth.Sessions[:0]
		for _, s := range d.Auth.Sessions {
			if s.Expires.After(now) {
				kept = append(kept, s)
			}
		}
		d.Auth.Sessions = append(kept, Session{Hash: sha(tok), Expires: now.Add(sessionTTL)})
		return nil
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds())})
	return nil
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || (fromLoopback(r) && r.Header.Get("X-Forwarded-Proto") == "https")
}

func fromLoopback(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (g *authGate) authenticated(r *http.Request) bool {
	if g.disable {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	h, ok := sha(c.Value), false
	g.store.View(func(d *Data) {
		for _, s := range d.Auth.Sessions {
			if subtle.ConstantTimeCompare([]byte(s.Hash), []byte(h)) == 1 && s.Expires.After(time.Now()) {
				ok = true
			}
		}
	})
	return ok
}

func (g *authGate) needsSetup() bool {
	var need bool
	g.store.View(func(d *Data) { need = d.Auth.PasswordHash == "" })
	return need && !g.disable
}

// routes enregistre /api/auth/* (ouvertes) ; le reste de l'API passe par wrap.
func (g *authGate) routes(mux *http.ServeMux, decode func(http.ResponseWriter, *http.Request) (map[string]any, bool)) {
	mux.HandleFunc("GET /api/auth/status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"authenticated": g.authenticated(r), "needsSetup": g.needsSetup(), "disabled": g.disable}, nil)
	})
	mux.HandleFunc("POST /api/auth/setup", func(w http.ResponseWriter, r *http.Request) {
		b, ok := decode(w, r)
		if !ok {
			return
		}
		if !g.needsSetup() {
			reply(w, nil, errors.New("le mot de passe existe déjà"))
			return
		}
		if g.throttled() {
			reply(w, nil, errors.New("trop d'essais, réessayez dans 15 minutes"))
			return
		}
		var code string
		g.store.View(func(d *Data) { code = d.Auth.SetupCode })
		given := strings.ToUpper(strings.TrimSpace(fmt.Sprint(b["code"])))
		if code == "" || subtle.ConstantTimeCompare([]byte(given), []byte(code)) != 1 {
			g.fail()
			reply(w, nil, errors.New("code d'installation incorrect"))
			return
		}
		pw, _ := b["password"].(string)
		if err := SetPassword(g.store, pw); err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, map[string]bool{"ok": true}, g.newSession(w, r))
	})
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		b, ok := decode(w, r)
		if !ok {
			return
		}
		if g.throttled() {
			reply(w, nil, errors.New("trop d'essais, réessayez dans 15 minutes"))
			return
		}
		pw, _ := b["password"].(string)
		if !g.checkPassword(pw) {
			g.fail()
			reply(w, nil, errors.New("mot de passe incorrect"))
			return
		}
		reply(w, map[string]bool{"ok": true}, g.newSession(w, r))
	})
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			h := sha(c.Value)
			_ = g.store.Update(func(d *Data) error {
				kept := d.Auth.Sessions[:0]
				for _, s := range d.Auth.Sessions {
					if s.Hash != h {
						kept = append(kept, s)
					}
				}
				d.Auth.Sessions = kept
				return nil
			})
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
		reply(w, map[string]bool{"ok": true}, nil)
	})
}

// wrap exige une session valide pour toute route /api/ hors /api/auth/.
func (g *authGate) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/auth/") && !g.authenticated(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"ok":false,"schema_version":"1","error":{"code":"unauthorized","message":"connexion requise"}}` + "\n"))
			return
		}
		h.ServeHTTP(w, r)
	})
}
