// Code generated by SQLBoiler 4.1.2 (https://github.com/volatiletech/sqlboiler). DO NOT EDIT.
// This file is meant to be re-generated in place and/or deleted at any time.

package models

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/friendsofgo/errors"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"github.com/volatiletech/sqlboiler/v4/queries/qmhelper"
	"github.com/volatiletech/strmangle"
)

// Commit is an object representing the database table.
type Commit struct {
	Ref       []byte    `boil:"ref" json:"ref" toml:"ref" yaml:"ref"`
	Timestamp time.Time `boil:"timestamp" json:"timestamp" toml:"timestamp" yaml:"timestamp"`
	Tree      []byte    `boil:"tree" json:"tree" toml:"tree" yaml:"tree"`
	SetID     int       `boil:"set_id" json:"set_id" toml:"set_id" yaml:"set_id"`

	R *commitR `boil:"-" json:"-" toml:"-" yaml:"-"`
	L commitL  `boil:"-" json:"-" toml:"-" yaml:"-"`
}

var CommitColumns = struct {
	Ref       string
	Timestamp string
	Tree      string
	SetID     string
}{
	Ref:       "ref",
	Timestamp: "timestamp",
	Tree:      "tree",
	SetID:     "set_id",
}

// Generated where

type whereHelper__byte struct{ field string }

func (w whereHelper__byte) EQ(x []byte) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.EQ, x) }
func (w whereHelper__byte) NEQ(x []byte) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.NEQ, x) }
func (w whereHelper__byte) LT(x []byte) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.LT, x) }
func (w whereHelper__byte) LTE(x []byte) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.LTE, x) }
func (w whereHelper__byte) GT(x []byte) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.GT, x) }
func (w whereHelper__byte) GTE(x []byte) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.GTE, x) }

type whereHelpertime_Time struct{ field string }

func (w whereHelpertime_Time) EQ(x time.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.EQ, x)
}
func (w whereHelpertime_Time) NEQ(x time.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.NEQ, x)
}
func (w whereHelpertime_Time) LT(x time.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.LT, x)
}
func (w whereHelpertime_Time) LTE(x time.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.LTE, x)
}
func (w whereHelpertime_Time) GT(x time.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.GT, x)
}
func (w whereHelpertime_Time) GTE(x time.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.GTE, x)
}

type whereHelperint struct{ field string }

func (w whereHelperint) EQ(x int) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.EQ, x) }
func (w whereHelperint) NEQ(x int) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.NEQ, x) }
func (w whereHelperint) LT(x int) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.LT, x) }
func (w whereHelperint) LTE(x int) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.LTE, x) }
func (w whereHelperint) GT(x int) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.GT, x) }
func (w whereHelperint) GTE(x int) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.GTE, x) }
func (w whereHelperint) IN(slice []int) qm.QueryMod {
	values := make([]interface{}, 0, len(slice))
	for _, value := range slice {
		values = append(values, value)
	}
	return qm.WhereIn(fmt.Sprintf("%s IN ?", w.field), values...)
}
func (w whereHelperint) NIN(slice []int) qm.QueryMod {
	values := make([]interface{}, 0, len(slice))
	for _, value := range slice {
		values = append(values, value)
	}
	return qm.WhereIn(fmt.Sprintf("%s NOT IN ?", w.field), values...)
}

var CommitWhere = struct {
	Ref       whereHelper__byte
	Timestamp whereHelpertime_Time
	Tree      whereHelper__byte
	SetID     whereHelperint
}{
	Ref:       whereHelper__byte{field: "\"commits\".\"ref\""},
	Timestamp: whereHelpertime_Time{field: "\"commits\".\"timestamp\""},
	Tree:      whereHelper__byte{field: "\"commits\".\"tree\""},
	SetID:     whereHelperint{field: "\"commits\".\"set_id\""},
}

// CommitRels is where relationship names are stored.
var CommitRels = struct {
	Set string
}{
	Set: "Set",
}

// commitR is where relationships are stored.
type commitR struct {
	Set *Set `boil:"Set" json:"Set" toml:"Set" yaml:"Set"`
}

