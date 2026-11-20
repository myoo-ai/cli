package likelihood

import (
	"math"
	"testing"
)

var questions = Rate{Signal: "questions", Human: 1, AI: 0.025, Spread: 1, MaxAI: 0.75, MaxHuman: 2.5}

func TestRateDirection(t *testing.T) {
	human := questions.Evidence(200, 100, "").LogOdds
	ai := questions.Evidence(0, 100, "").LogOdds
	if human >= -2 || ai <= 0.5 {
		t.Errorf("human-rate evidence %.2f, AI-rate evidence %.2f; want clearly negative and positive", human, ai)
	}
}

// TestRateShrinks checks that a small sample tells us less than a large one
// at the same rate. A question in 1000 words isn't the same as 100 in 100000.
func TestRateShrinks(t *testing.T) {
	small := questions.Evidence(0, 0.2, "").LogOdds
	large := questions.Evidence(0, 50, "").LogOdds
	if math.Abs(small) >= math.Abs(large)/2 {
		t.Errorf("200 words without questions give %.2f, 50000 words give %.2f; want the small sample much weaker", small, large)
	}
	if e := questions.Evidence(0, 0, "").LogOdds; math.Abs(e) > 0.1 {
		t.Errorf("no exposure gives %.2f, want about 0", e)
	}
}

func TestBounds(t *testing.T) {
	r := Rate{Human: 0.03, AI: 10, Spread: 1.5, MaxAI: 4}
	if e := r.Evidence(100000, 1000, "").LogOdds; e > 4 || e < 3.9 {
		t.Errorf("overwhelming evidence gives %.2f, want just under MaxAI 4", e)
	}
	if e := r.Evidence(0, 1000, "").LogOdds; e != 0 {
		t.Errorf("one-sided signal without occurrences gives %.2f, want 0", e)
	}
}

func TestShare(t *testing.T) {
	s := Share{Human: 0.8, AI: 0.35, Spread: 0.5, MaxAI: 3, MaxHuman: 3}
	if e := s.Evidence(850, 1000, "").LogOdds; e > -2 {
		t.Errorf("85 %% human-style items give %.2f, want strongly negative", e)
	}
	if e := s.Evidence(300, 1000, "").LogOdds; e < 2 {
		t.Errorf("30 %% human-style items give %.2f, want strongly positive", e)
	}
	if e := s.Evidence(1, 2, "").LogOdds; math.Abs(e) > 0.5 {
		t.Errorf("two items give %.2f, want weak evidence", e)
	}
}

func TestPercent(t *testing.T) {
	if p := Percent(-2.2, nil); math.Abs(p-10) > 0.1 {
		t.Errorf("prior -2.2 alone gives %.1f %%, want 10 %%", p)
	}
	if p := Percent(-2.2, []Evidence{{LogOdds: 3}, {LogOdds: 3}}); p < 97 {
		t.Errorf("two strong signals give %.1f %%, want above 97 %%", p)
	}
}
