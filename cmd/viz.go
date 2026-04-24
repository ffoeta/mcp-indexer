package main

import (
	"fmt"
	"mcp-indexer/internal/app"
	"mcp-indexer/internal/ui"
	"strings"

	"github.com/spf13/cobra"
)

func vizCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "viz <serviceId>",
		Short: "Start interactive graph visualization in browser",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			svcID := args[0]
			if _, ok := a.Registry.Get(svcID); !ok {
				ids := a.ListServicesSorted()
				if len(ids) == 0 {
					return fmt.Errorf("service %q not found (no services registered)", svcID)
				}
				return fmt.Errorf("service %q not found\navailable: %s", svcID, strings.Join(ids, ", "))
			}
			return ui.Serve(a, svcID, port)
		}),
	}
	cmd.Flags().IntVar(&port, "port", 8080, "HTTP port for the visualization server")
	return cmd
}
