package usecase_test

import "log/slog"

// discardLogger はテスト中のログ出力を捨てる。
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
