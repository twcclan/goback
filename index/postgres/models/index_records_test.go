// Code generated by SQLBoiler 4.8.3 (https://github.com/volatiletech/sqlboiler). DO NOT EDIT.
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

func testIndexRecords(t *testing.T) {
	t.Parallel()

	query := IndexRecords()

	if query.Query == nil {
		t.Error("expected a query, got nothing")
	}
}

func testIndexRecordsDelete(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
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

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 0 {
		t.Error("want zero records, got:", count)
	}
}

func testIndexRecordsQueryDeleteAll(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	if rowsAff, err := IndexRecords().DeleteAll(ctx, tx); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("should only have deleted one row, but affected:", rowsAff)
	}

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 0 {
		t.Error("want zero records, got:", count)
	}
}

func testIndexRecordsSliceDeleteAll(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	slice := IndexRecordSlice{o}

	if rowsAff, err := slice.DeleteAll(ctx, tx); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("should only have deleted one row, but affected:", rowsAff)
	}

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 0 {
		t.Error("want zero records, got:", count)
	}
}

func testIndexRecordsExists(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	e, err := IndexRecordExists(ctx, tx, o.Ref, o.ArchiveID)
	if err != nil {
		t.Errorf("Unable to check if IndexRecord exists: %s", err)
	}
	if !e {
		t.Errorf("Expected IndexRecordExists to return true, but got false.")
	}
}

func testIndexRecordsFind(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	indexRecordFound, err := FindIndexRecord(ctx, tx, o.Ref, o.ArchiveID)
	if err != nil {
		t.Error(err)
	}

	if indexRecordFound == nil {
		t.Error("want a record, got nil")
	}
}

func testIndexRecordsBind(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	if err = IndexRecords().Bind(ctx, tx, o); err != nil {
		t.Error(err)
	}
}

func testIndexRecordsOne(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	if x, err := IndexRecords().One(ctx, tx); err != nil {
		t.Error(err)
	} else if x == nil {
		t.Error("expected to get a non nil record")
	}
}

func testIndexRecordsAll(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	indexRecordOne := &IndexRecord{}
	indexRecordTwo := &IndexRecord{}
	if err = randomize.Struct(seed, indexRecordOne, indexRecordDBTypes, false, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}
	if err = randomize.Struct(seed, indexRecordTwo, indexRecordDBTypes, false, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = indexRecordOne.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}
	if err = indexRecordTwo.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	slice, err := IndexRecords().All(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if len(slice) != 2 {
		t.Error("want 2 records, got:", len(slice))
	}
}

func testIndexRecordsCount(t *testing.T) {
	t.Parallel()

	var err error
	seed := randomize.NewSeed()
	indexRecordOne := &IndexRecord{}
	indexRecordTwo := &IndexRecord{}
	if err = randomize.Struct(seed, indexRecordOne, indexRecordDBTypes, false, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}
	if err = randomize.Struct(seed, indexRecordTwo, indexRecordDBTypes, false, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = indexRecordOne.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}
	if err = indexRecordTwo.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 2 {
		t.Error("want 2 records, got:", count)
	}
}

func indexRecordBeforeInsertHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func indexRecordAfterInsertHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func indexRecordAfterSelectHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func indexRecordBeforeUpdateHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func indexRecordAfterUpdateHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func indexRecordBeforeDeleteHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func indexRecordAfterDeleteHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func indexRecordBeforeUpsertHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func indexRecordAfterUpsertHook(ctx context.Context, e boil.ContextExecutor, o *IndexRecord) error {
	*o = IndexRecord{}
	return nil
}

func testIndexRecordsHooks(t *testing.T) {
	t.Parallel()

	var err error

	ctx := context.Background()
	empty := &IndexRecord{}
	o := &IndexRecord{}

	seed := randomize.NewSeed()
	if err = randomize.Struct(seed, o, indexRecordDBTypes, false); err != nil {
		t.Errorf("Unable to randomize IndexRecord object: %s", err)
	}

	AddIndexRecordHook(boil.BeforeInsertHook, indexRecordBeforeInsertHook)
	if err = o.doBeforeInsertHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doBeforeInsertHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected BeforeInsertHook function to empty object, but got: %#v", o)
	}
	indexRecordBeforeInsertHooks = []IndexRecordHook{}

	AddIndexRecordHook(boil.AfterInsertHook, indexRecordAfterInsertHook)
	if err = o.doAfterInsertHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterInsertHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterInsertHook function to empty object, but got: %#v", o)
	}
	indexRecordAfterInsertHooks = []IndexRecordHook{}

	AddIndexRecordHook(boil.AfterSelectHook, indexRecordAfterSelectHook)
	if err = o.doAfterSelectHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterSelectHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterSelectHook function to empty object, but got: %#v", o)
	}
	indexRecordAfterSelectHooks = []IndexRecordHook{}

	AddIndexRecordHook(boil.BeforeUpdateHook, indexRecordBeforeUpdateHook)
	if err = o.doBeforeUpdateHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doBeforeUpdateHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected BeforeUpdateHook function to empty object, but got: %#v", o)
	}
	indexRecordBeforeUpdateHooks = []IndexRecordHook{}

	AddIndexRecordHook(boil.AfterUpdateHook, indexRecordAfterUpdateHook)
	if err = o.doAfterUpdateHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterUpdateHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterUpdateHook function to empty object, but got: %#v", o)
	}
	indexRecordAfterUpdateHooks = []IndexRecordHook{}

	AddIndexRecordHook(boil.BeforeDeleteHook, indexRecordBeforeDeleteHook)
	if err = o.doBeforeDeleteHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doBeforeDeleteHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected BeforeDeleteHook function to empty object, but got: %#v", o)
	}
	indexRecordBeforeDeleteHooks = []IndexRecordHook{}

	AddIndexRecordHook(boil.AfterDeleteHook, indexRecordAfterDeleteHook)
	if err = o.doAfterDeleteHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterDeleteHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterDeleteHook function to empty object, but got: %#v", o)
	}
	indexRecordAfterDeleteHooks = []IndexRecordHook{}

	AddIndexRecordHook(boil.BeforeUpsertHook, indexRecordBeforeUpsertHook)
	if err = o.doBeforeUpsertHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doBeforeUpsertHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected BeforeUpsertHook function to empty object, but got: %#v", o)
	}
	indexRecordBeforeUpsertHooks = []IndexRecordHook{}

	AddIndexRecordHook(boil.AfterUpsertHook, indexRecordAfterUpsertHook)
	if err = o.doAfterUpsertHooks(ctx, nil); err != nil {
		t.Errorf("Unable to execute doAfterUpsertHooks: %s", err)
	}
	if !reflect.DeepEqual(o, empty) {
		t.Errorf("Expected AfterUpsertHook function to empty object, but got: %#v", o)
	}
	indexRecordAfterUpsertHooks = []IndexRecordHook{}
}

