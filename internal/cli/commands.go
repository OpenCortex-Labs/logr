package cli

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/Mihir99-mk/logr/internal/fanin"
	"github.com/Mihir99-mk/logr/internal/source"
	"github.com/Mihir99-mk/logr/internal/tui"
	"github.com/Mihir99-mk/logr/pkg/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ── watch ─────────────────────────────────────────────────────────────────────

func newWatchCmd() *cobra.Command {
	var sf sourceFlags
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Live log tail with interactive TUI",
		Example: `  logr watch --docker
  logr watch --docker --service api,worker
  logr watch --kube --namespace production --label app=api
  logr watch --file ./logs/*.log
  kubectl logs -f pod/api-xyz | logr watch`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sources, err := resolveSources(&sf)
			if err != nil {
				return err
			}
			defer closeAll(sources)
			f, err := buildFilter(cmd)
			if err != nil {
				return err
			}
			return tui.Run(cmd.Context(), sources, f)
		},
	}
	addSourceFlags(cmd, &sf)
	return cmd
}

// ── tail ──────────────────────────────────────────────────────────────────────

func newTailCmd() *cobra.Command {
	var sf sourceFlags
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Live log tail — plain text, pipe-friendly",
		Example: `  logr tail --docker --level error
  logr tail --file app.log --grep "user_id=42"
  logr tail --kube --namespace prod | jq '.message'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sources, err := resolveSources(&sf)
			if err != nil {
				return err
			}
			defer closeAll(sources)
			f, err := buildFilter(cmd)
			if err != nil {
				return err
			}
			outputFmt, _ := cmd.Flags().GetString("output")
			noColor, _ := cmd.Flags().GetBool("no-color")
			printer := output.NewPrinter(outputFmt, !noColor)
			merged := fanin.Merge(cmd.Context(), sources)
			for entry := range merged {
				if f.Match(entry) {
					printer.Print(entry)
				}
			}
			return nil
		},
	}
	addSourceFlags(cmd, &sf)
	return cmd
}

// ── query ─────────────────────────────────────────────────────────────────────

func newQueryCmd() *cobra.Command {
	var sf sourceFlags
	var limit int
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Search historical logs",
		Example: `  logr query --file app.log --last 1h --level error
  logr query --docker --grep "panic" --output table`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sources, err := resolveSources(&sf)
			if err != nil {
				return err
			}
			defer closeAll(sources)
			f, err := buildFilter(cmd)
			if err != nil {
				return err
			}
			outputFmt, _ := cmd.Flags().GetString("output")
			noColor, _ := cmd.Flags().GetBool("no-color")
			printer := output.NewPrinter(outputFmt, !noColor)
			merged := fanin.Merge(cmd.Context(), sources)
			count := 0
			for entry := range merged {
				if f.Match(entry) {
					printer.Print(entry)
					count++
					if limit > 0 && count >= limit {
						return nil
					}
				}
			}
			return nil
		},
	}
	addSourceFlags(cmd, &sf)
	cmd.Flags().IntVar(&limit, "limit", 0, "max results (0 = unlimited)")
	return cmd
}

// ── stats ─────────────────────────────────────────────────────────────────────

func newStatsCmd() *cobra.Command {
	var sf sourceFlags
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show error rate summary across services",
		Example: `  logr stats --docker --last 1h
  logr stats --kube --namespace prod --last 30m`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sources, err := resolveSources(&sf)
			if err != nil {
				return err
			}
			defer closeAll(sources)
			f, err := buildFilter(cmd)
			if err != nil {
				return err
			}

			type stats struct{ total, errors, warns int }
			counts := map[string]*stats{}
			merged := fanin.Merge(cmd.Context(), sources)

			for entry := range merged {
				if !f.Match(entry) {
					continue
				}
				if _, ok := counts[entry.Service]; !ok {
					counts[entry.Service] = &stats{}
				}
				s := counts[entry.Service]
				s.total++
				switch entry.Level {
				case source.LevelError:
					s.errors++
				case source.LevelWarn:
					s.warns++
				}
			}

			services := make([]string, 0, len(counts))
			for svc := range counts {
				services = append(services, svc)
			}
			sort.Slice(services, func(i, j int) bool {
				si, sj := counts[services[i]], counts[services[j]]
				ri := float64(si.errors) / float64(max1(si.total, 1))
				rj := float64(sj.errors) / float64(max1(sj.total, 1))
				return ri > rj
			})

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SERVICE\tTOTAL\tERRORS\tWARNS\tERROR RATE")
			fmt.Fprintln(w, "-------\t-----\t------\t-----\t----------")
			for _, svc := range services {
				s := counts[svc]
				rate := float64(s.errors) / float64(max1(s.total, 1)) * 100
				flag := "✅"
				if rate > 5 {
					flag = "🔴"
				} else if rate > 1 {
					flag = "⚠️ "
				}
				fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%.2f%% %s\n",
					svc, s.total, s.errors, s.warns, rate, flag)
			}
			w.Flush()
			return nil
		},
	}
	addSourceFlags(cmd, &sf)
	return cmd
}

// ── config ────────────────────────────────────────────────────────────────────

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage logr configuration",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show current config",
			Run: func(cmd *cobra.Command, args []string) {
				fmt.Printf("Config file: %s\n\n", viper.ConfigFileUsed())
				for k, v := range viper.AllSettings() {
					fmt.Printf("  %s: %v\n", k, v)
				}
			},
		},
		&cobra.Command{
			Use:   "set [key] [value]",
			Short: "Set a config value",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				viper.Set(args[0], args[1])
				return viper.WriteConfig()
			},
		},
	)
	return cmd
}

// ── version ───────────────────────────────────────────────────────────────────

// These variables are injected at build time via -ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("logr %s (commit %s, built %s)\n", Version, Commit, BuildDate)
		},
	}
}

func max1(a, b int) int {
	if a > b {
		return a
	}
	return b
}
