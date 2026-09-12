package usecase

import "errors"

// ProviderPort が返す Provider 由来のエラー(docs/16-grpc.md §3)。
// 実装は gRPC の ErrorInfo.reason をこれらのセンチネルへ変換し、ユースケースは errors.Is で扱いを決める。
var (
	// ErrNameTaken はチャンネル名の重複。呼び出し元はサフィックスを付けて再試行する。
	ErrNameTaken = errors.New("channel name is already taken")
	// ErrBotPermission は Bot の権限不足。再試行しても解消しない。
	ErrBotPermission = errors.New("bot lacks permission")
	// ErrRateLimited はチャットツール側のレート制限。ジョブは backoff して再実行する。
	ErrRateLimited = errors.New("rate limited by chat platform")
	// ErrPinLimit はピン留めの上限超過。告知の投稿自体は成功しているため無視して継続する。
	ErrPinLimit = errors.New("pin limit reached")
	// ErrDMBlocked はユーザーが DM を受け取れない状態。機微な内容は公開投稿で代替しない。
	ErrDMBlocked = errors.New("direct message blocked")
	// ErrUnsupported は Provider が対応していない操作。Capabilities で事前に回避すべきもの。
	ErrUnsupported = errors.New("unsupported by provider")
)
