// Command manager は aso-bell の manager バイナリ。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	// distroless イメージには tzdata が無く、ワークスペースのタイムゾーンを解決できないため埋め込む(docs/12 §4)。
	_ "time/tzdata"

	"github.com/bear-san/aso-bell/internal/manager/app"
	"github.com/bear-san/aso-bell/internal/manager/config"
	"github.com/bear-san/aso-bell/internal/shared/logging"
	"github.com/bear-san/aso-bell/internal/shared/version"
)

const (
	exitError = 1
	exitUsage = 2
)

const usage = "usage: manager <serve|migrate|healthcheck|version>"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)

		return exitUsage
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(os.Stdout, version.String("manager"))

		return 0
	case "serve", "migrate", "healthcheck":
		return execute(args[0])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n%s\n", args[0], usage)

		return exitUsage
	}
}

func execute(command string) int {
	cfg, err := config.Load(nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return exitError
	}

	logger, err := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return exitError
	}

	// SIGTERM は Compose と Kubernetes の停止手順。受け取った時点で ctx を閉じ、順序どおりに止める。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err = dispatch(ctx, command, cfg, logger); err != nil {
		logger.ErrorContext(ctx, "manager failed", "command", command, "error", err)

		return exitError
	}

	return 0
}

func dispatch(ctx context.Context, command string, cfg *config.Config, logger *slog.Logger) error {
	switch command {
	case "migrate":
		return app.Migrate(ctx, cfg, logger)
	case "healthcheck":
		return app.Healthcheck(ctx, cfg)
	default:
		return serve(ctx, cfg, logger)
	}
}

func serve(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	manager, err := app.New(ctx, cfg, logger)
	if err != nil {
		return err
	}

	logger.InfoContext(ctx, "manager starting", "version", version.Version)

	return manager.Run(ctx)
}
