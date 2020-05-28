// Code generated by SQLBoiler 4.1.2 (https://github.com/volatiletech/sqlboiler). DO NOT EDIT.
// This file is meant to be re-generated in place and/or deleted at any time.

package models

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/volatiletech/randomize"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries"
	"github.com/volatiletech/strmangle"
)

var (
	// Relationships sometimes use the reflection helper queries.Equal/queries.Assign
	// so force a package dependency in case they don't.
	_ = queries.Equal
)

func testCommits(t *testing.T) {
	t.Parallel()

	query := Commits()

	if query.Query == nil {
		t.Error("expected a query, got nothing")
	}
}

func testCommitsDelete(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	if rowsAff, err := o.Delete(ctx, tx); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("should only have deleted one row, but affected:", rowsAff)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 0 {
		t.Error("want zero records, got:", count)
	}
}

func testCommitsQueryDeleteAll(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	if rowsAff, err := Commits().DeleteAll(ctx, tx); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("should only have deleted one row, but affected:", rowsAff)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 0 {
		t.Error("want zero records, got:", count)
	}
}

func testCommitsSliceDeleteAll(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	slice := CommitSlice{o}

	if rowsAff, err := slice.DeleteAll(ctx, tx); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("should only have deleted one row, but affected:", rowsAff)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 0 {
		t.Error("want zero records, got:", count)
	}
}

func testCommitsExists(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	e, err := CommitExists(ctx, tx, o.Ref)
	if err != nil {
		t.Errorf("Unable to check if Commit exists: %s", err)
	}
	if !e {
		t.Errorf("Expected CommitExists to return true, but got false.")
	}
}

func testCommitsFind(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	commitFound, err := FindCommit(ctx, tx, o.Ref)
	if err != nil {
		t.Error(err)
	}

	if commitFound == nil {
		t.Error("want a record, got nil")
	}
}

func testCommitsBind(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	if err = Commits().Bind(ctx, tx, o); err != nil {
		t.Error(err)
	}
}

func testCommitsOne(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	if x, err := Commits().One(ctx, tx); err != nil {
		t.Error(err)
	} else if x == nil {
		t.Error("expected to get a non nil record")
	}
}

func testCommitsAll(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	commitOne := &Commit{}
	commitTwo := &Commit{}
	if err = randomize.Struct(seed, commitOne, commitDBTypes, false, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}
	if err = randomize.Struct(seed, commitTwo, commitDBTypes, false, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = commitOne.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}
	if err = commitTwo.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	slice, err := Commits().All(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if len(slice) != 2 {
		t.Error("want 2 records, got:", len(slice))
	}
}

func testCommitsCount(t *testing.T) {
	t.Parallel()

	var err error
	seed := randomize.NewSeed()
	commitOne := &Commit{}
	commitTwo := &Commit{}
	if err = randomize.Struct(seed, commitOne, commitDBTypes, false, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}
	if err = randomize.Struct(seed, commitTwo, commitDBTypes, false, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = commitOne.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}
	if err = commitTwo.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 2 {
		t.Error("want 2 records, got:", count)
	}
}

func commitBeforeInsertHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func commitAfterInsertHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func commitAfterSelectHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func commitBeforeUpdateHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func commitAfterUpdateHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func commitBeforeDeleteHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func commitAfterDeleteHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func commitBeforeUpsertHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func commitAfterUpsertHook(ctx context.Context, e boil.ContextExecutor, o *Commit) error {
	*o = Commit{}
	return nil
}

func testCommitsHooks(t *testing.T) {
	t.Parallel()

	var err error

	ctx := context.Background()
	empty := &Commit{}
	o := &Commit{}

	seed := randomize.NewSeed()
	if err = randomize.Struct(seed, o, commitDBTypes, false); err != nil {
		t.Errorf("Unable to randomize Commit object: %s", err)
	}

	AddCommitHook(boil.BeforeInsertHook, commitBeforeInsertHook)
	if err = o.doBeforeInsertHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doBeforeInsertHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected BeforeInsertHook function to empty object, but got: %#v", o)
	}
	commitBeforeInsertHooks = []CommitHook{}

	AddCommitHook(boil.AfterInsertHook, commitAfterInsertHook)
	if err = o.doAfterInsertHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterInsertHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterInsertHook function to empty object, but got: %#v", o)
	}
	commitAfterInsertHooks = []CommitHook{}

	AddCommitHook(boil.AfterSelectHook, commitAfterSelectHook)
	if err = o.doAfterSelectHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterSelectHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterSelectHook function to empty object, but got: %#v", o)
	}
	commitAfterSelectHooks = []CommitHook{}

	AddCommitHook(boil.BeforeUpdateHook, commitBeforeUpdateHook)
	if err = o.doBeforeUpdateHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doBeforeUpdateHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected BeforeUpdateHook function to empty object, but got: %#v", o)
	}
	commitBeforeUpdateHooks = []CommitHook{}

	AddCommitHook(boil.AfterUpdateHook, commitAfterUpdateHook)
	if err = o.doAfterUpdateHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterUpdateHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterUpdateHook function to empty object, but got: %#v", o)
	}
	commitAfterUpdateHooks = []CommitHook{}

	AddCommitHook(boil.BeforeDeleteHook, commitBeforeDeleteHook)
	if err = o.doBeforeDeleteHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doBeforeDeleteHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected BeforeDeleteHook function to empty object, but got: %#v", o)
	}
	commitBeforeDeleteHooks = []CommitHook{}

	AddCommitHook(boil.AfterDeleteHook, commitAfterDeleteHook)
	if err = o.doAfterDeleteHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterDeleteHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterDeleteHook function to empty object, but got: %#v", o)
	}
	commitAfterDeleteHooks = []CommitHook{}

	AddCommitHook(boil.BeforeUpsertHook, commitBeforeUpsertHook)
	if err = o.doBeforeUpsertHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doBeforeUpsertHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected BeforeUpsertHook function to empty object, but got: %#v", o)
	}
	commitBeforeUpsertHooks = []CommitHook{}

	AddCommitHook(boil.AfterUpsertHook, commitAfterUpsertHook)
	if err = o.doAfterUpsertHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterUpsertHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterUpsertHook function to empty object, but got: %#v", o)
	}
	commitAfterUpsertHooks = []CommitHook{}
}

