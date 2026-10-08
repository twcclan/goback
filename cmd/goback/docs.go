package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/urfave/cli"
)

type docCommand struct {
	Name        string       `json:"name"`
	Aliases     []string     `json:"aliases,omitempty"`
	Usage       string       `json:"usage,omitempty"`
	Description string       `json:"description,omitempty"`
	ArgsUsage   string       `json:"argsUsage,omitempty"`
	Flags       []docFlag    `json:"flags,omitempty"`
	Commands    []docCommand `json:"commands,omitempty"`
}

type docFlag struct {
	Names   []string `json:"names"`
	Usage   string   `json:"usage,omitempty"`
	Env     []string `json:"env,omitempty"`
	Default string   `json:"default,omitempty"`
	Takes   bool     `json:"takesValue"`
}

var docsCommand = cli.Command{
	Name:   "docs",
	Usage:  "print every command and flag as JSON, for the reference docs",
	Hidden: true,
	Action: func(c *cli.Context) error {
		root := docCommand{
			Name:        c.App.Name,
			Usage:       c.App.Usage,
			Description: c.App.Description,
			Flags:       docFlags(c.App.Flags),
			Commands:    docCommands(c.App.Commands),
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")

		return enc.Encode(root)
	},
}

func docCommands(cmds []cli.Command) []docCommand {
	var out []docCommand
	for _, cmd := range cmds {
		if cmd.Hidden || cmd.Name == "help" {
			continue
		}

		out = append(out, docCommand{
			Name:        cmd.Name,
			Aliases:     cmd.Aliases,
			Usage:       cmd.Usage,
			Description: cmd.Description,
			ArgsUsage:   cmd.ArgsUsage,
			Flags:       docFlags(cmd.Flags),
			Commands:    docCommands(cmd.Subcommands),
		})
	}

	return out
}

// docFlags reads the fields every urfave/cli v1 flag type shares by name,
// since the Flag interface exposes only the name.
func docFlags(flags []cli.Flag) []docFlag {
	var out []docFlag
	for _, f := range flags {
		v := reflect.Indirect(reflect.ValueOf(f))
		if field(v, "Hidden").IsValid() && field(v, "Hidden").Bool() {
			continue
		}

		d := docFlag{}
		for _, name := range strings.Split(f.GetName(), ",") {
			d.Names = append(d.Names, strings.TrimSpace(name))
		}

		if usage := field(v, "Usage"); usage.IsValid() {
			d.Usage = usage.String()
		}

		if env := field(v, "EnvVar"); env.IsValid() && env.String() != "" {
			for _, name := range strings.Split(env.String(), ",") {
				d.Env = append(d.Env, strings.TrimSpace(name))
			}
		}

		if value := field(v, "Value"); value.IsValid() {
			d.Takes = true
			if !value.IsZero() {
				d.Default = fmt.Sprint(value.Interface())
			}
		}

		out = append(out, d)
	}

	return out
}

func field(v reflect.Value, name string) reflect.Value {
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}

	return v.FieldByName(name)
}
