// Package key manages the client-held store key.
package key

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/twcclan/goback/backup/storekey"

	"github.com/urfave/cli"
)

// Command is the key command.
var Command = cli.Command{
	Name:        "key",
	Description: "Manage the store key that encrypts names and contents before upload",
	Subcommands: []cli.Command{
		{
			Name:        "new",
			Description: "Generate a store key file",
			ArgsUsage:   "<name>",
			Action:      newAction,
			Flags:       []cli.Flag{outFlag},
		},
		{
			Name:        "derive",
			Description: "Derive the named store's key from a master key file; the same master and name always give the same key",
			ArgsUsage:   "<name>",
			Action:      deriveAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "master", Usage: "key file to derive from", Value: "master.key"},
				outFlag,
			},
		},
		{
			Name:        "escrow",
			Description: "Wrap a store key under a passphrase and print it as base64",
			Action:      escrowAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "key", Usage: "key file to escrow", Value: "store.key"},
				passphraseFlag,
			},
		},
		{
			Name:        "recover",
			Description: "Rebuild a key file from an escrowed key and its passphrase",
			ArgsUsage:   "<name>",
			Action:      recoverAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "escrow", Usage: "base64 escrowed key, as printed by escrow"},
				outFlag,
				passphraseFlag,
			},
		},
	},
}

var outFlag = cli.StringFlag{Name: "out", Usage: "key file to write", Value: "store.key"}

var passphraseFlag = cli.StringFlag{
	Name:   "passphrase",
	Usage:  "escrow passphrase",
	EnvVar: "GOBACK_PASSPHRASE",
}

func passphrase(c *cli.Context) (string, error) {
	p := c.String("passphrase")
	if p == "" {
		return "", errors.New("a passphrase is required (--passphrase or GOBACK_PASSPHRASE)")
	}

	return p, nil
}

// storeName is the store name the command was given.
func storeName(c *cli.Context) (string, error) {
	if c.NArg() != 1 || c.Args().First() == "" {
		return "", fmt.Errorf("usage: key %s <name>", c.Command.Name)
	}

	return c.Args().First(), nil
}

// write saves the key under --out, refusing to replace a file.
func write(c *cli.Context, key *storekey.Key, verb string) error {
	if _, err := os.Stat(c.String("out")); err == nil {
		return fmt.Errorf("%s exists, refusing to overwrite a store key", c.String("out"))
	}

	err := key.Save(c.String("out"))
	if err != nil {
		return err
	}

	fmt.Printf("%s key %s for store %s to %s\n", verb, key.IDString(), key.Name, c.String("out"))

	return nil
}

func newAction(c *cli.Context) error {
	name, err := storeName(c)
	if err != nil {
		return err
	}

	key, err := storekey.Generate(name)
	if err != nil {
		return err
	}

	return write(c, key, "wrote")
}

func deriveAction(c *cli.Context) error {
	name, err := storeName(c)
	if err != nil {
		return err
	}

	master, err := storekey.Load(c.String("master"))
	if err != nil {
		return err
	}

	key, err := master.Derive(name)
	if err != nil {
		return err
	}

	return write(c, key, "derived")
}

func escrowAction(c *cli.Context) error {
	pass, err := passphrase(c)
	if err != nil {
		return err
	}

	key, err := storekey.Load(c.String("key"))
	if err != nil {
		return err
	}

	escrowed, err := key.Escrow(pass)
	if err != nil {
		return err
	}

	fmt.Println(base64.StdEncoding.EncodeToString(escrowed))

	return nil
}

func recoverAction(c *cli.Context) error {
	name, err := storeName(c)
	if err != nil {
		return err
	}

	pass, err := passphrase(c)
	if err != nil {
		return err
	}

	escrowed, err := base64.StdEncoding.DecodeString(c.String("escrow"))
	if err != nil {
		return fmt.Errorf("decoding escrowed key: %w", err)
	}

	key, err := storekey.Recover(name, escrowed, pass, storekey.DefaultPolicy())
	if err != nil {
		return err
	}

	return write(c, key, "recovered")
}