func testCommitsInsert(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 1 {
		t.Error("want one record, got:", count)
	}
}

func testCommitsInsertWhitelist(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Whitelist(commitColumnsWithoutDefault...)); err != nil {
		t.Error(err)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 1 {
		t.Error("want one record, got:", count)
	}
}

func testCommitToOneSetUsingSet(t *testing.T) {
	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()

	var local Commit
	var foreign Set

	seed := randomize.NewSeed()
	if err := randomize.Struct(seed, &local, commitDBTypes, false, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}
	if err := randomize.Struct(seed, &foreign, setDBTypes, false, setColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Set struct: %s", err)
	}

	if err := foreign.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Fatal(err)
	}

	local.SetID = foreign.ID
	if err := local.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Fatal(err)
	}

	check, err := local.Set().One(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}

	if check.ID != foreign.ID {
		t.Errorf("want: %v, got %v", foreign.ID, check.ID)
	}

	slice := CommitSlice{&local}
	if err = local.L.LoadSet(ctx, tx, false, (*[]*Commit)(&slice), nil); err != nil {
		t.Fatal(err)
	}
	if local.R.Set == nil {
		t.Error("struct should have been eager loaded")
	}

	local.R.Set = nil
	if err = local.L.LoadSet(ctx, tx, true, &local, nil); err != nil {
		t.Fatal(err)
	}
	if local.R.Set == nil {
		t.Error("struct should have been eager loaded")
	}
}

func testCommitToOneSetOpSetUsingSet(t *testing.T) {
	var err error

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()

	var a Commit
	var b, c Set

	seed := randomize.NewSeed()
	if err = randomize.Struct(seed, &a, commitDBTypes, false, strmangle.SetComplement(commitPrimaryKeyColumns, commitColumnsWithoutDefault)...); err != nil {
		t.Fatal(err)
	}
	if err = randomize.Struct(seed, &b, setDBTypes, false, strmangle.SetComplement(setPrimaryKeyColumns, setColumnsWithoutDefault)...); err != nil {
		t.Fatal(err)
	}
	if err = randomize.Struct(seed, &c, setDBTypes, false, strmangle.SetComplement(setPrimaryKeyColumns, setColumnsWithoutDefault)...); err != nil {
		t.Fatal(err)
	}

	if err := a.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Fatal(err)
	}
	if err = b.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Fatal(err)
	}

	for i, x := range []*Set{&b, &c} {
		err = a.SetSet(ctx, tx, i != 0, x)
		if err != nil {
			t.Fatal(err)
		}

		if a.R.Set != x {
			t.Error("relationship struct not set to correct value")
		}

		if x.R.Commits[0] != &a {
			t.Error("failed to append to foreign relationship struct")
		}
		if a.SetID != x.ID {
			t.Error("foreign key was wrong value", a.SetID)
		}

		zero := reflect.Zero(reflect.TypeOf(a.SetID))
		reflect.Indirect(reflect.ValueOf(&a.SetID)).Set(zero)

		if err = a.Reload(ctx, tx); err != nil {
			t.Fatal("failed to reload", err)
		}

		if a.SetID != x.ID {
			t.Error("foreign key was wrong value", a.SetID, x.ID)
		}
	}
}

