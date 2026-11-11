package main

import (
	"slices"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
)

// recallAt is the share of want found in the first k of got; k <= 0 looks at
// all of got.
func recallAt(got, want []corpus.DocID, k int) float64 {
	if len(want) == 0 {
		return 0
	}
	if k > 0 && len(got) > k {
		got = got[:k]
	}
	found := 0
	for _, id := range want {
		if slices.Contains(got, id) {
			found++
		}
	}
	return float64(found) / float64(len(want))
}

// reciprocalRank is 1 over the 1-based position of the first of got that's
// in want, or 0 if none is.
func reciprocalRank[T comparable](got, want []T) float64 {
	for i, item := range got {
		if slices.Contains(want, item) {
			return 1 / float64(i+1)
		}
	}
	return 0
}

// hitAt reports whether any of the first k of got is in want.
func hitAt[T comparable](got, want []T, k int) bool {
	if len(got) > k {
		got = got[:k]
	}
	return slices.ContainsFunc(got, func(item T) bool { return slices.Contains(want, item) })
}

// mean is the average of xs, or 0 for none.
func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// percentile is the p'th percentile (0 to 1) of xs by nearest rank, or 0 for
// none.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(xs))
	return sorted[min(len(sorted)-1, int(p*float64(len(sorted))))]
}
