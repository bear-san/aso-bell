package providerclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// 状態監視の既定値(docs/16-grpc.md §6.2)。
const (
	DefaultPollInterval = 15 * time.Second
	DefaultMinBackoff   = time.Second
	DefaultMaxBackoff   = 30 * time.Second
)

// backoffFactor は再試行間隔を指数的に伸ばす倍率。
const backoffFactor = 2

// MonitorConfig は Monitor の依存と設定。
type MonitorConfig struct {
	Port    usecase.ProviderPort
	Meta    usecase.MetaRepository
	Clock   usecase.Clock
	Logger  *slog.Logger
	Address string
	// Interval は GetInfo の呼び出し間隔。
	Interval time.Duration
	// OfflineAfterFailures は offline と判定するまでの連続失敗回数。
	OfflineAfterFailures int
	// MinBackoff / MaxBackoff は Bootstrap の再試行間隔。
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

// Monitor は GetInfo を定期的に呼んで Provider の観測状態を meta へ反映する(docs/16-grpc.md §6.2)。
// 3 回連続の失敗、またはチャットツール未接続で offline とみなす。
type Monitor struct {
	cfg MonitorConfig

	mu    sync.RWMutex
	state domain.ProviderState
}

// NewMonitor は既定値を補った Monitor を作る。
func NewMonitor(cfg MonitorConfig) *Monitor {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	if cfg.Clock == nil {
		cfg.Clock = usecase.SystemClock{}
	}

	if cfg.Interval <= 0 {
		cfg.Interval = DefaultPollInterval
	}

	if cfg.OfflineAfterFailures <= 0 {
		cfg.OfflineAfterFailures = domain.DefaultProviderOfflineAfterFailures
	}

	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = DefaultMinBackoff
	}

	if cfg.MaxBackoff < cfg.MinBackoff {
		cfg.MaxBackoff = max(DefaultMaxBackoff, cfg.MinBackoff)
	}

	return &Monitor{cfg: cfg, state: domain.ProviderState{Address: cfg.Address, Status: domain.ProviderStatusOffline}}
}

// Bootstrap は GetInfo が成功するまで backoff しながら再試行し、記録済みの種別と照合する。
// 別の種別の Provider に接続している場合は domain.ErrProviderKindMismatch を返し、起動を中止させる。
func (m *Monitor) Bootstrap(ctx context.Context) error {
	stored, err := m.cfg.Meta.GetProvider(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("load provider state: %w", err)
	}

	if stored != nil {
		m.mu.Lock()
		m.state = *stored
		m.state.Address = m.cfg.Address
		m.mu.Unlock()
	}

	info, err := m.pollUntilReachable(ctx)
	if err != nil {
		return err
	}

	if !info.Kind.Valid() {
		return fmt.Errorf("%w: provider reported unknown kind %q", domain.ErrProviderKindMismatch, string(info.Kind))
	}

	if stored != nil && stored.Kind != "" && stored.Kind != info.Kind {
		return fmt.Errorf(
			"%w: recorded %q but connected provider is %q",
			domain.ErrProviderKindMismatch,
			string(stored.Kind),
			string(info.Kind),
		)
	}

	return m.save(ctx, m.observe(info))
}

// Refresh は GetInfo を 1 度呼び、観測状態を更新して保存する。
func (m *Monitor) Refresh(ctx context.Context) (domain.ProviderState, error) {
	info, err := m.cfg.Port.GetInfo(ctx)
	if err != nil {
		state := m.observeFailure()
		if saveErr := m.save(ctx, state); saveErr != nil {
			m.cfg.Logger.ErrorContext(ctx, "save provider state failed", "error", saveErr)
		}

		return state, err
	}

	recorded := m.recordedKind()

	state := m.observe(info)
	if saveErr := m.save(ctx, state); saveErr != nil {
		return state, saveErr
	}

	if recorded != "" && recorded != info.Kind {
		return state, fmt.Errorf(
			"%w: recorded %q but connected provider is %q",
			domain.ErrProviderKindMismatch,
			string(recorded),
			string(info.Kind),
		)
	}

	return state, nil
}

// Run は ctx が終わるまで Interval ごとに Refresh を繰り返す。個々の失敗は状態へ記録し、ログに残して継続する。
func (m *Monitor) Run(ctx context.Context) error {
	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := m.Refresh(ctx); err != nil {
				m.cfg.Logger.WarnContext(ctx, "provider get info failed", "error", err, "address", m.cfg.Address)
			}
		}
	}
}

// State は最後に観測した Provider の状態を返す。
func (m *Monitor) State() domain.ProviderState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.state
}

// Online は Provider が利用可能かを返す。ユースケースはこれを見て provider-unavailable を返す。
func (m *Monitor) Online() bool {
	return m.State().Status == domain.ProviderStatusOnline
}

func (m *Monitor) pollUntilReachable(ctx context.Context) (usecase.ProviderInfo, error) {
	wait := m.cfg.MinBackoff

	for {
		info, err := m.cfg.Port.GetInfo(ctx)
		if err == nil {
			return info, nil
		}

		if saveErr := m.save(ctx, m.observeFailure()); saveErr != nil {
			m.cfg.Logger.ErrorContext(ctx, "save provider state failed", "error", saveErr)
		}

		m.cfg.Logger.WarnContext(ctx, "provider unreachable, retrying", "error", err, "retry_in", wait.String())

		if sleepErr := sleep(ctx, wait); sleepErr != nil {
			return usecase.ProviderInfo{}, fmt.Errorf("wait for provider: %w", errors.Join(sleepErr, err))
		}

		wait = min(wait*backoffFactor, m.cfg.MaxBackoff)
	}
}

func (m *Monitor) recordedKind() domain.ProviderKind {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.state.Kind
}

func (m *Monitor) observe(info usecase.ProviderInfo) domain.ProviderState {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.cfg.Clock.Now()
	if m.state.FirstSeenAt.IsZero() {
		m.state.FirstSeenAt = now
	}

	m.state.Kind = info.Kind
	m.state.Address = m.cfg.Address
	m.state.Capabilities = info.Capabilities
	m.state.Version = info.Version
	m.state.BotUserID = info.BotUserID
	m.state.Connected = info.Connected
	m.state.Failures = 0
	m.state.Status = domain.DecideProviderStatus(info.Connected, 0, m.cfg.OfflineAfterFailures)
	m.state.LastSeenAt = now
	m.state.UpdatedAt = now

	return m.state
}

func (m *Monitor) observeFailure() domain.ProviderState {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.cfg.Clock.Now()
	if m.state.FirstSeenAt.IsZero() {
		m.state.FirstSeenAt = now
	}

	// GetInfo に到達できないことと、チャットツールへ未接続であることは別の事象なので
	// Connected は最後に観測した値を保つ。offline 判定は連続失敗回数で行う(docs/16-grpc.md §6.2)。
	m.state.Address = m.cfg.Address
	m.state.Failures++
	m.state.Status = domain.DecideProviderStatus(m.state.Connected, m.state.Failures, m.cfg.OfflineAfterFailures)
	m.state.UpdatedAt = now

	return m.state
}

func (m *Monitor) save(ctx context.Context, state domain.ProviderState) error {
	if err := m.cfg.Meta.SaveProvider(ctx, &state); err != nil {
		return fmt.Errorf("save provider state: %w", err)
	}

	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
