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
		prepareSyncCmd(),
		doSyncCmd(),
		searchCmd(),
		fileContextCmd(),
		neighborsCmd(),
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
			ids := a.ListServicesSorted()
			if len(ids) == 0 {
				fmt.Println("(no services registered)")
				return nil
			}
			for _, id := range ids {
				entry, _ := a.Registry.Get(id)
				fmt.Printf("%-20s  %s\n", id, entry.RootAbs)
			}
			return nil
		}),
	}
}

func addCmd() *cobra.Command {
	var svcID, name string
	cmd := &cobra.Command{
		Use:   "add <rootAbs>",
		Short: "Register a new service",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			id, err := a.AddService(args[0], svcID, name)
			if err != nil {
				return err
			}
			fmt.Printf("registered: %s\n", id)
			return nil
		}),
	}
	cmd.Flags().StringVar(&svcID, "id", "", "service ID (default: dir name)")
	cmd.Flags().StringVar(&name, "name", "", "human-readable name")
	return cmd
}

func prepareSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prepare-sync <serviceId>",
		Short: "Show what would change (no writes)",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.PrepareSync(args[0])
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
}

func doSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "do-sync <serviceId>",
		Short: "Hash diff + apply to index",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.DoSync(args[0])
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
}

func searchCmd() *cobra.Command {
	var symN, fileN, modN int
	cmd := &cobra.Command{
		Use:   "search <serviceId> <query>",
		Short: "Search symbols/files/modules",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			limits := app.SearchLimits{Sym: symN, File: fileN, Mod: modN}
			res, err := a.Search(args[0], args[1], limits)
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
	cmd.Flags().IntVar(&symN, "sym", 20, "max symbols (0=skip)")
	cmd.Flags().IntVar(&fileN, "file", 10, "max files (0=skip)")
	cmd.Flags().IntVar(&modN, "mod", 5, "max modules (0=skip)")
	return cmd
}

func fileContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "file-context <serviceId> <key>",
		Short: "Get file context (module, imports, symbols)",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			res, err := a.GetFileContext(args[0], args[1])
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
}

func neighborsCmd() *cobra.Command {
	var depth int
	var edgeTypes string
	cmd := &cobra.Command{
		Use:   "neighbors <serviceId> <nodeId>",
		Short: "BFS neighbors in the graph",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(a *app.App, _ *cobra.Command, args []string) error {
			var et []string
			if edgeTypes != "" {
				et = strings.Split(edgeTypes, ",")
			}
			res, err := a.GetNeighbors(args[0], args[1], depth, et)
			if err != nil {
				return err
			}
			return printJSON(res)
		}),
	}
	cmd.Flags().IntVar(&depth, "depth", 2, "BFS depth")
	cmd.Flags().StringVar(&edgeTypes, "edge-types", "", "comma-separated edge types")
	return cmd
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