// NewStruct creates a new relationship struct
func (*commitR) NewStruct() *commitR {
	return &commitR{}
}

// commitL is where Load methods for each relationship are stored.
type commitL struct{}

var (
	commitAllColumns            = []string{"ref", "timestamp", "tree", "set_id"}
	commitColumnsWithoutDefault = []string{"ref", "timestamp", "tree", "set_id"}
	commitColumnsWithDefault    = []string{}
	commitPrimaryKeyColumns     = []string{"ref"}
)

type (
	// CommitSlice is an alias for a slice of pointers to Commit.
	// This should generally be used opposed to []Commit.
	CommitSlice []*Commit
	// CommitHook is the signature for custom Commit hook methods
	CommitHook func(context.Context, boil.ContextExecutor, *Commit) error

	commitQuery struct {
		*queries.Query
	}
)

// Cache for insert, update and upsert
var (
	commitType                 = reflect.TypeOf(&Commit{})
	commitMapping              = queries.MakeStructMapping(commitType)
	commitPrimaryKeyMapping, _ = queries.BindMapping(commitType, commitMapping, commitPrimaryKeyColumns)
	commitInsertCacheMut       sync.RWMutex
	commitInsertCache          = make(map[string]insertCache)
	commitUpdateCacheMut       sync.RWMutex
	commitUpdateCache          = make(map[string]updateCache)
	commitUpsertCacheMut       sync.RWMutex
	commitUpsertCache          = make(map[string]insertCache)
)

var (
	// Force time package dependency for automated UpdatedAt/CreatedAt.
	_ = time.Second
	// Force qmhelper dependency for where clause generation (which doesn't
	// always happen)
	_ = qmhelper.Where
)

var commitBeforeInsertHooks []CommitHook
var commitBeforeUpdateHooks []CommitHook
var commitBeforeDeleteHooks []CommitHook
var commitBeforeUpsertHooks []CommitHook

var commitAfterInsertHooks []CommitHook
var commitAfterSelectHooks []CommitHook
var commitAfterUpdateHooks []CommitHook
var commitAfterDeleteHooks []CommitHook
var commitAfterUpsertHooks []CommitHook

// doBeforeInsertHooks executes all "before insert" hooks.
func (o *Commit) doBeforeInsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitBeforeInsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeUpdateHooks executes all "before Update" hooks.
func (o *Commit) doBeforeUpdateHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitBeforeUpdateHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeDeleteHooks executes all "before Delete" hooks.
func (o *Commit) doBeforeDeleteHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitBeforeDeleteHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeUpsertHooks executes all "before Upsert" hooks.
func (o *Commit) doBeforeUpsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitBeforeUpsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterInsertHooks executes all "after Insert" hooks.
func (o *Commit) doAfterInsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitAfterInsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterSelectHooks executes all "after Select" hooks.
func (o *Commit) doAfterSelectHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitAfterSelectHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterUpdateHooks executes all "after Update" hooks.
func (o *Commit) doAfterUpdateHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitAfterUpdateHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterDeleteHooks executes all "after Delete" hooks.
func (o *Commit) doAfterDeleteHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitAfterDeleteHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterUpsertHooks executes all "after Upsert" hooks.
func (o *Commit) doAfterUpsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range commitAfterUpsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// AddCommitHook registers your hook function for all future operations.
func AddCommitHook(hookPoint boil.HookPoint, commitHook CommitHook) {
	switch hookPoint {
	case boil.BeforeInsertHook:
		commitBeforeInsertHooks = append(commitBeforeInsertHooks, commitHook)
	case boil.BeforeUpdateHook:
		commitBeforeUpdateHooks = append(commitBeforeUpdateHooks, commitHook)
	case boil.BeforeDeleteHook:
		commitBeforeDeleteHooks = append(commitBeforeDeleteHooks, commitHook)
	case boil.BeforeUpsertHook:
		commitBeforeUpsertHooks = append(commitBeforeUpsertHooks, commitHook)
	case boil.AfterInsertHook:
		commitAfterInsertHooks = append(commitAfterInsertHooks, commitHook)
	case boil.AfterSelectHook:
		commitAfterSelectHooks = append(commitAfterSelectHooks, commitHook)
	case boil.AfterUpdateHook:
		commitAfterUpdateHooks = append(commitAfterUpdateHooks, commitHook)
	case boil.AfterDeleteHook:
		commitAfterDeleteHooks = append(commitAfterDeleteHooks, commitHook)
	case boil.AfterUpsertHook:
		commitAfterUpsertHooks = append(commitAfterUpsertHooks, commitHook)
	}
}

