// Package retention decides which commits of a set a policy keeps.
package retention

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
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
// stretches, each kept at its own granularity; KeepLast and KeepWithin
// hold on to the newest commits regardless of which bracket they fall
// in. The rules are ORed, and KeepLast is never below 1.
type Policy struct {
	Brackets   []Bracket
	KeepLast   int
	KeepWithin time.Duration
}

// Default is the policy a store applies until it sets its own.
var Default = Policy{KeepLast: 1, Brackets: []Bracket{
	{Period: Hourly, For: 14 * 24 * time.Hour},
	{Period: Daily, For: 60 * 24 * time.Hour},
	{Period: Weekly, For: 12 * 7 * 24 * time.Hour},
	{Period: Monthly},
}}

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

// ParseFor reads how long a bracket lasts. It takes Go's duration
// syntax and, because retention is written in longer units than that
// spells well, a whole number of days or weeks: "14d", "12w", "36h30m".
func ParseFor(text string) (time.Duration, error) {
	if len(text) > 1 {
		unit := map[byte]time.Duration{'d': 24 * time.Hour, 'w': 7 * 24 * time.Hour}[text[len(text)-1]]
		if unit > 0 {
			n, err := strconv.Atoi(text[:len(text)-1])
			if err != nil {
				return 0, fmt.Errorf("%q is not a number of %s", text, map[byte]string{'d': "days", 'w': "weeks"}[text[len(text)-1]])
			}

			return time.Duration(n) * unit, nil
		}
	}

	return time.ParseDuration(text)
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
	Brackets   []bracketJSON `json:"brackets,omitempty"`
	KeepLast   int           `json:"keep_last"`
	KeepWithin string        `json:"keep_within,omitempty"`
}

// MarshalJSON writes every duration as a duration string.
func (p Policy) MarshalJSON() ([]byte, error) {
	out := policyJSON{KeepLast: p.KeepLast}
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

// UnmarshalJSON reads every duration as a duration string. A policy
// written when periods were counted rather than bracketed is refused
// rather than read as the brackets it has none of, which would retire
// everything the counts were holding.
func (p *Policy) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	for _, counted := range []string{"keep_hourly", "keep_daily", "keep_weekly", "keep_monthly"} {
		if _, ok := fields[counted]; ok {
			return fmt.Errorf("%w: %s counts periods, which policies no longer do; set brackets instead",
				ErrInvalidPolicy, counted)
		}
	}

	var in policyJSON
	err := json.Unmarshal(data, &in)
	if err != nil {
		return err
	}

	*p = Policy{KeepLast: in.KeepLast}

	if in.KeepWithin != "" {
		p.KeepWithin, err = time.ParseDuration(in.KeepWithin)
		if err != nil {
			return fmt.Errorf("%w: keep_within: %v", ErrInvalidPolicy, err)
		}
	}

	for i, read := range in.Brackets {
		bracket := Bracket{Period: read.Period}

		if read.For != "" {
			bracket.For, err = ParseFor(read.For)
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

	last := p.KeepLast
	if last < 1 {
		last = 1
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
