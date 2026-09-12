// Command provider-discord は aso-bell の Discord Provider バイナリ。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	// distroless イメージには tzdata が無いため埋め込む(docs/12 §4)。
	_ "time/tzdata"

	"github.com/bear-san/aso-bell/internal/provider/app"
	"github.com/bear-san/aso-bell/internal/provider/config"
	"github.com/bear-san/aso-bell/internal/provider/discord"
	"github.com/bear-san/aso-bell/internal/shared/logging"
	"github.com/bear-san/aso-bell/internal/shared/version"
)

const (
	exitError = 1
	exitUsage = 2
)

const usage = "usage: provider-discord [serve|healthcheck|version] [--manager-addr ADDR] ..."

func main() {
	os.Exit(run(os.Args[1:]))
}

// run はサブコマンドを解釈する。省略時は serve として扱う(Compose の ENTRYPOINT が引数だけを渡すため)。
func run(args []string) int {
	command := "serve"
	if len(args) > 0 && !isFlag(args[0]) {
		command, args = args[0], args[1:]
	}

	if command == "version" {
		fmt.Fprintln(os.Stdout, version.String("provider-discord"))

		return 0
	}

	if command != "serve" && command != "healthcheck" {
		fmt.Fprintf(os.Stderr, "unknown command %q\n%s\n", command, usage)

		return exitUsage
	}

	return execute(command, args)
}

func isFlag(arg string) bool { return len(arg) > 0 && arg[0] == '-' }

func execute(command string, args []string) int {
	cfg, err := config.LoadDiscord(nil, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return exitError
	}

	logger, err := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return exitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err = dispatch(ctx, command, cfg, logger); err != nil {
		logger.ErrorContext(ctx, "provider failed", "command", command, "error", err)

		return exitError
	}

	return 0
}

func dispatch(ctx context.Context, command string, cfg *config.Discord, logger *slog.Logger) error {
	if command == "healthcheck" {
		return app.Healthcheck(ctx, &cfg.Common)
	}

	return serve(ctx, cfg, logger)
}

func serve(ctx context.Context, cfg *config.Discord, logger *slog.Logger) error {
	chat, err := discord.New(discord.Config{
		Token:         cfg.BotToken,
		ApplicationID: cfg.ApplicationID,
		CommandName:   cfg.CommandName,
		Logger:        logger,
	})
	if err != nil {
		return err
	}

	provider, err := app.New(ctx, &cfg.Common, chat, logger, version.Version)
	if err != nil {
		return err
	}

	logger.InfoContext(ctx, "provider starting", "version", version.Version, "kind", "discord")

	return provider.Run(ctx)
}
