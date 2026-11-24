package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpgBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAttachMediaRefusesNonImageAndOversize(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "avec visuel")
	good := pngBytes(t)
	cases := map[string][]byte{
		"texte":                  []byte("ceci n'est pas une image"),
		"html":                   []byte("<html><script>alert(1)</script></html>"),
		"vide":                   {},
		"en-tête png sans image": append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...),
		"trop lourd":             append(append([]byte{}, good...), bytes.Repeat([]byte{0}, 5<<20)...),
	}
	for name, data := range cases {
		if _, err := e.st.AttachMedia(p.ID, bytes.NewReader(data)); err == nil {
			t.Errorf("%s : aurait dû être refusé", name)
		}
	}
	if entries, _ := os.ReadDir(e.st.mediaDir()); len(entries) != 0 {
		t.Errorf("un refus ne doit rien laisser sur le disque : %v", entries)
	}
	if e.st.store.Snapshot().Post(p.ID).MediaFile != "" {
		t.Error("aucun visuel ne doit être associé après un refus")
	}
}

func TestAttachMediaStoresUnguessableCopyAndUsesItAtPublication(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "post illustré")
	got, err := e.st.AttachMedia(p.ID, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	if !mediaFileRe.MatchString(got.MediaFile) || got.MediaPath != "" {
		t.Fatalf("nom stable attendu, obtenu %+v", got)
	}
	full := filepath.Join(e.st.mediaDir(), got.MediaFile)
	info, err := os.Stat(full)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("copie absente ou permissions trop larges : %v %v", err, info)
	}
	// un second envoi tire un autre nom, et remplace (l'ancienne copie disparaît)
	again, err := e.st.AttachMedia(p.ID, bytes.NewReader(jpgBytes(t)))
	if err != nil || again.MediaFile == got.MediaFile || !strings.HasSuffix(again.MediaFile, ".jpg") {
		t.Fatalf("remplacement : %+v %v", again, err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Error("l'ancienne copie doit être supprimée au remplacement")
	}
	// publication programmée : c'est la copie stable qui part
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatal(err)
	}
	if r := e.st.PublishDue(context.Background()); len(r) != 1 || r[0].Status != StatusPublished {
		t.Fatalf("publication : %+v", r)
	}
	want := filepath.Join(e.st.mediaDir(), again.MediaFile)
	if len(e.x.posted) != 1 || e.x.posted[0].MediaPath != want {
		t.Fatalf("la copie stable aurait dû être utilisée : %+v (attendu %s)", e.x.posted, want)
	}
}

func TestMediaNoPathTraversal(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "x")
	// un nom forgé en base ne devient jamais un chemin
	for _, bad := range []string{"../../etc/passwd", "../data.json", "/etc/passwd", "a.png", strings.Repeat("a", 32) + ".svg", strings.Repeat("a", 32) + "/../x.png"} {
		_ = e.st.store.Update(func(d *Data) error { d.Post(p.ID).MediaFile = bad; return nil })
		post := e.st.store.Snapshot().Post(p.ID)
		if got := e.st.mediaSource(post); got != "" {
			t.Errorf("%q : chemin %q renvoyé", bad, got)
		}
		if _, _, ok := e.st.MediaFileOf(p.ID); ok {
			t.Errorf("%q : l'aperçu ne doit rien servir", bad)
		}
	}
	// l'API n'accepte ni chemin ni nom du client : seul le contenu compte
	srv := httptest.NewServer(buildHandler(e.st, ServeOptions{NoAuth: true}))
	defer srv.Close()
	up := func(url string, body []byte, hdr bool) int {
		req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
		if hdr {
			req.Header.Set("X-XGrowth", "1")
		}
		req.Header.Set("Content-Type", "image/png")
		req.Header.Set("Content-Disposition", `attachment; filename="../../evil.png"`)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	_ = e.st.store.Update(func(d *Data) error { d.Post(p.ID).MediaFile = ""; return nil })
	if c := up(srv.URL+"/api/posts/"+p.ID+"/media-file", pngBytes(t), false); c != http.StatusForbidden {
		t.Errorf("sans X-XGrowth : %d", c)
	}
	if c := up(srv.URL+"/api/posts/"+p.ID+"/media-file", pngBytes(t), true); c != http.StatusOK {
		t.Fatalf("envoi valide : %d", c)
	}
	entries, _ := os.ReadDir(e.st.mediaDir())
	if len(entries) != 1 || !mediaFileRe.MatchString(entries[0].Name()) {
		t.Fatalf("nom de fichier du client utilisé ? %v", entries)
	}
	r, _ := http.Get(srv.URL + "/api/posts/" + p.ID + "/media-file")
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("aperçu : %d %v", r.StatusCode, r.Header)
	}
	r.Body.Close()
}

func TestLegacyServerPathStillWorks(t *testing.T) {
	e := newEnv(t)
	legacy := filepath.Join(t.TempDir(), "ancien.png")
	if err := os.WriteFile(legacy, pngBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := e.st.AddDraft("main", "ancien visuel", "", "", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatal(err)
	}
	e.st.PublishDue(context.Background())
	if len(e.x.posted) != 1 || e.x.posted[0].MediaPath != legacy {
		t.Fatalf("le chemin serveur existant doit rester utilisé : %+v", e.x.posted)
	}
}

func TestMediaRemoveAndOrphanCleanup(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "x")
	a, err := e.st.AttachMedia(p.ID, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.SetMedia(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.st.mediaDir(), a.MediaFile)); !os.IsNotExist(err) {
		t.Error("le retrait doit supprimer la copie")
	}
	// orphelins : un ancien est nettoyé, un récent, un référencé et un intrus non conforme restent
	kept, _ := e.st.AttachMedia(p.ID, bytes.NewReader(pngBytes(t)))
	dir := e.st.mediaDir()
	old := strings.Repeat("b", 32) + ".png"
	fresh := strings.Repeat("c", 32) + ".png"
	other := "notes.txt"
	for _, n := range []string{old, fresh, other} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600)
	}
	past := e.now.Add(-2 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, old), past, past)
	_ = os.Chtimes(filepath.Join(dir, kept.MediaFile), past, past)
	e.st.now = func() time.Time { return e.now }
	if n := e.st.CleanMedia(); n != 1 {
		t.Fatalf("1 orphelin attendu, %d supprimé(s)", n)
	}
	for n, want := range map[string]bool{old: false, fresh: true, other: true, kept.MediaFile: true} {
		_, err := os.Stat(filepath.Join(dir, n))
		if (err == nil) != want {
			t.Errorf("%s : présent=%v attendu=%v", n, err == nil, want)
		}
	}
}

func TestMediaLimitsPerBackend(t *testing.T) {
	li := mediaLimitsFor(&Account{Platform: PlatformLinkedIn, Backend: "linkedin"})
	x := mediaLimitsFor(&Account{Backend: "agentx"})
	xapi := mediaLimitsFor(&Account{Backend: "xapi"})
	if li.MaxBytes != 10<<20 || !li.AltText || x.MaxBytes != 5<<20 || x.MaxImages != 1 || xapi.MaxImages != 0 {
		t.Fatalf("limites inattendues : %+v %+v %+v", li, x, xapi)
	}
	e := newEnv(t)
	_ = e.st.SaveAccount(&Account{Name: "api", Handle: "@api", Backend: "xapi", Timezone: "UTC", Slots: []string{"09:00"}, DailyLimit: 1})
	p, err := e.st.AddDraft("api", "texte", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AttachMedia(p.ID, bytes.NewReader(pngBytes(t))); err == nil {
		t.Error("xapi n'envoie pas de visuel : refus attendu")
	}
}
