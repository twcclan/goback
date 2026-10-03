// Package key manages the client-held store key and a store's at-rest key.
package key

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/backup/storekey/master"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/storage"
	"github.com/twcclan/goback/storage/pack"

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
			Flags:       []cli.Flag{outFlag("store.key")},
		},
		{
			Name:        "master",
			Description: "Generate a master key file that store keys can be derived from",
			Action:      masterAction,
			Flags:       []cli.Flag{outFlag("master.key")},
		},
		{
			Name:        "derive",
			Description: "Derive the named store's key from a master key file; the same master and name always give the same key",
			ArgsUsage:   "<name>",
			Action:      deriveAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "master", Usage: "master key file to derive from", Value: "master.key"},
				outFlag("store.key"),
			},
		},
		{
			Name:        "escrow",
			Description: "Wrap a store key under a passphrase as an armored age file, which `age -d` also opens, and print it or keep it with the store",
			Action:      escrowAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "key", Usage: "key file to escrow", Value: "store.key"},
				passphraseFlag,
				cli.StringFlag{Name: "upload", Usage: "admin surface of the store server (https://host:port) to keep the escrowed key with, instead of printing it"},
				cli.StringFlag{Name: "admin-token", Usage: "the server's admin token", EnvVar: "GOBACK_ADMIN_TOKEN"},
				cli.StringFlag{Name: "admin-ca", Usage: "PEM certificate authority the admin surface must present; empty trusts the system roots"},
			},
		},
		{
			Name:        "id",
			Description: "Print the id of a store key, which names it in the store and in its escrowed copies",
			Action:      idAction,
			Flags:       []cli.Flag{cli.StringFlag{Name: "key", Usage: "key file", Value: "store.key"}},
		},
		{
			Name:        "recover",
			Description: "Rebuild a key file from an escrowed key and its passphrase",
			Action:      recoverAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "escrow", Usage: "escrowed key file, as printed by escrow; - reads standard input", Value: "-"},
				outFlag("store.key"),
				passphraseFlag,
			},
		},
		{
			Name:        "at-rest",
			Description: "Generate the key a pack:// or gcs:// store seals its archives with, or rotate it",
			Action:      atRestAction,
			Flags: []cli.Flag{
				outFlag("at-rest.key"),
				cli.BoolFlag{Name: "rotate", Usage: "add a fresh primary key to the file; the keys it held still open what they sealed"},
			},
		},
	},
}

func outFlag(value string) cli.StringFlag {
	return cli.StringFlag{Name: "out", Usage: "key file to write", Value: value}
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

// storeName is the store name the command was given.
func storeName(c *cli.Context) (string, error) {
	if c.NArg() != 1 || c.Args().First() == "" {
		return "", fmt.Errorf("usage: key %s <name>", c.Command.Name)
	}

	return c.Args().First(), nil
}

// fresh refuses to replace an existing key file.
func fresh(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s exists, refusing to overwrite a key", path)
	}

	return nil
}

// write saves the key under --out.
func write(c *cli.Context, key *storekey.Key, verb string) error {
	out := c.String("out")
	if err := fresh(out); err != nil {
		return err
	}

	err := key.Save(out)
	if err != nil {
		return err
	}

	common.Result(keyWritten{Action: verb, KeyID: key.IDString(), Store: key.Name, Path: out}, func() {
		fmt.Printf("%s key %s for store %s to %s\n", verb, key.IDString(), key.Name, out)
	})

	return nil
}

