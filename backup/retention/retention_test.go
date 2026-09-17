package retention

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func daily(from time.Time, days int) []Commit {
	commits := make([]Commit, days)
	for i := range commits {
		commits[i] = Commit{ReceivedAt: from.AddDate(0, 0, -i)}
	}

	return commits
}

func kept(decisions []Decision) int {
	n := 0
	for _, d := range decisions {
		if d.Keep {
			n++
		}
	}

	return n
}

func TestEvaluateThinsDailiesToWeekliesToMonthlies(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	commits := daily(now, 120)

	got := Evaluate(commits, Policy{KeepLast: 1, KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 3}, now)

	require.Equal(t, []string{"latest", "last", "daily", "weekly", "monthly"}, got[0].Reasons)

	for i := 1; i < 7; i++ {
		require.True(t, got[i].Keep, "day %d", i)
		require.Contains(t, got[i].Reasons, "daily")
	}

	require.False(t, got[7].Keep, "the eighth daily is thinned")

	weeklies, monthlies := 0, 0
	for _, d := range got {
		for _, r := range d.Reasons {
			switch r {
			case "weekly":
				weeklies++
			case "monthly":
				monthlies++
			}
		}
	}

	require.Equal(t, 4, weeklies)
	require.Equal(t, 3, monthlies)
	require.Less(t, kept(got), 14)
}

func TestEvaluateCountsOnlyPeriodsWithCommits(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	// an agent that stopped six months ago
	commits := daily(now.AddDate(0, -6, 0), 3)

	got := Evaluate(commits, Policy{KeepLast: 1, KeepDaily: 3}, now)
	require.Equal(t, 3, kept(got), "old dailies are still the last three daily periods")
}

func TestEvaluateKeepsTheNewestAndPinned(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	commits := daily(now, 5)
	commits[3].Pinned = true

	got := Evaluate(commits, Policy{KeepLast: 1}, now)

	require.Equal(t, []string{"latest", "last"}, got[0].Reasons)
	require.False(t, got[1].Keep)
	require.Equal(t, []string{"pinned"}, got[3].Reasons)
	require.False(t, got[4].Keep)
}

func TestEvaluatePartialCheckpoints(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	commits := []Commit{
		{ReceivedAt: now, Partial: true},
		{ReceivedAt: now.Add(-time.Hour)},
	}

	got := Evaluate(commits, Policy{KeepLast: 1}, now)
	require.Equal(t, []string{"latest"}, got[0].Reasons, "a newest checkpoint is live but counts for nothing")
	require.Equal(t, []string{"last"}, got[1].Reasons, "the real commit is still the last one")

	commits = []Commit{
		{ReceivedAt: now},
		{ReceivedAt: now.Add(-time.Hour), Partial: true},
		{ReceivedAt: now.Add(-2 * time.Hour)},
	}

	got = Evaluate(commits, Policy{KeepLast: 2}, now)
	require.True(t, got[0].Keep)
	require.False(t, got[1].Keep, "a superseded checkpoint is retired")
	require.True(t, got[2].Keep, "and does not use up a keep_last slot")
}

func TestEvaluateKeepWithin(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	commits := []Commit{
		{ReceivedAt: now},
		{ReceivedAt: now.Add(-time.Minute)},
		{ReceivedAt: now.Add(-2 * time.Minute)},
		{ReceivedAt: now.Add(-48 * time.Hour)},
	}

	got := Evaluate(commits, Policy{KeepLast: 1, KeepWithin: 24 * time.Hour}, now)
	require.Equal(t, 3, kept(got), "a flood of commits inside the window cannot push each other out")
	require.False(t, got[3].Keep)

	commits = append(commits, Commit{ReceivedAt: now.AddDate(-50, 0, 0)})
	require.NoError(t, KeepAll.Validate())
	require.Equal(t, 5, kept(Evaluate(commits, KeepAll, now)), "keep-all retires nothing")
}

func TestEvaluateInputOrderIsPreserved(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	commits := []Commit{
		{ReceivedAt: now.Add(-2 * time.Hour)},
		{ReceivedAt: now},
		{ReceivedAt: now.Add(-time.Hour)},
	}

	got := Evaluate(commits, Policy{KeepLast: 1}, now)
	require.False(t, got[0].Keep)
	require.True(t, got[1].Keep)
	require.False(t, got[2].Keep)
}

func TestPolicyValidate(t *testing.T) {
	require.ErrorIs(t, Policy{}.Validate(), ErrInvalidPolicy, "keep-all by zeros is rejected")
	require.ErrorIs(t, Policy{KeepLast: 1, KeepDaily: -1}.Validate(), ErrInvalidPolicy)
	require.NoError(t, Policy{KeepLast: 1}.Validate())
}

func TestPolicyJSON(t *testing.T) {
	p := Policy{KeepLast: 2, KeepDaily: 7, KeepWithin: 36 * time.Hour}

	data, err := json.Marshal(p)
	require.NoError(t, err)
	require.JSONEq(t, `{"keep_last":2,"keep_daily":7,"keep_within":"36h0m0s"}`, string(data))

	back, err := Parse(data)
	require.NoError(t, err)
	require.Equal(t, p, back)

	_, err = Parse([]byte(`{"keep_last":0}`))
	require.ErrorIs(t, err, ErrInvalidPolicy)

	_, err = Parse([]byte(`{"keep_last":1,"keep_within":"soon"}`))
	require.ErrorIs(t, err, ErrInvalidPolicy)
}
