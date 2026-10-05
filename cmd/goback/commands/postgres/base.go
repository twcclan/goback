package postgres

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/postgres"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/cmd/goback/commands/common/views"

	"github.com/urfave/cli"
)

var baseCmd = cli.Command{
	Name:  "base",
	Usage: "take a base backup with pg_basebackup and commit it to the set named by --set",
	Description: "pg_basebackup connects as libpq does: through --dbname, or through PGHOST, PGUSER and the\n" +
		"other PG* variables. The user needs the REPLICATION attribute.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "dbname",
			Usage:  "connection string pg_basebackup connects with",
			EnvVar: "GOBACK_POSTGRES_DBNAME",
		},
		cli.StringFlag{
			Name:   "pg-basebackup",
			Usage:  "the pg_basebackup to run; its major version must match the server's",
			EnvVar: "GOBACK_POSTGRES_BASEBACKUP",
			Value:  "pg_basebackup",
		},
	},
	Action: baseAction,
}

func baseAction(c *cli.Context) error {
	if c.GlobalString("set") == "" {
		return cli.NewExitError("--set is required", 2)
	}

	store := common.GetObjectStore(c)
	index := common.OpenIndex(c, store)

	defer func() {
		common.CloseAll(store, index)
	}()

	// everything that can exit the process comes before pg_basebackup starts:
	// an orphaned pg_basebackup is reparented to PID 1, which in the Postgres
	// container is the postmaster, and its death restarts the cluster
	sessions, _ := store.(backup.SessionStore)

	run := &postgres.BaseBackup{
		Walker: &backup.Walker{
			Index:    index,
			Objects:  index,
			Sessions: sessions,
			Set:      c.GlobalString("set"),
			AgentID:  common.AgentID(c),
			Key:      common.StoreKey(c, store),
			Workers:  4,
		},
	}

	ctx := common.Context(c)

	args := []string{"-D", "-", "-Ft", "-X", "fetch", "--checkpoint=fast"}
	if dbname := c.String("dbname"); dbname != "" {
		args = append(args, "--dbname", dbname)
	}

	cmd := exec.CommandContext(ctx, c.String("pg-basebackup"), args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting pg_basebackup: %w", err)
	}

	waited := false
	wait := func() error {
		waited = true
		if err := cmd.Wait(); err != nil {
			return fmt.Errorf("pg_basebackup: %w", err)
		}

		return nil
	}

	result, err := run.Run(ctx, stdout, wait)
	if !waited {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}

	if err != nil {
		return err
	}

	common.Result(struct {
		views.WalkView
		WALStart string `json:"wal_start"`
	}{common.View.Walk(result), result.Commit.Metadata[postgres.MetaStartWALFile]}, func() {
		log.Printf("Commit %x: base backup of %d bytes, WAL from %s", result.Ref.Hash, result.Bytes, result.Commit.Metadata[postgres.MetaStartWALFile])
	})

	return nil
}