func testIndexRecordsInsert(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 1 {
		t.Error("want one record, got:", count)
	}
}

func testIndexRecordsInsertWhitelist(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Whitelist(indexRecordColumnsWithoutDefault...)); err != nil {
		t.Error(err)
	}

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 1 {
		t.Error("want one record, got:", count)
	}
}

func testIndexRecordToOneArchiveUsingArchive(t *testing.T) {
	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()

	var local IndexRecord
	var foreign Archive

	seed := randomize.NewSeed()
	if err := randomize.Struct(seed, &local, indexRecordDBTypes, false, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}
	if err := randomize.Struct(seed, &foreign, archiveDBTypes, false, archiveColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize Archive struct: %s", err)
	}

	if err := foreign.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Fatal(err)
	}

	local.ArchiveID = foreign.ID
	if err := local.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Fatal(err)
	}

	check, err := local.Archive().One(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}

	if check.ID != foreign.ID {
		t.Errorf("want: %v, got %v", foreign.ID, check.ID)
	}

	slice := IndexRecordSlice{&local}
	if err = local.L.LoadArchive(ctx, tx, false, (*[]*IndexRecord)(&slice), nil); err != nil {
		t.Fatal(err)
	}
	if local.R.Archive == nil {
		t.Error("struct should have been eager loaded")
	}

	local.R.Archive = nil
	if err = local.L.LoadArchive(ctx, tx, true, &local, nil); err != nil {
		t.Fatal(err)
	}
	if local.R.Archive == nil {
		t.Error("struct should have been eager loaded")
	}
}

func testIndexRecordToOneSetOpArchiveUsingArchive(t *testing.T) {
	var err error

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()

	var a IndexRecord
	var b, c Archive

	seed := randomize.NewSeed()
	if err = randomize.Struct(seed, &a, indexRecordDBTypes, false, strmangle.SetComplement(indexRecordPrimaryKeyColumns, indexRecordColumnsWithoutDefault)...); err != nil {
		t.Fatal(err)
	}
	if err = randomize.Struct(seed, &b, archiveDBTypes, false, strmangle.SetComplement(archivePrimaryKeyColumns, archiveColumnsWithoutDefault)...); err != nil {
		t.Fatal(err)
	}
	if err = randomize.Struct(seed, &c, archiveDBTypes, false, strmangle.SetComplement(archivePrimaryKeyColumns, archiveColumnsWithoutDefault)...); err != nil {
		t.Fatal(err)
	}

	if err := a.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Fatal(err)
	}
	if err = b.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Fatal(err)
	}

	for i, x := range []*Archive{&b, &c} {
		err = a.SetArchive(ctx, tx, i != 0, x)
		if err != nil {
			t.Fatal(err)
		}

		if a.R.Archive != x {
			t.Error("relationship struct not set to correct value")
		}

		if x.R.IndexRecords[0] != &a {
			t.Error("failed to append to foreign relationship struct")
		}
		if a.ArchiveID != x.ID {
			t.Error("foreign key was wrong value", a.ArchiveID)
		}

		if exists, err := IndexRecordExists(ctx, tx, a.Ref, a.ArchiveID); err != nil {
			t.Fatal(err)
		} else if !exists {
			t.Error("want 'a' to exist")
		}

	}
}

