package sftp_probe

import "testing"

func TestRank_VerifiedDescending(t *testing.T) {
	a := Aggregate{Candidate: Candidate{Label: "a"}, Verified: 5, Total: 10}
	b := Aggregate{Candidate: Candidate{Label: "b"}, Verified: 10, Total: 10}
	c := Aggregate{Candidate: Candidate{Label: "c"}, Verified: 3, Total: 10}
	got := Rank([]Aggregate{a, b, c})
	if got[0].Candidate.Label != "b" {
		t.Errorf("want b first; got %s", got[0].Candidate.Label)
	}
	if got[2].Candidate.Label != "c" {
		t.Errorf("want c last; got %s", got[2].Candidate.Label)
	}
}

func TestRank_StageDiversityTiebreak(t *testing.T) {
	single := Aggregate{
		Candidate: Candidate{Label: "single"},
		Verified:  0, Total: 10,
		Stages: map[Stage]int{StageDecrypt: 10},
	}
	multi := Aggregate{
		Candidate: Candidate{Label: "multi"},
		Verified:  0, Total: 10,
		Stages: map[Stage]int{
			StageDecrypt:      4,
			StageDecompress:   4,
			StageHashMismatch: 2,
		},
	}
	got := Rank([]Aggregate{multi, single})
	if got[0].Candidate.Label != "single" {
		t.Errorf("want single first (one failure stage); got %s",
			got[0].Candidate.Label)
	}
}

func TestRank_EmptyDoesNotPanic(t *testing.T) {
	got := Rank(nil)
	if got != nil && len(got) != 0 {
		t.Errorf("want empty; got %v", got)
	}
}

func TestAggregateAdd(t *testing.T) {
	var agg Aggregate
	agg.Add(SampleResult{Ok: true, Stage: StageOK})
	agg.Add(SampleResult{Ok: false, Stage: StageDecrypt})
	if agg.Verified != 1 {
		t.Errorf("Verified=%d want 1", agg.Verified)
	}
	if agg.Total != 2 {
		t.Errorf("Total=%d want 2", agg.Total)
	}
	if agg.Stages[StageOK] != 1 || agg.Stages[StageDecrypt] != 1 {
		t.Errorf("Stages = %v", agg.Stages)
	}
}
