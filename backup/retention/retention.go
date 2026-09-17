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

// Policy is a restic-style count policy: the rules are ORed, and a period
// rule counts only periods that have commits. KeepLast is never below 1.
type Policy struct {
	KeepLast    int
	KeepHourly  int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
	KeepWithin  time.Duration
}

// Limits caps every policy of the store; a zero field is unlimited.
type Limits struct {
	MaxLast    int
	MaxHourly  int
	MaxDaily   int
	MaxWeekly  int
	MaxMonthly int
	MaxWithin  time.Duration
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

	return nil
}

// Clamp returns the policy with every counter capped by the limits.
func (p Policy) Clamp(l Limits) Policy {
	cap := func(v, max int) int {
		if max > 0 && v > max {
			return max
		}
		return v
	}

	p.KeepLast = cap(p.KeepLast, l.MaxLast)
	p.KeepHourly = cap(p.KeepHourly, l.MaxHourly)
	p.KeepDaily = cap(p.KeepDaily, l.MaxDaily)
	p.KeepWeekly = cap(p.KeepWeekly, l.MaxWeekly)
	p.KeepMonthly = cap(p.KeepMonthly, l.MaxMonthly)

	if l.MaxWithin > 0 && p.KeepWithin > l.MaxWithin {
		p.KeepWithin = l.MaxWithin
	}

	if p.KeepLast < 1 {
		p.KeepLast = 1
	}

	return p
}

type policyJSON struct {
	KeepLast    int    `json:"keep_last"`
	KeepHourly  int    `json:"keep_hourly,omitempty"`
	KeepDaily   int    `json:"keep_daily,omitempty"`
	KeepWeekly  int    `json:"keep_weekly,omitempty"`
	KeepMonthly int    `json:"keep_monthly,omitempty"`
	KeepWithin  string `json:"keep_within,omitempty"`
}

// MarshalJSON writes keep_within as a duration string.
func (p Policy) MarshalJSON() ([]byte, error) {
	out := policyJSON{KeepLast: p.KeepLast, KeepHourly: p.KeepHourly, KeepDaily: p.KeepDaily, KeepWeekly: p.KeepWeekly, KeepMonthly: p.KeepMonthly}
	if p.KeepWithin > 0 {
		out.KeepWithin = p.KeepWithin.String()
	}

	return json.Marshal(out)
}

// UnmarshalJSON reads keep_within as a duration string.
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
		{"hourly", p.KeepHourly, func(t time.Time) string { return t.UTC().Format("2006-01-02T15") }},
		{"daily", p.KeepDaily, func(t time.Time) string { return t.UTC().Format("2006-01-02") }},
		{"weekly", p.KeepWeekly, func(t time.Time) string {
			y, w := t.UTC().ISOWeek()
			return fmt.Sprintf("%04d-W%02d", y, w)
		}},
		{"monthly", p.KeepMonthly, func(t time.Time) string { return t.UTC().Format("2006-01") }},
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
	}

	return decisions
}
