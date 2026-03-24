package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/OpenCortex-Labs/logr/internal/filter"
	"github.com/OpenCortex-Labs/logr/internal/source"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "logr",
		Short:         "Terminal-native log intelligence for developers",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.logr.yaml)")
	root.PersistentFlags().String("output", "pretty", "output format: pretty|json|logfmt|table")
	root.PersistentFlags().String("level", "", "min level: debug|info|warn|error")
	root.PersistentFlags().String("grep", "", "filter lines by string or regex")
	root.PersistentFlags().String("last", "", "show logs from last duration e.g. 1h, 30m")
	root.PersistentFlags().String("since", "", "show logs since RFC3339 timestamp")
	root.PersistentFlags().Bool("no-color", false, "disable color output")
	root.PersistentFlags().StringSlice("field", nil, "filter by structured field key=value (repeatable)")
	root.PersistentFlags().Int("sample", 1, "emit every Nth matching entry (1 = all)")

	if err := viper.BindPFlags(root.PersistentFlags()); err != nil {
		fmt.Fprintf(os.Stderr, "logr: failed to bind flags: %v\n", err)
	}
	cobra.OnInitialize(initConfig)

	root.AddCommand(
		newWatchCmd(),
		newTailCmd(),
		newQueryCmd(),
		newStatsCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)

	return root
}

func initConfig() {
	InitConfig(cfgFile)
}

// InitConfig loads config from path (empty = ~/.logr.yaml). Call from run package when building logger from config.
func InitConfig(configPath string) {
	if configPath != "" {
		viper.SetConfigFile(configPath)
	} else {
		home, _ := os.UserHomeDir()
		viper.AddConfigPath(home)
		viper.SetConfigName(".logr")
		viper.SetConfigType("yaml")
	}
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err != nil {
		if err != nil {
			fmt.Fprintf(os.Stderr, "logr: config error: %v\n", err)
		}
	}
}

// ── Source flags ──────────────────────────────────────────────────────────────

type sourceFlags struct {
	docker     bool
	services   []string
	kube       bool
	namespace  string
	labels     string
	podPrefix  string
	container  string
	kubeconfig string
	files      []string
	stdin      bool
}

func addSourceFlags(cmd *cobra.Command, f *sourceFlags) {
	cmd.Flags().BoolVar(&f.docker, "docker", false, "tail Docker containers")
	cmd.Flags().StringSliceVar(&f.services, "service", nil, "docker-compose services (api,worker)")
	cmd.Flags().BoolVar(&f.kube, "kube", false, "tail Kubernetes pods")
	cmd.Flags().StringVar(&f.namespace, "namespace", "default", "kubernetes namespace ('all' for all)")
	cmd.Flags().StringVar(&f.labels, "label", "", "kubernetes label selector (app=api)")
	cmd.Flags().StringVar(&f.podPrefix, "pod-prefix", "", "filter pods by name prefix")
	cmd.Flags().StringVar(&f.container, "container", "", "specific container name")
	cmd.Flags().StringVar(&f.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	cmd.Flags().StringSliceVar(&f.files, "file", nil, "log file paths or globs")
	cmd.Flags().BoolVar(&f.stdin, "stdin", false, "read from stdin")
}

func resolveSources(f *sourceFlags) ([]source.Source, error) {
	var sources []source.Source

	if f.docker {
		s, err := source.NewDockerSource(f.services)
		if err != nil {
			return nil, fmt.Errorf("docker: %w", err)
		}
		sources = append(sources, s)
	}

	if f.kube {
		s, err := source.NewKubeSource(source.KubeOptions{
			Namespace:  f.namespace,
			Labels:     f.labels,
			PodPrefix:  f.podPrefix,
			Container:  f.container,
			Kubeconfig: f.kubeconfig,
		})
		if err != nil {
			return nil, fmt.Errorf("kubernetes: %w", err)
		}
		sources = append(sources, s)
	}

	if len(f.files) > 0 {
		s, err := source.NewFileSource(f.files)
		if err != nil {
			return nil, fmt.Errorf("file: %w", err)
		}
		sources = append(sources, s)
	}

	isPiped := !isTerminal(os.Stdin)
	if f.stdin || (len(sources) == 0 && isPiped) {
		sources = append(sources, source.NewStdinSource())
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no source specified — use --docker, --kube, --file, or pipe via stdin")
	}

	return sources, nil
}

func buildFilter(cmd *cobra.Command) (*filter.Filter, error) {
	level, _ := cmd.Flags().GetString("level")
	grepStr, _ := cmd.Flags().GetString("grep")
	lastStr, _ := cmd.Flags().GetString("last")
	sinceStr, _ := cmd.Flags().GetString("since")
	fieldPairs, _ := cmd.Flags().GetStringSlice("field")
	sample, _ := cmd.Flags().GetInt("sample")

	f := &filter.Filter{
		Level:  strings.ToLower(level),
		Sample: sample,
	}

	if grepStr != "" {
		re, err := regexp.Compile(grepStr)
		if err != nil {
			return nil, fmt.Errorf("invalid --grep regex: %w", err)
		}
		f.Grep = re
	}

	if lastStr != "" {
		d, err := time.ParseDuration(lastStr)
		if err != nil {
			return nil, fmt.Errorf("invalid --last %q (use: 1h, 30m): %w", lastStr, err)
		}
		f.Since = time.Now().Add(-d)
	}

	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return nil, fmt.Errorf("invalid --since %q (RFC3339): %w", sinceStr, err)
		}
		f.Since = t
	}

	// Parse --field key=value pairs.
	if len(fieldPairs) > 0 {
		f.Fields = make(map[string]string, len(fieldPairs))
		for _, pair := range fieldPairs {
			k, v, ok := strings.Cut(pair, "=")
			if !ok || k == "" {
				return nil, fmt.Errorf("invalid --field %q: expected key=value", pair)
			}
			f.Fields[k] = v
		}
	}

	return f, nil
}

func closeAll(sources []source.Source) {
	for _, s := range sources {
		s.Close()
	}
}

func isTerminal(f *os.File) bool {
	stat, _ := f.Stat()
	return (stat.Mode() & os.ModeCharDevice) != 0
}
