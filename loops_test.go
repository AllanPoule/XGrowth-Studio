package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// blockingAI : un fournisseur qui ne répond jamais (il ne rend la main que si
// son contexte est annulé).
type blockingAI struct {
	mu      sync.Mutex
	started chan struct{}
	calls   int
	errs    []error
}

func newBlockingAI() *blockingAI { return &blockingAI{started: make(chan struct{}, 100)} }

func (b *blockingAI) JSON(ctx context.Context, _, _ string, _ map[string]any, _ any) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.started <- struct{}{}
	<-ctx.Done()
	b.mu.Lock()
	b.errs = append(b.errs, ctx.Err())
	b.mu.Unlock()
	return ctx.Err()
}

func (b *blockingAI) result() (int, []error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls, append([]error(nil), b.errs...)
}

// blockedEnv : un studio dont l'IA ne répond jamais, avec une campagne qui la
// sollicite à chaque passage IA, et un post programmé déjà dû.
func blockedEnv(t *testing.T) (*env, *blockingAI, *Post) {
	e := newEnv(t)
	e.now = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	b := newBlockingAI()
	e.st.ai = func(Settings) AI { return b }
	e.x.feed = []Tweet{feedTweet("1", "alice", "Impossible de savoir ce que l'agent a modifié", 1)}
	if _, err := e.st.SaveCampaign(testCampaign()); err != nil {
		t.Fatal(err)
	}
	p := e.draft(t, "à l'heure")
	if _, err := e.st.Validate(p.ID, "Allan", nil, true); err != nil {
		t.Fatal(err)
	}
	return e, b, p
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("délai dépassé : %s", what)
	}
}

// Critère : fournisseur IA bloqué, un post programmé part quand même à l'heure.
func TestPublishNotDelayedByBlockedAI(t *testing.T) {
	e, b, p := blockedEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.st.TickAI(ctx); close(done) }()
	waitFor(t, b.started, "l'IA n'a pas été sollicitée")

	start := time.Now()
	out := e.st.TickPublish(ctx)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("la publication a attendu l'IA (%s)", time.Since(start))
	}
	if out["published"] == nil || e.st.store.Snapshot().Post(p.ID).Status != StatusPublished {
		t.Fatalf("le post dû n'est pas parti : %v", out)
	}
	cancel()
	<-done
}

// Même critère par les vraies boucles, et arrêt propre des deux.
func TestSchedulerLoopsPublishWhileAIBlocked(t *testing.T) {
	e, b, p := blockedEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runSchedulerLoops(ctx, e.st, time.Minute, nil); close(done) }()
	waitFor(t, b.started, "l'IA n'a pas été sollicitée")

	deadline := time.Now().Add(5 * time.Second)
	for e.st.store.Snapshot().Post(p.ID).Status != StatusPublished {
		if time.Now().After(deadline) {
			t.Fatal("le post n'est pas parti pendant que l'IA est bloquée")
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := e.st.SchedulerStatus()
	if st[schedulerLoopPublish].LastRun == nil || st[schedulerLoopAI].LastRun == nil || !st[schedulerLoopAI].Running {
		t.Fatalf("état des boucles : %+v", st)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("les boucles ne s'arrêtent pas à l'annulation")
	}
	if st := e.st.SchedulerStatus(); st[schedulerLoopAI].Running || st[schedulerLoopAI].LastEnd == nil {
		t.Fatalf("passage IA non terminé : %+v", st[schedulerLoopAI])
	}
}

// Critère : délai maximum d'un appel IA du planificateur.
func TestAICallTimeout(t *testing.T) {
	e, b, _ := blockedEnv(t)
	e.st.aiCallTimeout = 50 * time.Millisecond
	start := time.Now()
	e.st.TickAI(context.Background())
	if time.Since(start) > 3*time.Second {
		t.Fatalf("TickAI n'a pas été borné (%s)", time.Since(start))
	}
	calls, errs := b.result()
	if calls == 0 || len(errs) != calls {
		t.Fatalf("appels %d, retours %d", calls, len(errs))
	}
	for _, err := range errs {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("l'appel devait finir en délai dépassé : %v", err)
		}
	}
}

// Un appel manuel (hors planificateur) n'est pas coupé par ce délai.
func TestAICallTimeoutOnlyForScheduler(t *testing.T) {
	e := newEnv(t)
	e.st.aiCallTimeout = 20 * time.Millisecond
	b := newBlockingAI()
	e.st.ai = func(Settings) AI { return b }
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = e.st.aiFor(nil).JSON(ctx, "", "", nil, nil)
	_, errs := b.result()
	if len(errs) != 1 || !errors.Is(errs[0], context.DeadlineExceeded) {
		t.Fatal("erreur inattendue")
	}
	// c'est le délai du contexte appelant (300 ms) qui a coupé, pas les 20 ms
}

// Pas de chevauchement : un passage IA lancé pendant qu'un autre tourne est sauté.
func TestAITickDoesNotOverlap(t *testing.T) {
	e, b, _ := blockedEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.st.TickAI(ctx); close(done) }()
	waitFor(t, b.started, "l'IA n'a pas été sollicitée")
	e.st.TickAI(context.Background()) // doit rendre la main aussitôt
	if st := e.st.SchedulerStatus(); st[schedulerLoopAI].Skipped != 1 {
		t.Fatalf("passage non sauté : %+v", st[schedulerLoopAI])
	}
	if calls, _ := b.result(); calls != 1 {
		t.Fatalf("appels IA : %d, attendu 1", calls)
	}
	cancel()
	<-done
}

// Concurrence limitée : jamais plus d'appels IA simultanés que le sémaphore.
func TestAIConcurrencyLimited(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	cur, max := 0, 0
	slow := aiFunc(func(ctx context.Context) error {
		mu.Lock()
		cur++
		if cur > max {
			max = cur
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		cur--
		mu.Unlock()
		return nil
	})
	e.st.ai = func(Settings) AI { return slow }
	ctx := schedulerCtx(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = e.st.aiFor(nil).JSON(ctx, "", "", nil, nil) }()
	}
	wg.Wait()
	if max > aiMaxConcurrent || max == 0 {
		t.Fatalf("appels simultanés : %d (limite %d)", max, aiMaxConcurrent)
	}
}

type aiFunc func(ctx context.Context) error

func (f aiFunc) JSON(ctx context.Context, _, _ string, _ map[string]any, _ any) error { return f(ctx) }
