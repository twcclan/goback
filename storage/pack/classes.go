package pack

import (
	"slices"
	"time"
)

// span is the length of the calendar period an age class covers.
type span int

const (
	spanDay span = iota
	spanWeek
	spanMonth
	spanYear
)

// ageClass is the calendar period an object's stored time falls in.
type ageClass struct {
	span  span
	start int64
}

// classOf is the period of t seen from now: its day within the last week,
// its week within the last five, its month within the last year and its
// year before that.
func classOf(t, now time.Time) ageClass {
	t, now = t.UTC(), now.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	if !day.Before(today.AddDate(0, 0, -6)) {
		return ageClass{spanDay, day.Unix()}
	}

	if week := monday(day); !week.Before(monday(today).AddDate(0, 0, -7*4)) {
		return ageClass{spanWeek, week.Unix()}
	}

	month := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	if !month.Before(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)) {
		return ageClass{spanMonth, month.Unix()}
	}

	return ageClass{spanYear, time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC).Unix()}
}

func monday(day time.Time) time.Time {
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// newer orders classes newest first.
func (c ageClass) newer(o ageClass) bool {
	if c.start != o.start {
		return c.start > o.start
	}

	return c.span < o.span
}

// outputClass is what a rewrite writes into one output: the objects of
// one owner stored in one period. A zero Set with a Group is what several
// sets of that group share; a zero Attribution is what the mark
// attributed to nobody.
type outputClass struct {
	owner Attribution
	age   ageClass
}

// classes numbers the output classes of a sweep and what of it survives.
type classes struct {
	ids   map[outputClass]int32
	all   []outputClass
	bytes []uint64
}

func newClasses() *classes {
	return &classes{ids: make(map[outputClass]int32)}
}

func (c *classes) add(class outputClass, length uint64) int32 {
	id, ok := c.ids[class]
	if !ok {
		id = int32(len(c.all))
		c.ids[class] = id
		c.all = append(c.all, class)
		c.bytes = append(c.bytes, 0)
	}

	c.bytes[id] += length

	return id
}

// fold maps every class to the one its objects are written into. A class
// holding less than small joins its owner's next older one; the oldest,
// if that leaves it small, joins the newer one it would have followed.
func (c *classes) fold(small uint64) []int32 {
	into := make([]int32, len(c.all))

	byOwner := make(map[Attribution][]int32)
	for id, class := range c.all {
		byOwner[class.owner] = append(byOwner[class.owner], int32(id))
	}

	for _, ids := range byOwner {
		slices.SortFunc(ids, func(a, b int32) int {
			if c.all[a].age.newer(c.all[b].age) {
				return -1
			}

			return 1
		})

		var carried []int32
		var held uint64
		last := int32(-1)

		for _, id := range ids {
			carried = append(carried, id)
			held += c.bytes[id]

			if held < small {
				continue
			}

			for _, from := range carried {
				into[from] = id
			}

			carried, held, last = nil, 0, id
		}

		target := last
		if target < 0 {
			target = ids[len(ids)-1]
		}

		for _, from := range carried {
			into[from] = target
		}
	}

	return into
}

// ownerOf is whom a rewrite files an object under: the set of the first
// group that reached it when that set alone did, the group when several
// of its sets did, nobody when none did.
func ownerOf(owners []Attribution) Attribution {
	var owner Attribution
	found := false

	for _, o := range owners {
		switch {
		case o.Set == 0:
		case !found:
			owner, found = o, true
		case o.Group != owner.Group:
			return owner
		case o.Set != owner.Set:
			owner.Set = 0
		}
	}

	return owner
}

// classify gives every record of the archives a sweep rewrites the output
// class it is written with.
func (r *gcRun) classify(live *liveRuns) error {
	var selected []*gcArchive
	for _, ga := range r.order {
		if r.selected(ga) {
			ga.owners = make([]Attribution, ga.count)
			selected = append(selected, ga)
		}
	}

	if len(selected) == 0 {
		return nil
	}

	err := r.scanOf(selected, live, func(ga *gcArchive, pos int, _ *IndexRecord, owners []Attribution, _ uint64) {
		ga.owners[pos] = ownerOf(owners)
	}, nil)
	if err != nil {
		return err
	}

	all := newClasses()

	for _, ga := range selected {
		ga.class = make([]int32, ga.count)

		err := scanArchive(ga.a, func(pos int, rec *IndexRecord) error {
			length := uint64(rec.Length)
			if r.droppable(ga, ga.next, pos, rec) {
				length = 0
			}

			ga.class[pos] = all.add(outputClass{ga.owners[pos], classOf(ga.a.version(*rec).Time, r.opts.Now)}, length)

			return nil
		}, nil, nil)
		if err != nil {
			return err
		}

		ga.owners = nil
	}

	into := all.fold(r.ps.compaction.small())
	for _, ga := range selected {
		for pos, id := range ga.class {
			ga.class[pos] = into[id]
		}
	}

	return nil
}

// class is the output class of the record at pos in candidate, or -1.
func (r *gcRun) class(candidate *archive, pos int) int32 {
	ga := r.archives[candidate.name]
	if ga == nil {
		return -1
	}

	return classAt(ga.class, pos)
}

func classAt(class []int32, pos int) int32 {
	if pos < 0 || pos >= len(class) {
		return -1
	}

	return class[pos]
}
