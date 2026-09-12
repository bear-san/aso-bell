package providerclient_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/providerclient"
	"github.com/bear-san/aso-bell/internal/manager/testutil"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

var errUnreachable = errors.New("provider unreachable")

type memMeta struct {
	mu      sync.Mutex
	state   *domain.ProviderState
	version int
}

func (m *memMeta) GetProvider(_ context.Context) (*domain.ProviderState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		return nil, domain.ErrNotFound
	}

	state := *m.state

	return &state, nil
}

func (m *memMeta) SaveProvider(_ context.Context, state *domain.ProviderState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	saved := *state
	if m.state != nil && !m.state.FirstSeenAt.IsZero() {
		saved.FirstSeenAt = m.state.FirstSeenAt
	}

	m.state = &saved

	return nil
}

func (m *memMeta) SchemaVersion(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.version, nil
}

func (m *memMeta) SetSchemaVersion(_ context.Context, version int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.version = version

	return nil
}

// stubPort は GetInfo だけを差し替える ProviderPort。他の RPC はこのテストで使わない。
type stubPort struct {
	usecase.ProviderPort

	mu           sync.Mutex
	info         usecase.ProviderInfo
	failuresLeft int
	calls        int
}

func (p *stubPort) GetInfo(_ context.Context) (usecase.ProviderInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++

	if p.failuresLeft > 0 {
		p.failuresLeft--

		return usecase.ProviderInfo{}, errUnreachable
	}

	return p.info, nil
}

func (p *stubPort) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.calls
}

func slackInfo(connected bool) usecase.ProviderInfo {
	return usecase.ProviderInfo{
		Kind:         domain.ProviderKindSlack,
		Capabilities: domain.Capabilities{Forms: true, Ephemeral: true, DirectMessage: true},
		Version:      "v0.1.0",
		BotUserID:    "UBOT",
		Connected:    connected,
	}
}

func newMonitor(t *testing.T, port usecase.ProviderPort, meta *memMeta, now time.Time) *providerclient.Monitor {
	t.Helper()

	return providerclient.NewMonitor(providerclient.MonitorConfig{
		Port:                 port,
		Logger:               slog.New(slog.DiscardHandler),
		Meta:                 meta,
		Clock:                testutil.NewFakeClock(now),
		Address:              "dns:///provider:9091",
		Interval:             time.Millisecond,
		OfflineAfterFailures: 3,
		MinBackoff:           time.Millisecond,
		MaxBackoff:           2 * time.Millisecond,
	})
}

func TestMonitorBootstrapRecordsState(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	meta := &memMeta{}
	monitor := newMonitor(t, &stubPort{info: slackInfo(true)}, meta, now)

	require.NoError(t, monitor.Bootstrap(t.Context()))

	state := monitor.State()
	assert.Equal(t, domain.ProviderKindSlack, state.Kind)
	assert.Equal(t, domain.ProviderStatusOnline, state.Status)
	assert.Equal(t, "dns:///provider:9091", state.Address)
	assert.Equal(t, now, state.FirstSeenAt)
	assert.True(t, monitor.Online())

	stored, err := meta.GetProvider(t.Context())
	require.NoError(t, err)
	assert.Equal(t, domain.ProviderKindSlack, stored.Kind)
	assert.Equal(t, "v0.1.0", stored.Version)
}

func TestMonitorBootstrapOverGRPC(t *testing.T) {
	client, _ := newClient(t)
	meta := &memMeta{}
	monitor := newMonitor(t, client, meta, time.Now().UTC())

	require.NoError(t, monitor.Bootstrap(t.Context()))

	assert.Equal(t, domain.ProviderKindSlack, monitor.State().Kind)
	assert.True(t, monitor.Online())
}

func TestMonitorBootstrapRetriesUntilReachable(t *testing.T) {
	port := &stubPort{info: slackInfo(true), failuresLeft: 2}
	meta := &memMeta{}
	monitor := newMonitor(t, port, meta, time.Now().UTC())

	require.NoError(t, monitor.Bootstrap(t.Context()))

	assert.Equal(t, 3, port.Calls())
	assert.Equal(t, 0, monitor.State().Failures)
}