// One returns a single commit record from the query.
func (q commitQuery) One(ctx context.Context, exec boil.ContextExecutor) (*Commit, error) {
	o := &Commit{}

	queries.SetLimit(q.Query, 1)

	err := q.Bind(ctx, exec, o)
	if err != nil {
		if errors.Cause(err) == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, errors.Wrap(err, "models: failed to execute a one query for commits")
	}

	if err := o.doAfterSelectHooks(ctx, exec); err != nil {
		return o, err
	}

	return o, nil
}

// All returns all Commit records from the query.
func (q commitQuery) All(ctx context.Context, exec boil.ContextExecutor) (CommitSlice, error) {
	var o []*Commit

	err := q.Bind(ctx, exec, &o)
	if err != nil {
		return nil, errors.Wrap(err, "models: failed to assign all query results to Commit slice")
	}

	if len(commitAfterSelectHooks) != 0 {
		for _, obj := range o {
			if err := obj.doAfterSelectHooks(ctx, exec); err != nil {
				return o, err
			}
		}
	}

	return o, nil
}

// Count returns the count of all Commit records in the query.
func (q commitQuery) Count(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	var count int64

	queries.SetSelect(q.Query, nil)
	queries.SetCount(q.Query)

	err := q.Query.QueryRowContext(ctx, exec).Scan(&count)
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to count commits rows")
	}

	return count, nil
}

// Exists checks if the row exists in the table.
func (q commitQuery) Exists(ctx context.Context, exec boil.ContextExecutor) (bool, error) {
	var count int64

	queries.SetSelect(q.Query, nil)
	queries.SetCount(q.Query)
	queries.SetLimit(q.Query, 1)

	err := q.Query.QueryRowContext(ctx, exec).Scan(&count)
	if err != nil {
		return false, errors.Wrap(err, "models: failed to check if commits exists")
	}

	return count > 0, nil
}

// Set pointed to by the foreign key.
func (o *Commit) Set(mods ...qm.QueryMod) setQuery {
	queryMods := []qm.QueryMod{
		qm.Where("\"id\" = ?", o.SetID),
	}

	queryMods = append(queryMods, mods...)

	query := Sets(queryMods...)
	queries.SetFrom(query.Query, "\"sets\"")

	return query
}

// LoadSet allows an eager lookup of values, cached into the
// loaded structs of the objects. This is for an N-1 relationship.
func (commitL) LoadSet(ctx context.Context, e boil.ContextExecutor, singular bool, maybeCommit interface{}, mods queries.Applicator) error {
	var slice []*Commit
	var object *Commit

	if singular {
		object = maybeCommit.(*Commit)
	} else {
		slice = *maybeCommit.(*[]*Commit)
	}

	args := make([]interface{}, 0, 1)
	if singular {
		if object.R == nil {
			object.R = &commitR{}
		}
		args = append(args, object.SetID)

	} else {
	Outer:
		for _, obj := range slice {
			if obj.R == nil {
				obj.R = &commitR{}
			}

			for _, a := range args {
				if a == obj.SetID {
					continue Outer
				}
			}

			args = append(args, obj.SetID)

		}
	}

	if len(args) == 0 {
		return nil
	}

	query := NewQuery(
		qm.From(`sets`),
		qm.WhereIn(`sets.id in ?`, args...),
	)
	if mods != nil {
		mods.Apply(query)
	}

	results, err := query.QueryContext(ctx, e)
	if err != nil {
		return errors.Wrap(err, "failed to eager load Set")
	}

	var resultSlice []*Set
	if err = queries.Bind(results, &resultSlice); err != nil {
		return errors.Wrap(err, "failed to bind eager loaded slice Set")
	}

	if err = results.Close(); err != nil {
		return errors.Wrap(err, "failed to close results of eager load for sets")
	}
	if err = results.Err(); err != nil {
		return errors.Wrap(err, "error occurred during iteration of eager loaded relations for sets")
	}

	if len(commitAfterSelectHooks) != 0 {
		for _, obj := range resultSlice {
			if err := obj.doAfterSelectHooks(ctx, e); err != nil {
				return err
			}
		}
	}

	if len(resultSlice) == 0 {
		return nil
	}

	if singular {
		foreign := resultSlice[0]
		object.R.Set = foreign
		if foreign.R == nil {
			foreign.R = &setR{}
		}
		foreign.R.Commits = append(foreign.R.Commits, object)
		return nil
	}

	for _, local := range slice {
		for _, foreign := range resultSlice {
			if local.SetID == foreign.ID {
				local.R.Set = foreign
				if foreign.R == nil {
					foreign.R = &setR{}
				}
				foreign.R.Commits = append(foreign.R.Commits, local)
				break
			}
		}
	}

	return nil
}

