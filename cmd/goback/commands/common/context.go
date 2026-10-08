package common

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gobackio/goback/auth"

	"github.com/urfave/cli"
)

// Context returns the command's context: cancelled by the first SIGINT or
// SIGTERM, with a second signal ending the process as usual, and carrying
// the agent as the principal.
func Context(c *cli.Context) context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	go func() {
		<-ctx.Done()
		stop()
	}()

	return auth.WithPrincipal(ctx, &auth.Principal{AgentID: AgentID(c)})
}

// AgentID is the global --agent-id flag, or the hostname.
func AgentID(c *cli.Context) string {
	if agent := c.GlobalString("agent-id"); agent != "" {
		return agent
	}

	host, _ := os.Hostname()

	return host
}