// keyWritten is a key a command saved, as JSON output shows it.
type keyWritten struct {
	Action string `json:"action"`
	KeyID  string `json:"key_id,omitempty"`
	Store  string `json:"store,omitempty"`
	Path   string `json:"path"`
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

func masterAction(c *cli.Context) error {
	out := c.String("out")
	if err := fresh(out); err != nil {
		return err
	}

	m, err := master.Generate()
	if err != nil {
		return err
	}

	err = m.Save(out)
	if err != nil {
		return err
	}

	common.Result(keyWritten{Action: "wrote", Path: out}, func() { fmt.Printf("wrote master key to %s\n", out) })

	return nil
}

func deriveAction(c *cli.Context) error {
	name, err := storeName(c)
	if err != nil {
		return err
	}

	m, err := master.Load(c.String("master"))
	if err != nil {
		return err
	}

	key, err := m.Derive(name)
	if err != nil {
		return err
	}

	return write(c, key, "derived")
}

// minPassphrase is the shortest passphrase a key is escrowed under: anyone
// holding an agent key of the store can fetch the escrowed copy and guess
// at it offline.
const minPassphrase = 12

func escrowAction(c *cli.Context) error {
	pass, err := passphrase(c)
	if err != nil {
		return err
	}

	if len([]rune(pass)) < minPassphrase {
		return fmt.Errorf("the passphrase needs at least %d characters", minPassphrase)
	}

	key, err := storekey.Load(c.String("key"))
	if err != nil {
		return err
	}

	escrowed, err := key.Escrow(pass)
	if err != nil {
		return err
	}

	if server := c.String("upload"); server != "" {
		return upload(c, server, key.IDString(), escrowed)
	}

	if common.JSON() {
		common.Result(struct {
			KeyID    string `json:"key_id"`
			Escrowed string `json:"escrowed"`
		}{key.IDString(), string(escrowed)}, nil)

		return nil
	}

	_, err = os.Stdout.Write(escrowed)

	return err
}

// upload keeps the escrowed key with the store through its admin surface.
func upload(c *cli.Context, server, keyID string, escrowed []byte) error {
	token := c.String("admin-token")
	if token == "" {
		return errors.New("--upload needs the admin token (--admin-token or GOBACK_ADMIN_TOKEN)")
	}

	tlsConfig, err := storage.ClientTLS(c.String("admin-ca"))
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]string{"key_id": keyID, "escrowed": string(escrowed)})
	if err != nil {
		return err
	}

	request, err := http.NewRequest(http.MethodPut, strings.TrimSuffix(server, "/")+"/v1/escrow", bytes.NewReader(body))
	if err != nil {
		return err
	}

	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}, Timeout: time.Minute}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		reply, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("the store refused the escrowed key: %s: %s", response.Status, strings.TrimSpace(string(reply)))
	}

	common.Result(struct {
		Action string `json:"action"`
		KeyID  string `json:"key_id"`
	}{"uploaded", keyID}, func() {
		fmt.Printf("the store keeps key %s escrowed; start agents with GOBACK_PASSPHRASE and no --store-key\n", keyID)
	})

	return nil
}

func idAction(c *cli.Context) error {
	key, err := storekey.Load(c.String("key"))
	if err != nil {
		return err
	}

	if common.JSON() {
		common.Result(struct {
			KeyID string `json:"key_id"`
			Store string `json:"store"`
		}{key.IDString(), key.Name}, nil)

		return nil
	}

	_, err = fmt.Fprintln(c.App.Writer, key.IDString())

	return err
}

func recoverAction(c *cli.Context) error {
	pass, err := passphrase(c)
	if err != nil {
		return err
	}

	var escrowed []byte
	if path := c.String("escrow"); path == "-" {
		escrowed, err = io.ReadAll(os.Stdin)
	} else {
		escrowed, err = os.ReadFile(path)
	}

	if err != nil {
		return fmt.Errorf("reading the escrowed key: %w", err)
	}

	key, err := storekey.Recover(escrowed, pass)
	if err != nil {
		return err
	}

	return write(c, key, "recovered")
}

func atRestAction(c *cli.Context) error {
	out := c.String("out")

	if !c.Bool("rotate") {
		if err := fresh(out); err != nil {
			return err
		}

		key, err := pack.GenerateAtRestKey()
		if err != nil {
			return err
		}

		err = key.Save(out)
		if err != nil {
			return err
		}

		common.Result(keyWritten{Action: "wrote", KeyID: fmt.Sprintf("%x", key.ID()), Path: out}, func() {
			fmt.Printf("wrote at-rest key %x to %s\n", key.ID(), out)
		})

		return nil
	}

	key, err := pack.LoadAtRestKey(out)
	if err != nil {
		return err
	}

	rotated, err := key.Rotate()
	if err != nil {
		return err
	}

	err = rotated.Save(out)
	if err != nil {
		return err
	}

	common.Result(keyWritten{Action: "rotated", KeyID: fmt.Sprintf("%x", rotated.ID()), Path: out}, func() {
		fmt.Printf("rotated %s: records are sealed under %x from now on; a compaction re-seals the older ones\n", out, rotated.ID())
	})

	return nil
}
