// Package replan turns a stream of replan_signal events into a single 0–1
// score per session. The score answers: "how much did this session pivot
// vs. progress steadily?" — combining live signals (pivot phrases on user
// prompts) with derived ones (TodoWrite status reversals).
//
// All functions here are pure and dependency-free: the ingester is the
// only caller that has the SQLite store; everything else operates on
// already-loaded slices.
package replan

import (
	"math"
	"sort"
	"time"
)

// Signal is one normalized replan_signal row, as the ingester reads it
// from SQLite. We don't import the events package here to keep this
// package's contract small.
type Signal struct {
	Ts     time.Time
	Kind   string  // "pivot_phrase" | "todowrite" | "todowrite_reversal" | ...
	Weight float64 // hook-assigned weight; the ingester may overwrite

	// TodoWrite-specific fields. Zero for non-TodoWrite kinds.
	Pending    int
	InProgress int
	Completed  int
	Total      int
}

// DefaultReversalWeight is the weight we attach to a synthetic reversal
// signal — heavier than the raw TodoWrite weight because a reversal is a
// stronger thrash signal than a TodoWrite write itself.
const DefaultReversalWeight = 1.5

// Reversal records a detected TodoWrite status reversal between two
// consecutive TodoWrite signals.
type Reversal struct {
	At              time.Time
	PendingIncrease int // how many items moved back to pending
	// Note: "completed → pending" specifically isn't observable from our
	// counts (we don't have item identities), but a net pending-count
	// increase across signals where total stayed flat is a strong proxy.
}

// DetectReversals walks signals in chronological order and reports the
// indices in `signals` where pending-count strictly increased compared to
// the previous TodoWrite signal for the same session, while total stayed
// approximately the same (no new items added).
//
// We deliberately avoid heuristics on completed-vs-pending swings: items
// the user added since the last TodoWrite muddy the signal. A strict
// pending-increase keeps false positives low — the design doc's stance is
// that this is a proxy, not a measurement.
func DetectReversals(signals []Signal) []Reversal {
	if len(signals) < 2 {
		return nil
	}
	chronological := append([]Signal(nil), signals...)
	sort.SliceStable(chronological, func(i, j int) bool {
		return chronological[i].Ts.Before(chronological[j].Ts)
	})

	var out []Reversal
	var prev *Signal
	for i := range chronological {
		s := &chronological[i]
		if s.Kind != "todowrite" {
			continue
		}
		if prev != nil {
			// Net reversal: pending grew faster than total. If both grew
			// in lockstep, the user just added new pending items — no
			// thrash signal there.
			net := (s.Pending - prev.Pending) - (s.Total - prev.Total)
			if net > 0 {
				out = append(out, Reversal{
					At:              s.Ts,
					PendingIncrease: net,
				})
			}
		}
		prev = s
	}
	return out
}

// ComputeScore returns a 0–1 score given the raw signals plus the detected
// reversals.
//
// Formula (deliberately simple, easy to explain):
//
//	total_weight = sum(signal.weight) + Σ reversal.PendingIncrease * DefaultReversalWeight
//	score        = 1 - exp(-total_weight / scale)
//
// `scale` controls how quickly the score saturates: at total_weight =
// scale, score = 0.63; at 2*scale, 0.86. We pick scale = 5.0 so a session
// with ~5 weighted signals lands near 0.6 — "noticeable thrash."
//
// Pros of an exponential saturation: a session with 50 pivots doesn't
// dwarf one with 10 in the final number, which is what we want for a
// dashboard. Cons: it's not interpretable to 0.01 precision — but the
// dashboard slices by deciles anyway.
func ComputeScore(signals []Signal, reversals []Reversal) float64 {
	const scale = 5.0
	var total float64
	for _, s := range signals {
		if s.Weight > 0 {
			total += s.Weight
		}
	}
	for _, r := range reversals {
		w := DefaultReversalWeight
		if r.PendingIncrease > 1 {
			w *= float64(r.PendingIncrease)
		}
		total += w
	}
	if total <= 0 {
		return 0
	}
	score := 1 - math.Exp(-total/scale)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
