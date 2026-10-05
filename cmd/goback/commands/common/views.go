package common

import "github.com/twcclan/goback/cmd/goback/commands/common/views/gen"

// View turns what a command did into what its JSON output shows.
var View = gen.MapperImpl{}

// Done is the result of a command that changed something and has nothing
// more to say than what it changed.
type Done struct {
	Action string `json:"action"`
	Ref    string `json:"ref,omitempty"`
	Name   string `json:"name,omitempty"`
}