// SetSet of the commit to the related item.
// Sets o.R.Set to related.
// Adds o to related.R.Commits.
func (o *Commit) SetSet(ctx context.Context, exec boil.ContextExecutor, insert bool, related *Set) error {
	var err error
	if insert {
		if err = related.Insert(ctx, exec, boil.Infer()); err != nil {
			return errors.Wrap(err, "failed to insert into foreign table")
		}
	}

	updateQuery := fmt.Sprintf(
		"UPDATE \"commits\" SET %s WHERE %s",
		strmangle.SetParamNames("\"", "\"", 1, []string{"set_id"}),
		strmangle.WhereClause("\"", "\"", 2, commitPrimaryKeyColumns),
	)
	values := []interface{}{related.ID, o.Ref}

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, updateQuery)
		fmt.Fprintln(writer, values)
	}
	if _, err = exec.ExecContext(ctx, updateQuery, values...); err != nil {
		return errors.Wrap(err, "failed to update local table")
	}

	o.SetID = related.ID
	if o.R == nil {
		o.R = &commitR{
			Set: related,
		}
	} else {
		o.R.Set = related
	}

	if related.R == nil {
		related.R = &setR{
			Commits: CommitSlice{o},
		}
	} else {
		related.R.Commits = append(related.R.Commits, o)
	}

	return nil
}

// Commits retrieves all the records using an executor.
func Commits(mods ...qm.QueryMod) commitQuery {
	mods = append(mods, qm.From("\"commits\""))
	return commitQuery{NewQuery(mods...)}
}

// FindCommit retrieves a single record by ID with an executor.
// If selectCols is empty Find will return all columns.
func FindCommit(ctx context.Context, exec boil.ContextExecutor, ref []byte, selectCols ...string) (*Commit, error) {
	commitObj := &Commit{}

	sel := "*"
	if len(selectCols) > 0 {
		sel = strings.Join(strmangle.IdentQuoteSlice(dialect.LQ, dialect.RQ, selectCols), ",")
	}
	query := fmt.Sprintf(
		"select %s from \"commits\" where \"ref\"=$1", sel,
	)

	q := queries.Raw(query, ref)

	err := q.Bind(ctx, exec, commitObj)
	if err != nil {
		if errors.Cause(err) == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, errors.Wrap(err, "models: unable to select from commits")
	}

	return commitObj, nil
}

