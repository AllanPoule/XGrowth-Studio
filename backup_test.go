package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupRotation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XGROWTH_BACKUP_KEEP", "3")
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	// deux appels le même jour : une seule sauvegarde
	for i := 0; i < 2; i++ {
		if err := backupIfDue(dir, base); err != nil {
			t.Fatal(err)
		}
	}
	// OpenStore a déjà sauvegardé aujourd'hui (date réelle) : on part de zéro
	os.RemoveAll(backupDir(dir))
	for d := 0; d < 5; d++ {
		if err := backupIfDue(dir, base.AddDate(0, 0, d)); err != nil {
			t.Fatal(err)
		}
		if err := backupIfDue(dir, base.AddDate(0, 0, d).Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	got := listBackups(dir)
	if len(got) != 3 {
		t.Fatalf("attendu 3 sauvegardes, obtenu %d : %v", len(got), got)
	}
	if !strings.Contains(got[0], "20261103") || !strings.Contains(got[2], "20261105") {
		t.Fatalf("mauvaise rotation : %v", got)
	}
	st, _ := os.Stat(got[0])
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("permissions %v", st.Mode().Perm())
	}
	ds, _ := os.Stat(backupDir(dir))
	if ds.Mode().Perm() != 0o700 {
		t.Fatalf("permissions dossier %v", ds.Mode().Perm())
	}
}

func TestBackupDisabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XGROWTH_BACKUP_KEEP", "0")
	if _, err := OpenStore(dir); err != nil {
		t.Fatal(err)
	}
	if len(listBackups(dir)) != 0 {
		t.Fatal("aucune sauvegarde attendue")
	}
}

func TestSaveCreatesDailyBackupAnd0600(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenStore(dir); err != nil {
		t.Fatal(err)
	}
	if len(listBackups(dir)) != 1 {
		t.Fatalf("une sauvegarde attendue : %v", listBackups(dir))
	}
	st, err := os.Stat(filepath.Join(dir, "data.json"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("data.json : %v %v", err, st)
	}
}

func TestRestoreRefusesCorrupted(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenStore(dir); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "data.json"))
	cases := map[string]string{
		"corrompu":       `{"version": 2, "accounts": [`,
		"pas-un-objet":   `[1,2]`,
		"sans-version":   `{"accounts": []}`,
		"version-future": `{"version": 99}`,
		"mauvais-type":   `{"version": 2, "accounts": "x"}`,
	}
	for name, content := range cases {
		src := filepath.Join(t.TempDir(), name+".json")
		os.WriteFile(src, []byte(content), 0o600)
		if _, err := restoreBackup(dir, src); err == nil {
			t.Fatalf("%s : restauration acceptée à tort", name)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "data.json"))
	if string(before) != string(after) {
		t.Fatal("data.json modifié par une restauration refusée")
	}
}

func TestRestoreValid(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(d *Data) error { d.Accounts = append(d.Accounts, &Account{Name: "avant"}); return nil }); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "ancien.json")
	os.WriteFile(src, []byte(`{"version": 2, "settings": {"effort": "low", "minGapMinutes": 30}, "accounts": [{"name": "restaure"}], "posts": [], "items": [], "learnings": [], "log": [], "auth": {}}`), 0o600)
	cur, _ := os.ReadFile(filepath.Join(dir, "data.json"))

	saved, err := restoreBackup(dir, src)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(saved)
	if err != nil || string(old) != string(cur) {
		t.Fatalf("copie de l'état courant absente ou différente : %v", err)
	}
	if !strings.HasPrefix(filepath.Base(saved), preRestorePrefix) {
		t.Fatalf("nom inattendu : %s", saved)
	}
	s2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := s2.Snapshot()
	if len(d.Accounts) != 1 || d.Accounts[0].Name != "restaure" {
		t.Fatalf("état non restauré : %+v", d.Accounts)
	}
	st, _ := os.Stat(filepath.Join(dir, "data.json"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("permissions %v", st.Mode().Perm())
	}
}
