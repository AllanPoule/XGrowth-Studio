package main

// Boucles du planificateur : la publication a la sienne, indépendante des
// traitements IA (veille, tri, actualités, campagnes, synthèse, analyse).
// Un fournisseur IA lent ne retarde donc jamais un post programmé.
//
// Architecture volontairement simple : deux goroutines et deux tickers, un
// délai par appel IA (context.WithTimeout) et un sémaphore de concurrence.
// Ni file externe ni dépendance nouvelle.

import (
	"context"
	"sync"
	"time"
)

const (
	// aiCallTimeout : durée maximale d'un appel IA lancé par le planificateur.
	aiCallTimeout = 3 * time.Minute
	// aiMaxConcurrent : appels IA simultanés au plus, tous traitements confondus.
	aiMaxConcurrent = 2
	// schedulerLoopPublish et schedulerLoopAI : noms des boucles (centre de contrôle).
	schedulerLoopPublish = "publish"
	schedulerLoopAI      = "ai"
)

type schedCtxKey struct{}

// schedulerCtx marque un contexte comme venant du planificateur : seuls ses
// appels IA reçoivent le délai maximal et passent par le sémaphore (les
// demandes manuelles de l'utilisateur ne sont pas coupées).
func schedulerCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, schedCtxKey{}, true)
}

// guardedAI borne les appels IA du planificateur.
type guardedAI struct {
	AI
	sem     chan struct{}
	timeout time.Duration
}

func (g guardedAI) JSON(ctx context.Context, system, user string, schema map[string]any, out any) error {
	if v, _ := ctx.Value(schedCtxKey{}).(bool); !v {
		return g.AI.JSON(ctx, system, user, schema, out)
	}
	select {
	case g.sem <- struct{}{}:
		defer func() { <-g.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}
	cctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	return g.AI.JSON(cctx, system, user, schema, out)
}

func (s *Studio) aiSem() chan struct{} {
	s.loopSt.mu.Lock()
	defer s.loopSt.mu.Unlock()
	if s.loopSt.sem == nil {
		s.loopSt.sem = make(chan struct{}, aiMaxConcurrent)
	}
	return s.loopSt.sem
}

func (s *Studio) aiTimeout() time.Duration {
	if s.aiCallTimeout > 0 {
		return s.aiCallTimeout
	}
	return aiCallTimeout
}

// LoopStatus : état d'une boucle du planificateur.
type LoopStatus struct {
	LastRun *time.Time `json:"lastRun"` // début du dernier passage terminé ou en cours
	LastEnd *time.Time `json:"lastEnd"` // fin du dernier passage terminé
	Running bool       `json:"running"` // un passage est en cours
	Skipped int        `json:"skipped"` // passages sautés car le précédent tournait encore
}

type loopState struct {
	mu    sync.Mutex
	loops map[string]*LoopStatus
	sem   chan struct{}
}

// beginLoop note le début d'un passage ; false si le précédent tourne encore
// (pas de chevauchement d'une même tâche).
func (s *Studio) beginLoop(name string) bool {
	s.loopSt.mu.Lock()
	defer s.loopSt.mu.Unlock()
	if s.loopSt.loops == nil {
		s.loopSt.loops = map[string]*LoopStatus{}
	}
	l := s.loopSt.loops[name]
	if l == nil {
		l = &LoopStatus{}
		s.loopSt.loops[name] = l
	}
	if l.Running {
		l.Skipped++
		return false
	}
	now := s.now().UTC()
	l.Running, l.LastRun = true, &now
	return true
}

func (s *Studio) endLoop(name string) {
	s.loopSt.mu.Lock()
	defer s.loopSt.mu.Unlock()
	now := s.now().UTC()
	if l := s.loopSt.loops[name]; l != nil {
		l.Running, l.LastEnd = false, &now
	}
}

// SchedulerStatus : l'heure du dernier passage de chaque boucle.
func (s *Studio) SchedulerStatus() map[string]LoopStatus {
	s.loopSt.mu.Lock()
	defer s.loopSt.mu.Unlock()
	out := map[string]LoopStatus{schedulerLoopPublish: {}, schedulerLoopAI: {}}
	for k, v := range s.loopSt.loops {
		out[k] = *v
	}
	return out
}

// runSchedulerLoops lance les deux boucles et ne rend la main qu'une fois les
// deux arrêtées (ctx annulé) : un passage en cours reçoit l'annulation.
func runSchedulerLoops(ctx context.Context, st *Studio, every time.Duration, onResult func(map[string]any)) {
	if every < 10*time.Second {
		every = 10 * time.Second
	}
	var wg sync.WaitGroup
	loop := func(name string, step func(context.Context) map[string]any) {
		defer wg.Done()
		run := func() {
			if out := step(ctx); onResult != nil && len(out) > 1 {
				onResult(out)
			}
		}
		run()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}
	wg.Add(2)
	go loop(schedulerLoopPublish, st.TickPublish)
	go loop(schedulerLoopAI, st.TickAI)
	wg.Wait()
}