func testIndexRecordsReload(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
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

func testIndexRecordsReloadAll(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	slice := IndexRecordSlice{o}

	if err = slice.ReloadAll(ctx, tx); err != nil {
		t.Error(err)
	}
}

func testIndexRecordsSelect(t *testing.T) {
	t.Parallel()

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	slice, err := IndexRecords().All(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if len(slice) != 1 {
		t.Error("want one record, got:", len(slice))
	}
}

var (
	indexRecordDBTypes = map[string]string{`Ref`: `bytea`, `Offset`: `integer`, `Length`: `integer`, `Type`: `integer`, `ArchiveID`: `integer`}
	_                  = bytes.MinRead
)

func testIndexRecordsUpdate(t *testing.T) {
	t.Parallel()

	if 0 == len(indexRecordPrimaryKeyColumns) {
		t.Skip("Skipping table with no primary key columns")
	}
	if len(indexRecordAllColumns) == len(indexRecordPrimaryKeyColumns) {
		t.Skip("Skipping table with only primary key columns")
	}

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 1 {
		t.Error("want one record, got:", count)
	}

	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordPrimaryKeyColumns...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	if rowsAff, err := o.Update(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("should only affect one row but affected", rowsAff)
	}
}

func testIndexRecordsSliceUpdateAll(t *testing.T) {
	t.Parallel()

	if len(indexRecordAllColumns) == len(indexRecordPrimaryKeyColumns) {
		t.Skip("Skipping table with only primary key columns")
	}

	seed := randomize.NewSeed()
	var err error
	o := &IndexRecord{}
	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordColumnsWithDefault...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Insert(ctx, tx, boil.Infer()); err != nil {
		t.Error(err)
	}

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}

	if count != 1 {
		t.Error("want one record, got:", count)
	}

	if err = randomize.Struct(seed, o, indexRecordDBTypes, true, indexRecordPrimaryKeyColumns...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	// Remove Primary keys and unique columns from what we plan to update
	var fields []string
	if strmangle.StringSliceMatch(indexRecordAllColumns, indexRecordPrimaryKeyColumns) {
		fields = indexRecordAllColumns
	} else {
		fields = strmangle.SetComplement(
			indexRecordAllColumns,
			indexRecordPrimaryKeyColumns,
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

	slice := IndexRecordSlice{o}
	if rowsAff, err := slice.UpdateAll(ctx, tx, updateMap); err != nil {
		t.Error(err)
	} else if rowsAff != 1 {
		t.Error("wanted one record updated but got", rowsAff)
	}
}

func testIndexRecordsUpsert(t *testing.T) {
	t.Parallel()

	if len(indexRecordAllColumns) == len(indexRecordPrimaryKeyColumns) {
		t.Skip("Skipping table with only primary key columns")
	}

	seed := randomize.NewSeed()
	var err error
	// Attempt the INSERT side of an UPSERT
	o := IndexRecord{}
	if err = randomize.Struct(seed, &o, indexRecordDBTypes, true); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	ctx := context.Background()
	tx := MustTx(boil.BeginTx(ctx, nil))
	defer func() { _ = tx.Rollback() }()
	if err = o.Upsert(ctx, tx, false, nil, boil.Infer(), boil.Infer()); err != nil {
		t.Errorf("Unable to upsert IndexRecord: %s", err)
	}

	count, err := IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}
	if count != 1 {
		t.Error("want one record, got:", count)
	}

	// Attempt the UPDATE side of an UPSERT
	if err = randomize.Struct(seed, &o, indexRecordDBTypes, false, indexRecordPrimaryKeyColumns...); err != nil {
		t.Errorf("Unable to randomize IndexRecord struct: %s", err)
	}

	if err = o.Upsert(ctx, tx, true, nil, boil.Infer(), boil.Infer()); err != nil {
		t.Errorf("Unable to upsert IndexRecord: %s", err)
	}

	count, err = IndexRecords().Count(ctx, tx)
	if err != nil {
		t.Error(err)
	}
	if count != 1 {
		t.Error("want one record, got:", count)
	}
}
