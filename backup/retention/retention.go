// Package retention decides which commits of a set a policy keeps.
package retention

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Period is how finely a bracket keeps: one commit per hour, day, week
// or month of the stretch it covers.
type Period string

// The periods a bracket may keep by.
const (
	Hourly  Period = "hourly"
	Daily   Period = "daily"
	Weekly  Period = "weekly"
	Monthly Period = "monthly"
)

// A Bracket keeps one commit per Period over a stretch of a set's past.
// Brackets run consecutively, each starting where the one before it
// ended, so For is how long this one lasts rather than how old its
// commits may be. A For of zero is the tail: everything older than the
// brackets before it, kept forever.
type Bracket struct {
	Period Period
	For    time.Duration
}

// Policy is what a set keeps. Brackets divide its past into consecutive
// stretches, each kept at its own granularity. The count rules beside
// them are restic-style: ORed with the brackets and with each other, and
// a period rule counts only periods that have commits. KeepLast is never
// below 1.
type Policy struct {
	Brackets    []Bracket
	KeepLast    int
	KeepHourly  int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
	KeepWithin  time.Duration
}

// Default is the policy a store applies until it sets its own.
var Default = Policy{KeepLast: 1, KeepDaily: 14, KeepWeekly: 8, KeepMonthly: 12}

// KeepAll retires nothing; a local index without an operator uses it.
var KeepAll = Policy{KeepLast: 1, KeepWithin: time.Duration(math.MaxInt64)}

// ErrInvalidPolicy wraps every validation failure.
var ErrInvalidPolicy = errors.New("invalid retention policy")

// Validate rejects a policy with a negative counter, a keep_last below 1
// or nothing to keep at all.
func (p Policy) Validate() error {
	if p.KeepLast < 1 {
		return fmt.Errorf("%w: keep_last must be at least 1", ErrInvalidPolicy)
	}

	for name, v := range map[string]int{"keep_hourly": p.KeepHourly, "keep_daily": p.KeepDaily, "keep_weekly": p.KeepWeekly, "keep_monthly": p.KeepMonthly} {
		if v < 0 {
			return fmt.Errorf("%w: %s is negative", ErrInvalidPolicy, name)
		}
	}

	if p.KeepWithin < 0 {
		return fmt.Errorf("%w: keep_within is negative", ErrInvalidPolicy)
	}

	for i, b := range p.Brackets {
		if _, ok := periods[b.Period]; !ok {
			return fmt.Errorf("%w: bracket %d keeps by %q, not one of hourly, daily, weekly or monthly",
				ErrInvalidPolicy, i, b.Period)
		}

		if b.For < 0 {
			return fmt.Errorf("%w: bracket %d lasts a negative time", ErrInvalidPolicy, i)
		}

		if b.For == 0 && i != len(p.Brackets)-1 {
			return fmt.Errorf("%w: bracket %d keeps %s forever, so the brackets after it never begin",
				ErrInvalidPolicy, i, b.Period)
		}
	}

	return nil
}

// periods is how a commit's place in each period is named, so that two
// commits of one period compare equal.
var periods = map[Period]func(time.Time) string{
	Hourly: func(t time.Time) string { return t.UTC().Format("2006-01-02T15") },
	Daily:  func(t time.Time) string { return t.UTC().Format("2006-01-02") },
	Weekly: func(t time.Time) string {
		y, w := t.UTC().ISOWeek()

		return fmt.Sprintf("%04d-W%02d", y, w)
	},
	Monthly: func(t time.Time) string { return t.UTC().Format("2006-01") },
}

// bracketAt is the bracket a commit of this age falls in, and whether
// the brackets reach that far back at all.
func (p Policy) bracketAt(age time.Duration) (int, bool) {
	var until time.Duration

	for i, b := range p.Brackets {
		if b.For == 0 {
			return i, true
		}

		until += b.For
		if age < until {
			return i, true
		}
	}

	return 0, false
}

type bracketJSON struct {
	Period Period `json:"period"`
	// absent for the tail, which lasts forever
	For string `json:"for,omitempty"`
}

type policyJSON struct {
	Brackets    []bracketJSON `json:"brackets,omitempty"`
	KeepLast    int           `json:"keep_last"`
	KeepHourly  int           `json:"keep_hourly,omitempty"`
	KeepDaily   int           `json:"keep_daily,omitempty"`
	KeepWeekly  int           `json:"keep_weekly,omitempty"`
	KeepMonthly int           `json:"keep_monthly,omitempty"`
	KeepWithin  string        `json:"keep_within,omitempty"`
}