func testCommitsReload(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	if err = o.Reload(ctx, tx); err != nil {
		t.Error(err)
	}
}

func testCommitsReloadAll(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	slice := CommitSlice{o}

	if err = slice.ReloadAll(ctx, tx); err != nil {
		t.Error(err)
	}
}

func testCommitsSelect(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	slice, err := Commits().All(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if len(slice) != 1 {
		t.Error("want one record, got:", len(slice))
	}
}

var (
	commitDBTypes = map[string]string{`Ref`: `bytea`, `Timestamp`: `timestamp with time zone`, `Tree`: `bytea`, `SetID`: `integer`}
	_             = bytes.MinRead
)

func testCommitsUpdate(t *testing.T) {
	t.Parallel()

	if 0 == len(commitPrimaryKeyColumns) {
		t.Skip("Skipping table with no primary key columns")
	}
	if len(commitAllColumns) == len(commitPrimaryKeyColumns) {
		t.Skip("Skipping table with only primary key columns")
	}

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 1 {
		t.Error("want one record, got:", count)
	}

	if err = randomize.Struct(seed, o, commitDBTypes, true, commitPrimaryKeyColumns...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	if rowsAff, err := o.Update(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("should only affect one row but affected", rowsAff)
	}
}

func testCommitsSliceUpdateAll(t *testing.T) {
	t.Parallel()

	if len(commitAllColumns) == len(commitPrimaryKeyColumns) {
		t.Skip("Skipping table with only primary key columns")
	}

	seed := randomize.NewSeed()
	var err error
	o := &Commit{}
	if err = randomize.Struct(seed, o, commitDBTypes, true, commitColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 1 {
		t.Error("want one record, got:", count)
	}

	if err = randomize.Struct(seed, o, commitDBTypes, true, commitPrimaryKeyColumns...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	// Remove Primary keys and unique columns from what we plan to update
	var fields []string
	if strmangle.StringSliceMatch(commitAllColumns, commitPrimaryKeyColumns) {
		fields = commitAllColumns
	} else {
		fields = strmangle.SetComplement(
			commitAllColumns,
			commitPrimaryKeyColumns,
		)
	}

	value := reflect.Indirect(reflect.ValueOf(o))
	typ := reflect.TypeOf(o).Elem()
	n := typ.NumField()

	updateMap := M{}
	for _, col := range fields {
		for i := 0; i < n; i++ {
			f := typ.Field(i)
			if f.Tag.Get("boil") == col {
				updateMap[col] = value.Field(i).Interface()
			}
		}
	}

	slice := CommitSlice{o}
	if rowsAff, err := slice.UpdateAll(ctx, tx, updateMap); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("wanted one record updated but got", rowsAff)
	}
}

func testCommitsUpsert(t *testing.T) {
	t.Parallel()

	if len(commitAllColumns) == len(commitPrimaryKeyColumns) {
		t.Skip("Skipping table with only primary key columns")
	}

	seed := randomize.NewSeed()
	var err error
	// Attempt the INSERT side of an UPSERT
	o := Commit{}
	if err = randomize.Struct(seed, &o, commitDBTypes, true); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Upsert(ctx, tx, false, nil, boil.Infer(), boil.Infer()); err != nil {
		t.Errorf("Unable to upsert Commit: %s", err)
	}

	count, err := Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}
	if count != 1 {
		t.Error("want one record, got:", count)
	}

	// Attempt the UPDATE side of an UPSERT
	if err = randomize.Struct(seed, &o, commitDBTypes, false, commitPrimaryKeyColumns...); err != nil {
		t.Errorf("Unable to randomize Commit struct: %s", err)
	}

	if err = o.Upsert(ctx, tx, true, nil, boil.Infer(), boil.Infer()); err != nil {
		t.Errorf("Unable to upsert Commit: %s", err)
	}

	count, err = Commits().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}
	if count != 1 {
		t.Error("want one record, got:", count)
	}
}
