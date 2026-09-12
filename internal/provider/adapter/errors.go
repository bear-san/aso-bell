package adapter

import "errors"

// Adapter が返すセンチネルエラー(docs/06-provider.md §2.1)。
// Runtime がこれらを gRPC ステータスと ErrorInfo.reason へ変換する(docs/16-grpc.md §3)。
var (
	// ErrNotFound はチャンネル・メッセージ・ユーザーが見つからない。
	ErrNotFound = errors.New("not found")
	// ErrAlreadyMember は既にチャンネルのメンバーである。
	ErrAlreadyMember = errors.New("already a member")
	// ErrNotMember はチャンネルのメンバーではない。
	ErrNotMember = errors.New("not a member")
	// ErrNameTaken はチャンネル名が既に使われている。
	ErrNameTaken = errors.New("channel name taken")
	// ErrPermission は Bot の権限・スコープが足りない。
	ErrPermission = errors.New("missing permission")
	// ErrRateLimited はレート制限に達した。
	ErrRateLimited = errors.New("rate limited")
	// ErrPinLimit はピン留めの上限に達した。
	ErrPinLimit = errors.New("pin limit reached")
	// ErrUnavailable はチャットツールに接続できない。
	ErrUnavailable = errors.New("platform unavailable")
	// ErrUnsupported はこの Adapter が対応していない操作。
	ErrUnsupported = errors.New("unsupported")
	// ErrDMBlocked はユーザーが DM を受け取れない設定にしている。
	ErrDMBlocked = errors.New("direct message blocked")
)