// MarshalJSON writes every duration as a duration string.
func (p Policy) MarshalJSON() ([]byte, error) {
	out := policyJSON{KeepLast: p.KeepLast, KeepHourly: p.KeepHourly, KeepDaily: p.KeepDaily, KeepWeekly: p.KeepWeekly, KeepMonthly: p.KeepMonthly}
	if p.KeepWithin > 0 {
		out.KeepWithin = p.KeepWithin.String()
	}

	for _, b := range p.Brackets {
		written := bracketJSON{Period: b.Period}
		if b.For > 0 {
			written.For = b.For.String()
		}

		out.Brackets = append(out.Brackets, written)
	}

	return json.Marshal(out)
}

// UnmarshalJSON reads every duration as a duration string.
func (p *Policy) UnmarshalJSON(data []byte) error {
	var in policyJSON
	err := json.Unmarshal(data, &in)
	if err != nil {
		return err
	}

	*p = Policy{KeepLast: in.KeepLast, KeepHourly: in.KeepHourly, KeepDaily: in.KeepDaily, KeepWeekly: in.KeepWeekly, KeepMonthly: in.KeepMonthly}

	if in.KeepWithin != "" {
		p.KeepWithin, err = time.ParseDuration(in.KeepWithin)
		if err != nil {
			return fmt.Errorf("%w: keep_within: %v", ErrInvalidPolicy, err)
		}
	}

	for i, read := range in.Brackets {
		bracket := Bracket{Period: read.Period}

		if read.For != "" {
			bracket.For, err = time.ParseDuration(read.For)
			if err != nil {
				return fmt.Errorf("%w: bracket %d: %v", ErrInvalidPolicy, i, err)
			}
		}

		p.Brackets = append(p.Brackets, bracket)
	}

	return nil
}

// Parse decodes and validates a JSON policy.
func Parse(data []byte) (Policy, error) {
	var p Policy
	err := json.Unmarshal(data, &p)
	if err != nil {
		return p, fmt.Errorf("%w: %v", ErrInvalidPolicy, err)
	}

	return p, p.Validate()
}

// Commit is what evaluation needs to know about one commit of a set.
type Commit struct {
	ReceivedAt time.Time
	Partial    bool
	Pinned     bool
}

// Decision is the outcome for one commit: kept for the listed reasons, or
// retired.
type Decision struct {
	Keep    bool
	Reasons []string
}

// RetainedBy renders the reasons as the string stored on the commit row.
func (d Decision) RetainedBy() string {
	return strings.Join(d.Reasons, ",")
}

type rule struct {
	name   string
	count  int
	period func(time.Time) string
}

// Evaluate applies the policy to a set's commits and returns one decision
// per commit, in input order. The newest commit is always kept. A partial
// commit is kept only while it is the newest; it counts for no rule. A
// pinned commit is kept and counts like any other. Commits within
// KeepWithin of now are kept regardless of counts.
func Evaluate(commits []Commit, p Policy, now time.Time) []Decision {
	order := make([]int, len(commits))
	for i := range order {
		order[i] = i
	}

	sort.SliceStable(order, func(a, b int) bool {
		return commits[order[a]].ReceivedAt.After(commits[order[b]].ReceivedAt)
	})

	decisions := make([]Decision, len(commits))
	keep := func(i int, reason string) {
		decisions[i].Keep = true
		decisions[i].Reasons = append(decisions[i].Reasons, reason)
	}

	rules := []rule{
		{string(Hourly), p.KeepHourly, periods[Hourly]},
		{string(Daily), p.KeepDaily, periods[Daily]},
		{string(Weekly), p.KeepWeekly, periods[Weekly]},
		{string(Monthly), p.KeepMonthly, periods[Monthly]},
	}

	last := p.KeepLast
	if last < 1 {
		last = 1
	}

	remaining := make([]int, len(rules))
	lastPeriod := make([]string, len(rules))
	for i, r := range rules {
		remaining[i] = r.count
	}

	// one per bracket, because a commit is only ever weighed against the
	// bracket its own age falls in
	lastBracket := make([]string, len(p.Brackets))

	for n, i := range order {
		c := commits[i]

		if n == 0 {
			keep(i, "latest")
		}

		if c.Pinned {
			keep(i, "pinned")
		}

		if c.Partial {
			continue
		}

		if last > 0 {
			keep(i, "last")
			last--
		}

		if p.KeepWithin > 0 && !c.ReceivedAt.Before(now.Add(-p.KeepWithin)) {
			keep(i, "within")
		}

		for r := range rules {
			period := rules[r].period(c.ReceivedAt)
			if period == lastPeriod[r] {
				continue
			}

			lastPeriod[r] = period

			if remaining[r] > 0 {
				keep(i, rules[r].name)
				remaining[r]--
			}
		}

		if b, ok := p.bracketAt(now.Sub(c.ReceivedAt)); ok {
			period := periods[p.Brackets[b].Period](c.ReceivedAt)
			if period != lastBracket[b] {
				lastBracket[b] = period
				keep(i, string(p.Brackets[b].Period))
			}
		}
	}

	return decisions
}