// Insert a single record using an executor.
// See boil.Columns.InsertColumnSet documentation to understand column list inference for inserts.
func (o *Commit) Insert(ctx context.Context, exec boil.ContextExecutor, columns boil.Columns) error {
	if o == nil {
		return errors.New("models: no commits provided for insertion")
	}

	var err error

	if err := o.doBeforeInsertHooks(ctx, exec); err != nil {
		return err
	}

	nzDefaults := queries.NonZeroDefaultSet(commitColumnsWithDefault, o)

	key := makeCacheKey(columns, nzDefaults)
	commitInsertCacheMut.RLock()
	cache, cached := commitInsertCache[key]
	commitInsertCacheMut.RUnlock()

	if !cached {
		wl, returnColumns := columns.InsertColumnSet(
			commitAllColumns,
			commitColumnsWithDefault,
			commitColumnsWithoutDefault,
			nzDefaults,
		)

		cache.valueMapping, err = queries.BindMapping(commitType, commitMapping, wl)
		if err != nil {
			return err
		}
		cache.retMapping, err = queries.BindMapping(commitType, commitMapping, returnColumns)
		if err != nil {
			return err
		}
		if len(wl) != 0 {
			cache.query = fmt.Sprintf("INSERT INTO \"commits\" (\"%s\") %%sVALUES (%s)%%s", strings.Join(wl, "\",\""), strmangle.Placeholders(dialect.UseIndexPlaceholders, len(wl), 1, 1))
		} else {
			cache.query = "INSERT INTO \"commits\" %sDEFAULT VALUES%s"
		}

		var queryOutput, queryReturning string

		if len(cache.retMapping) != 0 {
			queryReturning = fmt.Sprintf(" RETURNING \"%s\"", strings.Join(returnColumns, "\",\""))
		}

		cache.query = fmt.Sprintf(cache.query, queryOutput, queryReturning)
	}

	value := reflect.Indirect(reflect.ValueOf(o))
	vals := queries.ValuesFromMapping(value, cache.valueMapping)

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, cache.query)
		fmt.Fprintln(writer, vals)
	}

	if len(cache.retMapping) != 0 {
		err = exec.QueryRowContext(ctx, cache.query, vals...).Scan(queries.PtrsFromMapping(value, cache.retMapping)...)
	} else {
		_, err = exec.ExecContext(ctx, cache.query, vals...)
	}

	if err != nil {
		return errors.Wrap(err, "models: unable to insert into commits")
	}

	if !cached {
		commitInsertCacheMut.Lock()
		commitInsertCache[key] = cache
		commitInsertCacheMut.Unlock()
	}

	return o.doAfterInsertHooks(ctx, exec)
}

// Update uses an executor to update the Commit.
// See boil.Columns.UpdateColumnSet documentation to understand column list inference for updates.
// Update does not automatically update the record in case of default values. Use .Reload() to refresh the records.
func (o *Commit) Update(ctx context.Context, exec boil.ContextExecutor, columns boil.Columns) (int64, error) {
	var err error
	if err = o.doBeforeUpdateHooks(ctx, exec); err != nil {
		return 0, err
	}
	key := makeCacheKey(columns, nil)
	commitUpdateCacheMut.RLock()
	cache, cached := commitUpdateCache[key]
	commitUpdateCacheMut.RUnlock()

	if !cached {
		wl := columns.UpdateColumnSet(
			commitAllColumns,
			commitPrimaryKeyColumns,
		)

		if !columns.IsWhitelist() {
			wl = strmangle.SetComplement(wl, []string{"created_at"})
		}
		if len(wl) == 0 {
			return 0, errors.New("models: unable to update commits, could not build whitelist")
		}

		cache.query = fmt.Sprintf("UPDATE \"commits\" SET %s WHERE %s",
			strmangle.SetParamNames("\"", "\"", 1, wl),
			strmangle.WhereClause("\"", "\"", len(wl)+1, commitPrimaryKeyColumns),
		)
		cache.valueMapping, err = queries.BindMapping(commitType, commitMapping, append(wl, commitPrimaryKeyColumns...))
		if err != nil {
			return 0, err
		}
	}

	values := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(o)), cache.valueMapping)

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, cache.query)
		fmt.Fprintln(writer, values)
	}
	var result sql.Result
	result, err = exec.ExecContext(ctx, cache.query, values...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to update commits row")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by update for commits")
	}

	if !cached {
		commitUpdateCacheMut.Lock()
		commitUpdateCache[key] = cache
		commitUpdateCacheMut.Unlock()
	}

	return rowsAff, o.doAfterUpdateHooks(ctx, exec)
}

// UpdateAll updates all rows with the specified column values.
func (q commitQuery) UpdateAll(ctx context.Context, exec boil.ContextExecutor, cols M) (int64, error) {
	queries.SetUpdate(q.Query, cols)

	result, err := q.Query.ExecContext(ctx, exec)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to update all for commits")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to retrieve rows affected for commits")
	}

	return rowsAff, nil
}

