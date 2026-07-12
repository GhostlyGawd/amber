package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/mcpserver"
)

func cmdServe() *cobra.Command {
	c := &cobra.Command{
		Use:   "serve",
		Short: "MCP server over stdio (memory_remember/recall/show/forget/entities)",
		Long: `Serve the store over the Model Context Protocol on stdio. Mount from
any MCP client, e.g. Claude Code:

  claude mcp add amber -- amber serve

Same core, same scoping, same trust policy as the CLI: tool/web-origin
writes are quarantined until reviewed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			e.Store.BumpCounter("serve")
			return mcpserver.Run(ctx, mcpserver.Deps{Writer: e.Writer, Store: e.Store, Scope: string(e.Scope)})
		},
	}
	return c
}
