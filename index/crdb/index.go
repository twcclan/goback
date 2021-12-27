package crdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index/crdb/models"
	"github.com/twcclan/goback/proto"

	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

var _ backup.Index = (*Index)(nil)

func (c *Index) FileInfo(ctx context.Context, backupSet string, path string, notAfter time.Time, count int) ([]*proto.TreeNode, error) {
	set, err := models.Sets(
		models.SetWhere.Name.EQ(backupSet),
	).One(ctx, c.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// if the backup set does not exist, we also won't have the file
			return nil, nil
		}

		return nil, err
	}

	files, err := models.Files(
		models.FileWhere.Path.EQ(path),
		models.FileWhere.Timestamp.LTE(notAfter),
		models.FileWhere.SetID.EQ(set.ID),
		qm.OrderBy(fmt.Sprintf(`"%s" DESC`, models.FileColumns.Timestamp)),
		qm.Limit(count),
	).All(ctx, c.db)

	if err != nil {
		return nil, err
	}

	infoList := make([]*proto.TreeNode, len(files))
	for i, file := range files {
		infoList[i] = &proto.TreeNode{
			Stat: &proto.FileInfo{
				Name:      filepath.Base(file.Path),
				Mode:      uint32(file.Mode),
				User:      file.User,
				Group:     file.Group,
				Timestamp: file.Timestamp.Unix(),
				Size:      file.Size,
				Tree:      false,
			},
			Ref: &proto.Ref{Sha1: file.Ref},
		}
	}

	return infoList, nil
}

func (c *Index) CommitInfo(ctx context.Context, backupSet string, notAfter time.Time, count int) ([]*proto.Commit, error) {
	set, err := models.Sets(
		models.SetWhere.Name.EQ(backupSet),
	).One(ctx, c.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// if the backup set does not exist, we also won't have the commit
			return nil, nil
		}

		return nil, err
	}

	commits, err := models.Commits(
		models.CommitWhere.Timestamp.LTE(notAfter),
		models.CommitWhere.SetID.EQ(set.ID),
		qm.Limit(count),
		qm.OrderBy(fmt.Sprintf("%s DESC", models.CommitColumns.Timestamp)),
	).All(ctx, c.db)

	if err != nil {
		return nil, err
	}

	infoList := make([]*proto.Commit, len(commits))

	for i, commit := range commits {
		infoList[i] = &proto.Commit{
			Timestamp: commit.Timestamp.Unix(),
			Tree:      &proto.Ref{Sha1: commit.Tree},
			BackupSet: backupSet,
		}
	}

	return infoList, nil
}

func (c *Index) Put(ctx context.Context, object *proto.Object) error {
	// store the object first
	err := c.ObjectStore.Put(ctx, object)
	if err != nil {
		return err
	}

	switch object.Type() {
	case proto.ObjectType_COMMIT:
		return c.indexCommit(ctx, object.GetCommit(), object.Ref())
	}

	return nil
}

func (i *Index) indexCommit(ctx context.Context, commit *proto.Commit, ref *proto.Ref) error {
	// whenever we get to index a commit
	// we'll traverse the complete backup tree
	// to create our filesystem path index

	// TODO: this might break due to transaction size limits. will likely need to set up some
	// 	kind of asynchronous batch processing

	treeObj, err := i.ObjectStore.Get(ctx, commit.Tree)
	if err != nil {
		log.Printf("Failed getting root tree: %s", err)
		return err
	}

	if treeObj == nil {
		log.Printf("Root tree %x could not be retrieved", commit.Tree.Sha1)
		return nil
	}

	tx, err := i.db.Begin()
	if err != nil {
		log.Printf("Failed starting transaction: %s", err)
		return err
	}
	defer tx.Rollback()

	_, err = models.FindCommit(ctx, tx, ref.Sha1)
	if !errors.Is(err, sql.ErrNoRows) {
		// if the commit exists already, we can skip here
		return err
	}

	start := time.Now()

	set, err := models.Sets(models.SetWhere.Name.EQ(commit.GetBackupSet())).One(ctx, tx)
	if err != nil {
		// if the set does not exist in the database, we'll just create it here
		if errors.Is(err, sql.ErrNoRows) {
			set = &models.Set{
				Name: commit.GetBackupSet(),
			}

			err = set.Insert(ctx, tx, boil.Infer())
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	var txMtx sync.Mutex
	err = backup.TraverseTree(ctx, i.ObjectStore, treeObj, 64, func(filepath string, node *proto.TreeNode) error {
		// seems pg doesn't appreciate concurrent inserts on the same transactions,
		// just synchronize everything for now
		txMtx.Lock()
		defer txMtx.Unlock()

		info := node.GetStat()
		if info.GetTree() {
			dbTree, err := models.FindTree(ctx, tx, node.Ref.Sha1, filepath, set.ID)
			if errors.Is(err, sql.ErrNoRows) {
				// if we get here, it means the tree doesn't currently exist in the database,
				// so we'll create it so we can skip indexing this sub-tree next time we see it
				dbTree = &models.Tree{
					Ref:   node.Ref.Sha1,
					Path:  filepath,
					SetID: set.ID,
				}

				err = dbTree.Insert(ctx, tx, boil.Infer())
				return err
			}

			if err != nil {
				return err
			}

			return backup.SkipTree
		}

		file := &models.File{
			Path:      filepath,
			Timestamp: time.Unix(info.GetTimestamp(), 0),
			SetID:     set.ID,
			Ref:       node.GetRef().Sha1,
			Mode:      int64(info.Mode),
			User:      info.User,
			Group:     info.Group,
			Size:      info.Size,
		}

		return file.Upsert(ctx, tx, false, nil, boil.Infer(), boil.Infer())
	})
	if err != nil {
		log.Printf("Ignoring commit %x, failed traversing tree: %s", proto.NewObject(commit).Ref().Sha1, err)
		return nil
	}

	duration := time.Since(start)
	log.Printf("Indexed commit(%s) in %v", set.Name, duration)

	c := &models.Commit{
		Ref:       ref.Sha1,
		Timestamp: time.Unix(commit.Timestamp, 0),
		Tree:      treeObj.Ref().Sha1,
		SetID:     set.ID,
	}

	err = c.Insert(ctx, tx, boil.Infer())
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (c *Index) Delete(ctx context.Context, ref *proto.Ref) error {
	return errors.New("direct deletion of objects not supported")
}

func (i *Index) ReIndex(ctx context.Context) error {
	return i.ObjectStore.Walk(ctx, true, proto.ObjectType_COMMIT, func(obj *proto.Object) error {
		return i.indexCommit(ctx, obj.GetCommit(), obj.Ref())
	})
}