// UpdateAll updates all rows with the specified column values, using an executor.
func (o CommitSlice) UpdateAll(ctx context.Context, exec boil.ContextExecutor, cols M) (int64, error) {
	ln := int64(len(o))
	if ln == 0 {
		return 0, nil
	}

	if len(cols) == 0 {
		return 0, errors.New("models: update all requires at least one column argument")
	}

	colNames := make([]string, len(cols))
	args := make([]interface{}, len(cols))

	i := 0
	for name, value := range cols {
		colNames[i] = name
		args[i] = value
		i++
	}

	// Append all of the primary key values for each column
	for _, obj := range o {
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), commitPrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := fmt.Sprintf("UPDATE \"commits\" SET %s WHERE %s",
		strmangle.SetParamNames("\"", "\"", 1, colNames),
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), len(colNames)+1, commitPrimaryKeyColumns, len(o)))

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args...)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to update all in commit slice")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to retrieve rows affected all in update all commit")
	}
	return rowsAff, nil
}

// Upsert attempts an insert using an executor, and does an update or ignore on conflict.
// See boil.Columns documentation for how to properly use updateColumns and insertColumns.
func (o *Commit) Upsert(ctx context.Context, exec boil.ContextExecutor, updateOnConflict bool, conflictColumns []string, updateColumns, insertColumns boil.Columns) error {
	if o == nil {
		return errors.New("models: no commits provided for upsert")
	}

	if err := o.doBeforeUpsertHooks(ctx, exec); err != nil {
		return err
	}

	nzDefaults := queries.NonZeroDefaultSet(commitColumnsWithDefault, o)

	// Build cache key in-line uglily - mysql vs psql problems
	buf := strmangle.GetBuffer()
	if updateOnConflict {
		buf.WriteByte('t')
	} else {
		buf.WriteByte('f')
	}
	buf.WriteByte('.')
	for _, c := range conflictColumns {
		buf.WriteString(c)
	}
	buf.WriteByte('.')
	buf.WriteString(strconv.Itoa(updateColumns.Kind))
	for _, c := range updateColumns.Cols {
		buf.WriteString(c)
	}
	buf.WriteByte('.')
	buf.WriteString(strconv.Itoa(insertColumns.Kind))
	for _, c := range insertColumns.Cols {
		buf.WriteString(c)
	}
	buf.WriteByte('.')
	for _, c := range nzDefaults {
		buf.WriteString(c)
	}
	key := buf.String()
	strmangle.PutBuffer(buf)

	commitUpsertCacheMut.RLock()
	cache, cached := commitUpsertCache[key]
	commitUpsertCacheMut.RUnlock()

	var err error

	if !cached {
		insert, ret := insertColumns.InsertColumnSet(
			commitAllColumns,
			commitColumnsWithDefault,
			commitColumnsWithoutDefault,
			nzDefaults,
		)
		update := updateColumns.UpdateColumnSet(
			commitAllColumns,
			commitPrimaryKeyColumns,
		)

		if updateOnConflict && len(update) == 0 {
			return errors.New("models: unable to upsert commits, could not build update column list")
		}

		conflict := conflictColumns
		if len(conflict) == 0 {
			conflict = make([]string, len(commitPrimaryKeyColumns))
			copy(conflict, commitPrimaryKeyColumns)
		}
		cache.query = buildUpsertQueryPostgres(dialect, "\"commits\"", updateOnConflict, ret, update, conflict, insert)

		cache.valueMapping, err = queries.BindMapping(commitType, commitMapping, insert)
		if err != nil {
			return err
		}
		if len(ret) != 0 {
			cache.retMapping, err = queries.BindMapping(commitType, commitMapping, ret)
			if err != nil {
				return err
			}
		}
	}

	value := reflect.Indirect(reflect.ValueOf(o))
	vals := queries.ValuesFromMapping(value, cache.valueMapping)
	var returns []interface{}
	if len(cache.retMapping) != 0 {
		returns = queries.PtrsFromMapping(value, cache.retMapping)
	}

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, cache.query)
		fmt.Fprintln(writer, vals)
	}
	if len(cache.retMapping) != 0 {
		err = exec.QueryRowContext(ctx, cache.query, vals...).Scan(returns...)
		if err == sql.ErrNoRows {
			err = nil // Postgres doesn't return anything when there's no update
		}
	} else {
		_, err = exec.ExecContext(ctx, cache.query, vals...)
	}
	if err != nil {
		return errors.Wrap(err, "models: unable to upsert commits")
	}

	if !cached {
		commitUpsertCacheMut.Lock()
		commitUpsertCache[key] = cache
		commitUpsertCacheMut.Unlock()
	}

	return o.doAfterUpsertHooks(ctx, exec)
}

