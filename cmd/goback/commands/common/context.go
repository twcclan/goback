package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/twcclan/goback/auth"

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

// APIKey is what a goback:// store server is shown: the global
// --api-key-file, else --api-key or GOBACK_API_KEY.
func APIKey(c *cli.Context) (string, error) {
	if path := c.GlobalString("api-key-file"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading the api key: %w", err)
		}

		return strings.TrimSpace(string(data)), nil
	}

	key := c.GlobalString("api-key")
	if key == "" {
		return "", errors.New("--api-key, --api-key-file or GOBACK_API_KEY is required for a goback:// store")
	}

	return key, nil
}