func TestMonitorBootstrapRejectsKindMismatch(t *testing.T) {
	meta := &memMeta{state: &domain.ProviderState{Kind: domain.ProviderKindDiscord}}
	monitor := newMonitor(t, &stubPort{info: slackInfo(true)}, meta, time.Now().UTC())

	err := monitor.Bootstrap(t.Context())

	require.ErrorIs(t, err, domain.ErrProviderKindMismatch)

	stored, getErr := meta.GetProvider(t.Context())
	require.NoError(t, getErr)
	assert.Equal(t, domain.ProviderKindDiscord, stored.Kind)
}

func TestMonitorBootstrapRejectsUnknownKind(t *testing.T) {
	monitor := newMonitor(t, &stubPort{info: usecase.ProviderInfo{Connected: true}}, &memMeta{}, time.Now().UTC())

	require.ErrorIs(t, monitor.Bootstrap(t.Context()), domain.ErrProviderKindMismatch)
}

func TestMonitorBootstrapGivesUpWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	monitor := newMonitor(t, &stubPort{info: slackInfo(true), failuresLeft: 10}, &memMeta{}, time.Now().UTC())

	require.ErrorIs(t, monitor.Bootstrap(ctx), context.Canceled)
}

func TestMonitorGoesOfflineAfterConsecutiveFailures(t *testing.T) {
	port := &stubPort{info: slackInfo(true)}
	monitor := newMonitor(t, port, &memMeta{}, time.Now().UTC())
	require.NoError(t, monitor.Bootstrap(t.Context()))

	port.mu.Lock()
	port.failuresLeft = 3
	port.mu.Unlock()

	for i := 1; i <= 2; i++ {
		_, err := monitor.Refresh(t.Context())
		require.ErrorIs(t, err, errUnreachable)
		assert.Equal(t, domain.ProviderStatusOnline, monitor.State().Status, "failure %d", i)
	}

	_, err := monitor.Refresh(t.Context())
	require.ErrorIs(t, err, errUnreachable)
	assert.Equal(t, domain.ProviderStatusOffline, monitor.State().Status)
	assert.False(t, monitor.Online())

	_, err = monitor.Refresh(t.Context())
	require.NoError(t, err)
	assert.Equal(t, domain.ProviderStatusOnline, monitor.State().Status)
	assert.Equal(t, 0, monitor.State().Failures)
}

func TestMonitorDisconnectedProviderIsOffline(t *testing.T) {
	monitor := newMonitor(t, &stubPort{info: slackInfo(false)}, &memMeta{}, time.Now().UTC())

	state, err := monitor.Refresh(t.Context())
	require.NoError(t, err)

	assert.Equal(t, domain.ProviderStatusOffline, state.Status)
	assert.Equal(t, 0, state.Failures)
}

func TestMonitorRefreshDetectsKindChange(t *testing.T) {
	port := &stubPort{info: slackInfo(true)}
	monitor := newMonitor(t, port, &memMeta{}, time.Now().UTC())
	require.NoError(t, monitor.Bootstrap(t.Context()))

	port.mu.Lock()
	port.info.Kind = domain.ProviderKindDiscord
	port.mu.Unlock()

	_, err := monitor.Refresh(t.Context())

	require.ErrorIs(t, err, domain.ErrProviderKindMismatch)
}

func TestMonitorRunPollsUntilContextEnds(t *testing.T) {
	port := &stubPort{info: slackInfo(true)}
	monitor := newMonitor(t, port, &memMeta{}, time.Now().UTC())

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- monitor.Run(ctx) }()

	require.Eventually(t, func() bool { return port.Calls() >= 2 }, time.Second, time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestMonitorRunSurvivesFailures(t *testing.T) {
	port := &stubPort{info: slackInfo(true), failuresLeft: 2}
	monitor := newMonitor(t, port, &memMeta{}, time.Now().UTC())

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = monitor.Run(ctx) }()

	require.Eventually(t, func() bool { return monitor.Online() }, time.Second, time.Millisecond)
}
