package replan

import (
	"math"
	"testing"
	"time"
)

func at(min int) time.Time {
	return time.Date(2026, 5, 14, 10, min, 0, 0, time.UTC)
}

func TestDetectReversalsBasic(t *testing.T) {
	signals := []Signal{
		{Ts: at(0), Kind: "todowrite", Pending: 1, InProgress: 1, Completed: 0, Total: 2},
		{Ts: at(1), Kind: "todowrite", Pending: 0, InProgress: 1, Completed: 1, Total: 2},
		// item moved BACK to pending — classic reversal:
		{Ts: at(2), Kind: "todowrite", Pending: 1, InProgress: 1, Completed: 0, Total: 2},
	}
	rev := DetectReversals(signals)
	if len(rev) != 1 {
		t.Fatalf("got %d reversals, want 1: %+v", len(rev), rev)
	}
	if rev[0].PendingIncrease != 1 {
		t.Errorf("PendingIncrease: %d", rev[0].PendingIncrease)
	}
}

func TestDetectReversalsIgnoresAdditions(t *testing.T) {
	// Adding new items shouldn't count as a reversal.
	signals := []Signal{
		{Ts: at(0), Kind: "todowrite", Pending: 0, Total: 0},
		{Ts: at(1), Kind: "todowrite", Pending: 3, Total: 3}, // 3 new pending added — not a reversal
	}
	if rev := DetectReversals(signals); len(rev) != 0 {
		t.Errorf("expected no reversals for added items, got %+v", rev)
	}
}

func TestDetectReversalsMultiple(t *testing.T) {
	signals := []Signal{
		{Ts: at(0), Kind: "todowrite", Pending: 2, Total: 2},
		{Ts: at(1), Kind: "todowrite", Pending: 1, InProgress: 1, Total: 2},
		{Ts: at(2), Kind: "todowrite", Pending: 0, InProgress: 1, Completed: 1, Total: 2},
		{Ts: at(3), Kind: "todowrite", Pending: 2, Total: 2}, // both items reverted
	}
	rev := DetectReversals(signals)
	if len(rev) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(rev), rev)
	}
	if rev[0].PendingIncrease != 2 {
		t.Errorf("PendingIncrease: %d", rev[0].PendingIncrease)
	}
}

func TestDetectReversalsHonorsChronologicalOrder(t *testing.T) {
	// Out-of-order input — Detect should sort internally.
	signals := []Signal{
		{Ts: at(2), Kind: "todowrite", Pending: 1, Total: 2},
		{Ts: at(0), Kind: "todowrite", Pending: 0, Total: 2},
		{Ts: at(1), Kind: "todowrite", Pending: 0, Total: 2},
	}
	rev := DetectReversals(signals)
	if len(rev) != 1 {
		t.Errorf("expected 1 reversal after sort, got %+v", rev)
	}
}

func TestDetectReversalsSkipsNonTodoWrite(t *testing.T) {
	signals := []Signal{
		{Ts: at(0), Kind: "pivot_phrase", Weight: 1.0},
		{Ts: at(1), Kind: "todowrite", Pending: 2, Total: 2},
		{Ts: at(2), Kind: "pivot_phrase", Weight: 1.0},
		{Ts: at(3), Kind: "todowrite", Pending: 3, Total: 3}, // grew, not a reversal
	}
	if rev := DetectReversals(signals); len(rev) != 0 {
		t.Errorf("expected 0 reversals, got %+v", rev)
	}
}

func TestComputeScoreSmoothlyApproaches1(t *testing.T) {
	cases := []struct {
		name    string
		signals []Signal
		want    float64 // expected score (tolerance ±0.05)
	}{
		{"empty", nil, 0.0},
		{"one pivot phrase ~0.18", []Signal{{Kind: "pivot_phrase", Weight: 1.0}}, 0.18},
		{"five pivot phrases ~0.63", []Signal{
			{Kind: "pivot_phrase", Weight: 1.0},
			{Kind: "pivot_phrase", Weight: 1.0},
			{Kind: "pivot_phrase", Weight: 1.0},
			{Kind: "pivot_phrase", Weight: 1.0},
			{Kind: "pivot_phrase", Weight: 1.0},
		}, 0.63},
		{"twenty pivots ~0.98", repeatPivot(20), 0.98},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeScore(c.signals, nil)
			if math.Abs(got-c.want) > 0.05 {
				t.Errorf("got %.3f want ≈%.3f", got, c.want)
			}
			if got < 0 || got > 1 {
				t.Errorf("out of [0,1]: %f", got)
			}
		})
	}
}

func TestComputeScoreReversalsCarryMoreWeight(t *testing.T) {
	pivotOnly := ComputeScore([]Signal{{Kind: "pivot_phrase", Weight: 1.0}}, nil)
	withReversal := ComputeScore(
		[]Signal{{Kind: "pivot_phrase", Weight: 1.0}},
		[]Reversal{{At: at(1), PendingIncrease: 1}},
	)
	if withReversal <= pivotOnly {
		t.Errorf("reversal should bump score above pivot-only: pivot=%.3f rev=%.3f", pivotOnly, withReversal)
	}
}

func repeatPivot(n int) []Signal {
	out := make([]Signal, n)
	for i := range out {
		out[i] = Signal{Kind: "pivot_phrase", Weight: 1.0}
	}
	return out
}
