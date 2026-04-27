package main

import (
	"encoding/json"
	"fmt"
	"mcp-indexer/internal/app"
	imcp "mcp-indexer/internal/mcp"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mcp-indexer",
		Short: "Local multi-service code indexer with MCP interface",
	}
	root.AddCommand(
		serveCmd(),
		listCmd(),
		addCmd(),
		syncCmd(),
		searchCmd(),
		peekCmd(),
		walkCmd(),
		codeCmd(),
		fileCmd(),
		statsCmd(),
		vizCmd(),
	)
	return root
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.New()
			if err != nil {
				return err
			}
			defer a.Close()
			srv := server.NewMCPServer("mcp-indexer", "0.1.0")
			imcp.Register(srv, a)
			return server.NewStdioServer(srv).Listen(cmd.Context(), os.Stdin, os.Stdout)
		},
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered services",
		RunE: withApp(func(a *app.App, _ *cobra.Command, _ []string) error {
			services := a.ListServices()
			if len(services) == 0 {
				fmt.Println("(no services registered)")
				return nil
			}
			for _, s := range services {
				fmt.Printf("%-20s  %s\n", s.ID, s.Root)
			}
			return nil
		}),
	}
}

func addCmd() *cobra.Command {
	var svcID, description, mainEntitiesRaw string
	cmd := &cobra.Command{
		Use:   "add <rootAbs>",
		Short: "Register a new service (and run initial index)",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			var mainEntities []string
			if mainEntitiesRaw != "" {
				for _, e := range strings.Split(mainEntitiesRaw, ",") {
					if s := strings.TrimSpace(e); s != "" {
						mainEntities = append(mainEntities, s)
					}
				}
			}
			id, err := a.AddService(args[0], svcID, description, mainEntities)
			if err != nil {
				return err
			}
			fmt.Printf("registered: %s\n", id)
			return nil
		}),
	}
	cmd.Flags().StringVar(&svcID, "id", "", "service ID (default: dir name)")
	cmd.Flags().StringVar(&description, "description", "", "short description")
	cmd.Flags().StringVar(&mainEntitiesRaw, "entities", "", "comma-separated main domain entities")
	return cmd
}

func syncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync <serviceId>",
		Short: "Re-index a service from scratch",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			if err := a.Sync(args[0]); err != nil {
				return err
			}
			fmt.Printf("synced: %s\n", args[0])
			return nil
		}),
	}
}

func searchCmd() *cobra.Command {
	var kind string
	var limit int
	cmd := &cobra.Command{
		Use:   "search <serviceId> <query>",
		Short: "FTS5 search for methods/objects/files",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.Search(args[0], args[1], kind, limit)
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
	cmd.Flags().StringVar(&kind, "kind", "", "method | object | file (empty = all)")
	cmd.Flags().IntVar(&limit, "limit", 10, "max hits")
	return cmd
}

func peekCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "peek <serviceId> <id>",
		Short: "Compact summary of node/file by short or canonical id",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.Peek(args[0], args[1])
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
}

func walkCmd() *cobra.Command {
	var dir string
	var limit, offset int
	cmd := &cobra.Command{
		Use:   "walk <serviceId> <id> <edge>",
		Short: "Walk graph edges around id. edge: calls|inherits|imports|defines",
		Args:  cobra.ExactArgs(3),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.Walk(args[0], args[1], args[2], dir, limit, offset)
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
	cmd.Flags().StringVar(&dir, "dir", "both", "in | out | both")
	cmd.Flags().IntVar(&limit, "limit", 20, "max items")
	cmd.Flags().IntVar(&offset, "offset", 0, "offset")
	return cmd
}

func codeCmd() *cobra.Command {
	var ctx int
	cmd := &cobra.Command{
		Use:   "code <serviceId> <id>",
		Short: "Source code of a node (method/object)",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.Code(args[0], args[1], ctx)
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
	cmd.Flags().IntVar(&ctx, "ctx", 0, "extra lines around range")
	return cmd
}

func fileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "file <serviceId> <path>",
		Short: "File overview: imports, objects, top-level methods",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.File(args[0], args[1])
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
}

func statsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats <serviceId>",
		Short: "Index counters",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.Stats(args[0])
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
}

type appFn func(a *app.App, cmd *cobra.Command, args []string) error

func withApp(fn appFn) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		a, err := app.New()
		if err != nil {
			return err
		}
		defer a.Close()
		return fn(a, cmd, args)
	}
}

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}