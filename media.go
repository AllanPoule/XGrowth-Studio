package main

// Visuels déposés depuis le navigateur. Le serveur garde une copie stable de
// l'image dans <données>/media, sous un nom aléatoire que personne ne peut
// deviner (32 caractères hexadécimaux + extension tirée du CONTENU, jamais du
// nom envoyé) : la publication programmée l'utilise, même si le fichier d'origine
// a disparu du téléphone. Le client n'envoie jamais de chemin ni de nom de
// fichier. Les posts qui portent un ancien chemin serveur (MediaPath)
// continuent de fonctionner tels quels.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MediaLimits : ce que permet le backend d'un compte (affiché dans l'interface).
type MediaLimits struct {
	MaxImages int      `json:"maxImages"` // 0 = le backend n'envoie pas de visuel
	Formats   []string `json:"formats"`
	MaxBytes  int64    `json:"maxBytes"`
	MaxPixels int      `json:"maxPixels,omitempty"`
	AltText   bool     `json:"altText"` // le texte alternatif est transmis à la publication
	Note      string   `json:"note,omitempty"`
}

const mediaUploadCeiling = linkedinMaxImageBytes // plafond de lecture, tous backends confondus

var mediaFileRe = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|png|gif)$`)

// mediaLimitsFor : les limites réelles du backend du compte.
func mediaLimitsFor(a *Account) MediaLimits {
	formats := []string{"JPG", "PNG", "GIF"}
	if isLinkedIn(a) {
		return MediaLimits{MaxImages: 1, Formats: formats, MaxBytes: linkedinMaxImageBytes, MaxPixels: linkedinMaxPixels, AltText: true,
			Note: "Une seule image par post."}
	}
	if a != nil && a.Backend == "xapi" {
		return MediaLimits{Formats: formats, Note: "Le backend xapi n'envoie pas de visuel : choisissez agentx pour publier avec une image."}
	}
	return MediaLimits{MaxImages: 1, Formats: formats, MaxBytes: 5 << 20,
		Note: "Une seule image par post ; le texte alternatif n'est pas transmis à X avec ce backend."}
}

func (l MediaLimits) formatsOK(f string) bool {
	for _, x := range l.Formats {
		if strings.EqualFold(x, f) || (f == "jpeg" && x == "JPG") {
			return true
		}
	}
	return false
}

func (s *Studio) mediaDir() string { return filepath.Join(s.store.Dir(), "media") }

// mediaSource est le fichier à envoyer à la publication : la copie stable si
// le post en a une, sinon l'ancien chemin serveur. Le nom stocké est revérifié
// (jamais de séparateur ni de « .. »).
func (s *Studio) mediaSource(p *Post) string {
	if p.MediaFile != "" {
		if !mediaFileRe.MatchString(p.MediaFile) {
			return "" // nom forgé en base : pas de fichier plutôt qu'un chemin quelconque
		}
		return filepath.Join(s.mediaDir(), p.MediaFile)
	}
	return p.MediaPath
}

// inspectImage vérifie le CONTENU : format reconnu par son en-tête (pas par
// l'extension ni le type annoncé), type MIME détecté cohérent, taille et pixels.
func inspectImage(data []byte, l MediaLimits) (ext string, err error) {
	if l.MaxImages == 0 {
		return "", errors.New("ce compte ne publie pas de visuel : " + l.Note)
	}
	if int64(len(data)) > l.MaxBytes {
		return "", fmt.Errorf("visuel trop lourd (%d Mo maximum)", l.MaxBytes>>20)
	}
	cfg, format, derr := image.DecodeConfig(bytes.NewReader(data))
	if derr != nil || !l.formatsOK(format) {
		return "", errors.New("visuel refusé : " + strings.Join(l.Formats, ", ") + " attendu")
	}
	mime := http.DetectContentType(data)
	want := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif"}[format]
	if mime != want {
		return "", errors.New("visuel refusé : le contenu ne correspond pas à un fichier image valide")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return "", errors.New("visuel refusé : image vide")
	}
	if l.MaxPixels > 0 && cfg.Width*cfg.Height >= l.MaxPixels {
		return "", errors.New("visuel refusé : image trop grande pour ce backend")
	}
	if format == "jpeg" {
		return "jpg", nil
	}
	return format, nil
}

// AttachMedia enregistre l'image envoyée par le navigateur comme visuel du
// post. Elle remplace l'éventuelle image précédente ; le texte alternatif est gardé.
func (s *Studio) AttachMedia(id string, body io.Reader) (*Post, error) {
	d := s.store.Snapshot()
	p := d.Post(id)
	if p == nil {
		return nil, fmt.Errorf("post %q inconnu", id)
	}
	if err := editable(p); err != nil {
		return nil, err
	}
	a := d.Account(p.Account)
	if handOnly(a, p) {
		return nil, errors.New("une réponse ou un commentaire LinkedIn n'a pas de visuel")
	}
	limits := mediaLimitsFor(a)
	if limits.MaxImages == 0 {
		return nil, errors.New("ce compte ne publie pas de visuel : " + limits.Note)
	}
	data, err := io.ReadAll(io.LimitReader(body, limits.MaxBytes+1))
	if err != nil {
		return nil, errors.New("envoi du visuel interrompu")
	}
	if len(data) == 0 {
		return nil, errors.New("aucun fichier reçu")
	}
	ext, err := inspectImage(data, limits)
	if err != nil {
		return nil, err
	}
	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, err
	}
	name := hex.EncodeToString(rnd[:]) + "." + ext
	dir := s.mediaDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errors.New("dossier des visuels inaccessible")
	}
	tmp, err := os.CreateTemp(dir, ".up-*")
	if err != nil {
		return nil, errors.New("copie du visuel impossible")
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), 0o600) != nil || os.Rename(tmp.Name(), filepath.Join(dir, name)) != nil {
		_ = os.Remove(tmp.Name())
		return nil, errors.New("copie du visuel impossible")
	}
	var out *Post
	var old string
	err = s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if err := editable(p); err != nil {
			return err
		}
		old = p.MediaFile
		p.MediaFile, p.MediaPath = name, ""
		resetValidation(p)
		out = p
		return nil
	})
	if err != nil {
		_ = os.Remove(filepath.Join(dir, name))
		return nil, err
	}
	s.dropMediaFile(old)
	return out, nil
}

// dropMediaFile supprime une copie qui n'est plus référencée par aucun post.
func (s *Studio) dropMediaFile(name string) {
	if name == "" || !mediaFileRe.MatchString(name) {
		return
	}
	for _, p := range s.store.Snapshot().Posts {
		if p.MediaFile == name {
			return
		}
	}
	_ = os.Remove(filepath.Join(s.mediaDir(), name))
}

// MediaFileOf renvoie le chemin de la copie stable d'un post (pour l'aperçu).
func (s *Studio) MediaFileOf(id string) (path, ext string, ok bool) {
	p := s.store.Snapshot().Post(id)
	if p == nil || !mediaFileRe.MatchString(p.MediaFile) {
		return "", "", false
	}
	return filepath.Join(s.mediaDir(), p.MediaFile), filepath.Ext(p.MediaFile), true
}

const mediaOrphanGrace = time.Hour // une copie toute neuve n'est pas encore rattachée à son post

// CleanMedia supprime les copies qu'aucun post ne référence plus (post effacé,
// image remplacée ou retirée, envoi interrompu). Renvoie le nombre supprimé.
func (s *Studio) CleanMedia() int {
	entries, err := os.ReadDir(s.mediaDir())
	if err != nil {
		return 0
	}
	used := map[string]bool{}
	for _, p := range s.store.Snapshot().Posts {
		if p.MediaFile != "" {
			used[p.MediaFile] = true
		}
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || used[name] || !(mediaFileRe.MatchString(name) || strings.HasPrefix(name, ".up-")) {
			continue
		}
		if info, err := e.Info(); err != nil || s.now().Sub(info.ModTime()) < mediaOrphanGrace {
			continue
		}
		if os.Remove(filepath.Join(s.mediaDir(), name)) == nil {
			n++
		}
	}
	return n
}
