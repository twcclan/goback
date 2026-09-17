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

var Command = cli.Command{
	Name:        "key",
	Description: "Manage the store key that encrypts names and contents before upload",
	Subcommands: []cli.Command{
		{
			Name:        "new",
			Description: "Generate a store key file",
			Action:      newAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "store", Usage: "id of the store the key belongs to"},
				cli.StringFlag{Name: "out", Usage: "key file to write", Value: "store.key"},
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
			Action:      recoverAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "store", Usage: "id of the store the key belongs to"},
				cli.StringFlag{Name: "escrow", Usage: "base64 escrowed key, as printed by escrow"},
				cli.StringFlag{Name: "out", Usage: "key file to write", Value: "store.key"},
				passphraseFlag,
			},
		},
	},
}

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

func newAction(c *cli.Context) error {
	if c.String("store") == "" {
		return errors.New("--store is required")
	}

	if _, err := os.Stat(c.String("out")); err == nil {
		return fmt.Errorf("%s exists, refusing to overwrite a store key", c.String("out"))
	}

	key, err := storekey.Generate(c.String("store"))
	if err != nil {
		return err
	}

	err = key.Save(c.String("out"))
	if err != nil {
		return err
	}

	fmt.Printf("wrote key %s for store %s to %s\n", key.IDString(), key.StoreID, c.String("out"))

	return nil
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
	if c.String("store") == "" {
		return errors.New("--store is required")
	}

	pass, err := passphrase(c)
	if err != nil {
		return err
	}

	escrowed, err := base64.StdEncoding.DecodeString(c.String("escrow"))
	if err != nil {
		return fmt.Errorf("decoding escrowed key: %w", err)
	}

	key, err := storekey.Recover(c.String("store"), escrowed, pass, storekey.DefaultPolicy())
	if err != nil {
		return err
	}

	if _, err := os.Stat(c.String("out")); err == nil {
		return fmt.Errorf("%s exists, refusing to overwrite a store key", c.String("out"))
	}

	err = key.Save(c.String("out"))
	if err != nil {
		return err
	}

	fmt.Printf("recovered key %s for store %s into %s\n", key.IDString(), key.StoreID, c.String("out"))

	return nil
}
