package sftp_probe

import "sort"

// Aggregate accumulates SampleResult verdicts for a single
// Candidate. Verified counts the OK results; Total counts every
// Add call. Stages is the per-stage histogram used for the
// tiebreak rule in Rank.
type Aggregate struct {
	Candidate Candidate
	Verified  int
	Total     int
	Stages    map[Stage]int
}

// Add folds one SampleResult into the aggregate. Lazy-initializes
// Stages on first call so callers can use Aggregate{} as a zero
// value safely.
func (a *Aggregate) Add(r SampleResult) {
	if a.Stages == nil {
		a.Stages = make(map[Stage]int)
	}
	a.Total++
	if r.Ok {
		a.Verified++
	}
	a.Stages[r.Stage]++
}

// Rank orders aggregates by Verified descending, with ties broken
// by stage diversity: among aggregates with equal Verified, the
// one whose failures concentrate at fewer Stages ranks higher.
// Single-stage failures are more diagnosable than multi-stage
// flailers — the user gets a clearer "this candidate is wrong
// because X" signal.
//
// Empty input returns the same nil/empty slice; the function does
// not panic on len(in) == 0.
func Rank(in []Aggregate) []Aggregate {
	if len(in) == 0 {
		return in
	}
	out := append([]Aggregate(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Verified != out[j].Verified {
			return out[i].Verified > out[j].Verified
		}
		di := distinctFailureStages(out[i])
		dj := distinctFailureStages(out[j])
		return di < dj
	})
	return out
}

// distinctFailureStages counts stages other than StageOK that
// have at least one count. Used by Rank's tiebreak.
func distinctFailureStages(a Aggregate) int {
	n := 0
	for stage, count := range a.Stages {
		if stage == StageOK || count == 0 {
			continue
		}
		n++
	}
	return n
}
