package run

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/OpenCortex-Labs/logr/internal/cli"
	"github.com/OpenCortex-Labs/logr/logger"
	"github.com/spf13/viper"
)

const defaultWatchLogFile = "./logs/app.log"

// App is the logr runner. Build it with New(), configure with chained methods, then call .Start().
//
//	run.New(myApp.Run).
//	    LogFile("./logs/api.log").
//	    OnReady(func() { fmt.Println("ready!") }).
//	    Start()
type App struct {
	runFunc      func(ctx context.Context) error
	watchLogFile string
	onReady      func()
}

// New creates a new App with the given run function as the app entrypoint.
// Call .Start() to launch — or chain options first.
func New(runFunc func(ctx context.Context) error) *App {
	return &App{runFunc: runFunc}
}

// LogFile sets the log file path the TUI will tail in watch mode.
// Defaults to ./logs/app.log if not set.
func (a *App) LogFile(path string) *App {
	a.watchLogFile = path
	return a
}

// OnReady registers a callback invoked once the TUI is ready and tailing.
// If not set and a run function was provided, it is started automatically.
func (a *App) OnReady(fn func()) *App {
	a.onReady = fn
	return a
}

// Start runs the app. It sets up signal handling (Ctrl+C / SIGTERM),
// wires the logr CLI, and blocks until the app exits.
// Call this as the last line of main().

func (a *App) Start() {
	os.Exit(a.start())
}

func (a *App) start() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := a.run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// StartContext is like Start but uses the provided context.
// Useful in tests or when you manage the context yourself.
func (a *App) StartContext(ctx context.Context) error {
	return a.run(ctx, os.Args[1:])
}

func (a *App) run(ctx context.Context, args []string) error {
	// No args: run the app directly without invoking the CLI.
	if len(args) == 0 && a.runFunc != nil {
		return a.runFunc(ctx)
	}

	if len(args) >= 1 && args[0] == "watch" {
		logFile := a.watchLogFile
		if logFile == "" {
			logFile = defaultWatchLogFile
		}
		onReady := a.onReady
		if onReady == nil && a.runFunc != nil {
			onReady = func() {
				go func() {
					if err := a.runFunc(ctx); err != nil {
						fmt.Fprintf(os.Stderr, "logr: app error: %v\n", err)
					}
				}()
			}
		}
		return runWatchWithFile(ctx, args, logFile, onReady)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
		return err
	}
	if a.runFunc != nil && (len(args) == 0 || args[0] != "watch") {
		return a.runFunc(ctx)
	}
	return nil
}

// ── Watch internals ───────────────────────────────────────────────────────────

func runWatchWithFile(ctx context.Context, args []string, logFilePath string, onReady func()) error {
	if err := ensureLogFile(logFilePath); err != nil {
		return fmt.Errorf("ensure log file: %w", err)
	}

	// Strip any existing --file flag from args before appending ours,
	// so a caller who already passed --file doesn't produce duplicate flags.
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--file" || args[i] == "-f" {
			i++ // skip the value too
			continue
		}
		filtered = append(filtered, args[i])
	}
	watchArgs := append([]string{}, filtered...)
	watchArgs = append(watchArgs, "--file", logFilePath)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd := cli.NewRootCmd()
		cmd.SetArgs(watchArgs)
		_ = cmd.ExecuteContext(ctx)
	}()

	// Let the TUI open the file before we start writing to it.
	time.Sleep(300 * time.Millisecond)

	prev := logger.Default
	logger.SetDefault(defaultLoggerFromConfig(logFilePath))
	defer logger.SetDefault(prev)

	if onReady != nil {
		onReady()
	}
	wg.Wait()
	return nil
}

func defaultLoggerFromConfig(watchLogFile string) *logger.Logger {
	cli.InitConfig("")
	var configs []logger.WriterConfig
	if err := viper.UnmarshalKey("loggers", &configs); err == nil && len(configs) > 0 {
		hasWatchFile := false
		for _, c := range configs {
			if c.Type == "file" && c.Path == watchLogFile {
				hasWatchFile = true
				break
			}
		}
		if !hasWatchFile {
			configs = append(configs, logger.WriterConfig{Type: "file", Path: watchLogFile})
		}
		if l, err := logger.NewLoggerFromConfig(configs); err == nil {
			return l
		}
	}
	f, err := os.OpenFile(watchLogFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return logger.NewLogger(os.Stdout)
	}
	return logger.NewLogger(f)
}

func ensureLogFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		_ = f.Close()
	}
	return nil
}
