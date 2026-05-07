package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/client"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

// NewPluginCmd creates the plugin command group with injected dependencies.
func NewPluginCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	if w == nil {
		w = os.Stdout
	}
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage installed plugins",
	}
	cmd.AddCommand(
		newPluginListCmd(c, w),
		newPluginInfoCmd(c, w),
		newPluginInstallCmd(c, w),
		newPluginRemoveCmd(c, w),
		newPluginReloadCmd(c, w),
	)
	return cmd
}

func newPluginListCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plugins, err := c.PluginList(context.Background())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tVERSION\tREGISTRY\tSTATUS\tINSTALLED")
			for _, p := range plugins {
				installed := p.InstalledAt
				if len(installed) >= 10 {
					installed = installed[:10]
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					p.Name, p.Version, orDash(p.Registry), p.Status, installed)
			}
			return tw.Flush()
		},
	}
}

// resolvePluginID looks up a plugin by name and returns its ID.
func resolvePluginID(ctx context.Context, c client.OrchestratorClient, name string) (string, error) {
	plugins, err := c.PluginList(ctx)
	if err != nil {
		return "", fmt.Errorf("list plugins: %w", err)
	}
	for _, p := range plugins {
		if p.Name == name {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("plugin %q not found", name)
}

func newPluginInfoCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show detailed information about a plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := resolvePluginID(context.Background(), c, args[0])
			if err != nil {
				return err
			}
			p, err := c.PluginGet(context.Background(), id)
			if err != nil {
				return err
			}
			printPluginDetail(w, p)
			return nil
		},
	}
}

func printPluginDetail(w io.Writer, p domain.Plugin) {
	fmt.Fprintf(w, "Name:        %s\n", p.Name)
	fmt.Fprintf(w, "Version:     %s\n", p.Version)
	fmt.Fprintf(w, "Status:      %s\n", p.Status)
	fmt.Fprintf(w, "Registry:    %s\n", orDash(p.Registry))
	fmt.Fprintf(w, "Installed:   %s\n", p.InstalledAt)
	fmt.Fprintf(w, "Entrypoint:  %s\n", orDash(p.Manifest.Entrypoint))
	if len(p.Manifest.Provides.Tools) > 0 {
		fmt.Fprintln(w, "Tools:")
		for _, t := range p.Manifest.Provides.Tools {
			fmt.Fprintf(w, "  - %s: %s\n", t.Name, t.Description)
		}
	}
	perms := p.Manifest.Permissions
	if perms.Filesystem || len(perms.Network) > 0 || len(perms.EnvVars) > 0 {
		fmt.Fprintln(w, "Permissions:")
		if perms.Filesystem {
			fmt.Fprintln(w, "  filesystem: true")
		}
		if len(perms.Network) > 0 {
			fmt.Fprintf(w, "  network: %s\n", strings.Join(perms.Network, ", "))
		}
		if len(perms.EnvVars) > 0 {
			fmt.Fprintf(w, "  env_vars: %s\n", strings.Join(perms.EnvVars, ", "))
		}
	}
}

func newPluginInstallCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	var registry, version string
	cmd := &cobra.Command{
		Use:   "install <name>",
		Short: "Install a plugin from a registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := domain.InstallPluginRequest{
				Name:     args[0],
				Registry: registry,
				Version:  version,
			}
			p, err := c.PluginInstall(context.Background(), req)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Plugin %q installation initiated (status: %s, id: %s)\n", p.Name, p.Status, p.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&registry, "registry", "", "registry name to install from")
	cmd.Flags().StringVar(&version, "version", "", "version to install (default: latest)")
	return cmd
}

func newPluginRemoveCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an installed plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := resolvePluginID(context.Background(), c, args[0])
			if err != nil {
				return err
			}
			if err := c.PluginRemove(context.Background(), id); err != nil {
				return err
			}
			fmt.Fprintf(w, "Plugin %q removed\n", args[0])
			return nil
		},
	}
}

func newPluginReloadCmd(c client.OrchestratorClient, w io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "reload <name>",
		Short: "Reload a plugin's manifest from disk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := resolvePluginID(context.Background(), c, args[0])
			if err != nil {
				return err
			}
			if err := c.PluginReload(context.Background(), id); err != nil {
				return err
			}
			fmt.Fprintf(w, "Plugin %q reloaded\n", args[0])
			return nil
		},
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
