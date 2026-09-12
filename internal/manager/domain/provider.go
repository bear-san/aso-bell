package domain

import "time"

// ProviderKind は対になる Provider の種別。Manager 1 に対して 1 種別のみ(ADR 0008)。
type ProviderKind string

// Provider の種別。
const (
	ProviderKindSlack   ProviderKind = "slack"
	ProviderKindDiscord ProviderKind = "discord"
)

// ProviderStatus は Provider の死活状態。
type ProviderStatus string

// Provider の死活状態。
const (
	ProviderStatusOnline  ProviderStatus = "online"
	ProviderStatusOffline ProviderStatus = "offline"
)

// DefaultProviderOfflineAfterFailures は offline と判定するまでの GetInfo 連続失敗回数(docs/04 §2.1)。
const DefaultProviderOfflineAfterFailures = 3

// Capabilities は Provider が対応する応答手段。Manager が返信方法を決める際に参照する。
type Capabilities struct {
	Forms         bool
	Ephemeral     bool
	DirectMessage bool
}

// ProviderState は対になる Provider の観測状態。`meta` コレクションの単一ドキュメントとして保持する。
type ProviderState struct {
	Kind         ProviderKind
	Address      string
	Capabilities Capabilities
	Version      string
	BotUserID    string
	Connected    bool
	Status       ProviderStatus
	Failures     int
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
	UpdatedAt    time.Time
}

// Valid は既知の Provider 種別かを返す。
func (k ProviderKind) Valid() bool {
	switch k {
	case ProviderKindSlack, ProviderKindDiscord:
		return true
	default:
		return false
	}
}

// DecideProviderStatus は接続状態と GetInfo の連続失敗回数から死活状態を決める。
// チャットツールへ未接続(connected=false)の場合は失敗回数によらず offline とする。
func DecideProviderStatus(connected bool, failures, threshold int) ProviderStatus {
	if !connected || failures >= threshold {
		return ProviderStatusOffline
	}

	return ProviderStatusOnline
}