// Delete deletes a single Commit record with an executor.
// Delete will match against the primary key column to find the record to delete.
func (o *Commit) Delete(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if o == nil {
		return 0, errors.New("models: no Commit provided for delete")
	}

	if err := o.doBeforeDeleteHooks(ctx, exec); err != nil {
		return 0, err
	}

	args := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(o)), commitPrimaryKeyMapping)
	sql := "DELETE FROM \"commits\" WHERE \"ref\"=$1"

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args...)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete from commits")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by delete for commits")
	}

	if err := o.doAfterDeleteHooks(ctx, exec); err != nil {
		return 0, err
	}

	return rowsAff, nil
}

// DeleteAll deletes all matching rows.
func (q commitQuery) DeleteAll(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if q.Query == nil {
		return 0, errors.New("models: no commitQuery provided for delete all")
	}

	queries.SetDelete(q.Query)

	result, err := q.Query.ExecContext(ctx, exec)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete all from commits")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by deleteall for commits")
	}

	return rowsAff, nil
}

// DeleteAll deletes all rows in the slice, using an executor.
func (o CommitSlice) DeleteAll(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if len(o) == 0 {
		return 0, nil
	}

	if len(commitBeforeDeleteHooks) != 0 {
		for _, obj := range o {
			if err := obj.doBeforeDeleteHooks(ctx, exec); err != nil {
				return 0, err
			}
		}
	}

	var args []interface{}
	for _, obj := range o {
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), commitPrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := "DELETE FROM \"commits\" WHERE " +
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), 1, commitPrimaryKeyColumns, len(o))

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete all from commit slice")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by deleteall for commits")
	}

	if len(commitAfterDeleteHooks) != 0 {
		for _, obj := range o {
			if err := obj.doAfterDeleteHooks(ctx, exec); err != nil {
				return 0, err
			}
		}
	}

	return rowsAff, nil
}

// Reload refetches the object from the database
// using the primary keys with an executor.
func (o *Commit) Reload(ctx context.Context, exec boil.ContextExecutor) error {
	ret, err := FindCommit(ctx, exec, o.Ref)
	if err != nil {
		return err
	}

	*o = *ret
	return nil
}

// ReloadAll refetches every row with matching primary key column values
// and overwrites the original object slice with the newly updated slice.
func (o *CommitSlice) ReloadAll(ctx context.Context, exec boil.ContextExecutor) error {
	if o == nil || len(*o) == 0 {
		return nil
	}

	slice := CommitSlice{}
	var args []interface{}
	for _, obj := range *o {
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), commitPrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := "SELECT \"commits\".* FROM \"commits\" WHERE " +
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), 1, commitPrimaryKeyColumns, len(*o))

	q := queries.Raw(sql, args...)

	err := q.Bind(ctx, exec, &slice)
	if err != nil {
		return errors.Wrap(err, "models: unable to reload all in CommitSlice")
	}

	*o = slice

	return nil
}

// CommitExists checks if the Commit row exists.
func CommitExists(ctx context.Context, exec boil.ContextExecutor, ref []byte) (bool, error) {
	var exists bool
	sql := "select exists(select 1 from \"commits\" where \"ref\"=$1 limit 1)"

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, ref)
	}
	row := exec.QueryRowContext(ctx, sql, ref)

	err := row.Scan(&exists)
	if err != nil {
		return false, errors.Wrap(err, "models: unable to check if commits exists")
	}

	return exists, nil
}
