package main

// Réorganiser la file de publication à la main (glisser-déposer dans le
// studio) sans sortir de la programmation de chaque compte : les heures restent
// celles que la configuration a données (créneaux, écarts, limites) et ne font
// que changer de post. Déposer un brouillon dans la file le valide (signé) puis
// l'insère à l'endroit voulu ; déposer un post sur un jour le cale au premier
// créneau libre de ce jour.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// reassign redonne aux posts d'un compte, dans l'ordre voulu, les heures
// qu'ils occupaient déjà (triées) : l'ensemble des heures ne change pas.
func reassign(ps []*Post, now time.Time) {
	times := make([]time.Time, 0, len(ps))
	for _, p := range ps {
		times = append(times, *p.ScheduledAt)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	for i, p := range ps {
		t := times[i]
		if !t.Equal(*p.ScheduledAt) {
			p.ScheduledAt, p.UpdatedAt = &t, now
		}
	}
}

// ReorderQueue applique un nouvel ordre à des posts planifiés. L'ordre vaut
// compte par compte : chaque compte garde ses heures, distribuées dans l'ordre
// donné.
func (s *Studio) ReorderQueue(ids []string) ([]*Post, error) {
	if len(ids) < 2 {
		return nil, errors.New("indiquez au moins deux posts à réordonner")
	}
	var out []*Post
	err := s.store.Update(func(d *Data) error {
		byAccount := map[string][]*Post{}
		var order []string
		seen := map[string]bool{}
		for _, id := range ids {
			p := d.Post(id)
			if p == nil {
				return fmt.Errorf("post %q inconnu", id)
			}
			if seen[id] {
				return fmt.Errorf("post %q en double", id)
			}
			seen[id] = true
			if p.Status != StatusScheduled || p.ScheduledAt == nil {
				return fmt.Errorf("le post %s n'est pas planifié : seul un post planifié peut être déplacé", id)
			}
			if p.CampaignID != "" {
				return fmt.Errorf("le post %s est une réponse de campagne : elle suit le rythme de sa campagne", id)
			}
			if _, ok := byAccount[p.Account]; !ok {
				order = append(order, p.Account)
			}
			byAccount[p.Account] = append(byAccount[p.Account], p)
		}
		now := s.now().UTC()
		for _, acc := range order {
			reassign(byAccount[acc], now)
			out = append(out, byAccount[acc]...)
		}
		return nil
	})
	return out, err
}

// ScheduleDraft valide un brouillon (signé par `by`) puis, si `before` désigne
// un post planifié du même compte, l'insère juste avant lui : les heures du
// compte, y compris le nouveau créneau, sont redistribuées dans le nouvel ordre.
func (s *Studio) ScheduleDraft(id, by, before string) (*Post, error) {
	p, err := s.Validate(id, by, nil, false)
	if err != nil || before == "" || before == id {
		return p, err
	}
	err = s.store.Update(func(d *Data) error {
		np, target := d.Post(id), d.Post(before)
		if target == nil || target.Account != np.Account || target.Status != StatusScheduled || target.ScheduledAt == nil {
			return nil // pas de cible valable : le post garde le créneau que la programmation lui a donné
		}
		var queue []*Post
		for _, x := range d.Posts {
			if x.Account == np.Account && x.Status == StatusScheduled && x.ScheduledAt != nil && x.ID != np.ID && x.CampaignID == "" {
				queue = append(queue, x)
			}
		}
		sort.Slice(queue, func(i, j int) bool { return queue[i].ScheduledAt.Before(*queue[j].ScheduledAt) })
		ordered := make([]*Post, 0, len(queue)+1)
		for _, x := range queue {
			if x.ID == before {
				ordered = append(ordered, np)
			}
			ordered = append(ordered, x)
		}
		reassign(ordered, s.now().UTC())
		p = np
		return nil
	})
	return p, err
}

// MoveToDay cale un post planifié au premier créneau libre d'un jour donné
// (AAAA-MM-JJ, dans le fuseau du compte), selon la programmation du compte.
func (s *Studio) MoveToDay(id, day string) (*Post, error) {
	var out *Post
	err := s.store.Update(func(d *Data) error {
		p := d.Post(id)
		if p == nil {
			return fmt.Errorf("post %q inconnu", id)
		}
		if p.Status != StatusScheduled {
			return fmt.Errorf("le post %s n'est pas planifié : seul un post planifié peut être déplacé", id)
		}
		if p.CampaignID != "" {
			return errors.New("une réponse de campagne suit le rythme de sa campagne : elle ne se déplace pas")
		}
		a := d.Account(p.Account)
		if a == nil {
			return fmt.Errorf("compte %q inconnu", p.Account)
		}
		loc := location(a)
		start, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(day), loc)
		if err != nil {
			return fmt.Errorf("jour %q invalide (AAAA-MM-JJ)", day)
		}
		now := s.now()
		if start.AddDate(0, 0, 1).Before(now) {
			return errors.New("ce jour est passé")
		}
		after := start.Add(-3 * time.Minute) // NextSlot cherche après « after + 2 min »
		if after.Before(now) {
			after = now
		}
		slot, err := NextSlot(d, a, after, p.ID)
		if err != nil {
			return err
		}
		if slot.In(loc).Format("2006-01-02") != start.Format("2006-01-02") {
			return fmt.Errorf("plus de place le %s selon la programmation de @%s (créneaux, écart minimal, posts par jour)", start.Format("02/01"), a.Handle)
		}
		p.ScheduledAt, p.UpdatedAt = &slot, now.UTC()
		out = p
		return nil
	})
	return out, err
}
