package main

import (
	"context"
	"errors"
	"testing"
)

func TestEmptyEditDoesNotOverwriteDraft(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "Un texte à garder")
	if _, err := e.st.EditPost(p.ID, ""); err == nil {
		t.Fatal("un texte vide aurait dû être refusé")
	}
	if _, err := e.st.EditPost(p.ID, "   \n"); err == nil {
		t.Fatal("un texte blanc aurait dû être refusé")
	}
	if got := e.st.store.Snapshot().Post(p.ID); got.Text != "Un texte à garder" || len(got.History) != 0 {
		t.Fatalf("brouillon altéré : %q, historique %d", got.Text, len(got.History))
	}
	// effacement explicite
	if got, err := e.st.EditPostOpts(p.ID, "", EditOptions{Clear: true}); err != nil || got.Text != "" {
		t.Fatalf("effacement explicite refusé : %v", err)
	}
}

func TestUnchangedEditAddsNoVersion(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "Identique")
	got, err := e.st.EditPost(p.ID, "Identique")
	if err != nil || got.Version != 0 || len(got.History) != 0 {
		t.Fatalf("enregistrement sans changement : err=%v version=%d historique=%d", err, got.Version, len(got.History))
	}
}

func TestStaleEditRefused(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "v1")
	if _, err := e.st.EditPost(p.ID, "v2"); err != nil {
		t.Fatal(err)
	}
	old := 0
	if _, err := e.st.EditPostOpts(p.ID, "v3", EditOptions{ExpectedVersion: &old}); !errors.Is(err, ErrStale) {
		t.Fatalf("version périmée acceptée : %v", err)
	}
	cur := 1
	if _, err := e.st.EditPostOpts(p.ID, "v3", EditOptions{ExpectedVersion: &cur}); err != nil {
		t.Fatal(err)
	}
}

func TestStaleAIRewriteRefused(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "texte initial")
	// pendant l'appel IA, Allan corrige le texte
	e.ai.fail = func(string) error {
		_, err := e.st.EditPost(p.ID, "correction d'Allan, plus récente")
		return err
	}
	_, err := e.st.RewritePost(context.Background(), p.ID, "plus court")
	if !errors.Is(err, ErrStale) {
		t.Fatalf("réécriture tardive acceptée : %v", err)
	}
	if got := e.st.store.Snapshot().Post(p.ID); got.Text != "correction d'Allan, plus récente" {
		t.Fatalf("la correction a été écrasée : %q", got.Text)
	}
}

func TestRestoreVersionCreatesNewVersion(t *testing.T) {
	e := newEnv(t)
	p := e.draft(t, "v1")
	_, _ = e.st.EditPost(p.ID, "v2")
	_, _ = e.st.EditPost(p.ID, "v3")
	got, err := e.st.RestorePost(p.ID, 0) // History[0] = "v1"
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "v1" || len(got.History) != 3 || got.History[2].Text != "v3" || got.Version != 3 {
		t.Fatalf("restauration : texte %q, historique %d, version %d", got.Text, len(got.History), got.Version)
	}
	if got.History[0].Text != "v1" || got.History[1].Text != "v2" {
		t.Fatal("l'historique existant a été réécrit")
	}
	if _, err := e.st.RestorePost(p.ID, 9); err == nil {
		t.Fatal("index inconnu accepté")
	}
}
