// Package memstore は usecase.Repository のインメモリ実装。単体テスト専用で本番では使わない。
// Mongo 実装と同じ storetest の契約テストを通すことで、Fake と実装の乖離を防ぐ(docs/13-testing.md §3)。
package memstore

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	cursorParts     = 2
)

// Store は usecase のテスト用リポジトリ実装。storetest の契約テストを Mongo 実装と同じ内容で通す。
// 単一のミューテックスで全コレクションを保護し、Upsert / Claim の原子性を実装と同じ粒度で再現する。
type Store struct {
	mu  sync.Mutex
	seq int

	provider      *domain.ProviderState
	schemaVersion int

	workspaces     []*domain.Workspace
	events         []*domain.Event
	participations []*domain.Participation
	jobs           []*domain.Job
	reminderLogs   []*domain.ReminderLog
	consoleUsers   []*domain.ConsoleUser
	chatIdentities []*domain.ChatIdentity
	linkTokens     map[string]*domain.LinkToken
	sessions       map[string]*domain.Session
	oauthStates    map[string]*domain.OAuthState
}

func New() *Store {
	return &Store{
		linkTokens:  map[string]*domain.LinkToken{},
		sessions:    map[string]*domain.Session{},
		oauthStates: map[string]*domain.OAuthState{},
	}
}

func (s *Store) Meta() usecase.MetaRepository                    { return metaRepo{s} }
func (s *Store) Workspaces() usecase.WorkspaceRepository         { return workspacesRepo{s} }
func (s *Store) Events() usecase.EventRepository                 { return eventsRepo{s} }
func (s *Store) Participations() usecase.ParticipationRepository { return participationsRepo{s} }
func (s *Store) Jobs() usecase.JobRepository                     { return jobsRepo{s} }
func (s *Store) ReminderLogs() usecase.ReminderLogRepository     { return reminderLogsRepo{s} }
func (s *Store) ConsoleUsers() usecase.ConsoleUserRepository     { return consoleUsersRepo{s} }
func (s *Store) ChatIdentities() usecase.ChatIdentityRepository  { return chatIdentitiesRepo{s} }
func (s *Store) LinkTokens() usecase.LinkTokenRepository         { return linkTokensRepo{s} }
func (s *Store) Sessions() usecase.SessionRepository             { return sessionsRepo{s} }
func (s *Store) OAuthStates() usecase.OAuthStateRepository       { return oAuthStatesRepo{s} }

// nextID は ObjectID と同じ 24 桁 16 進の ID を採番する。domain.IsID の検証を通すため。
func (s *Store) nextID() string {
	s.seq++

	return fmt.Sprintf("%024x", s.seq)
}

type metaRepo struct{ s *Store }

func (m metaRepo) GetProvider(context.Context) (*domain.ProviderState, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if m.s.provider == nil {
		return nil, domain.ErrNotFound
	}

	state := *m.s.provider

	return &state, nil
}

func (m metaRepo) SaveProvider(_ context.Context, state *domain.ProviderState) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	saved := *state
	if m.s.provider != nil {
		// firstSeenAt は初回接続時刻のまま据え置く($setOnInsert 相当)。
		saved.FirstSeenAt = m.s.provider.FirstSeenAt
	}

	m.s.provider = &saved

	return nil
}

func (m metaRepo) SchemaVersion(context.Context) (int, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	return m.s.schemaVersion, nil
}

func (m metaRepo) SetSchemaVersion(_ context.Context, version int) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	m.s.schemaVersion = version

	return nil
}

type workspacesRepo struct{ s *Store }

func (m workspacesRepo) Get(_ context.Context, id string) (*domain.Workspace, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, ws := range m.s.workspaces {
		if ws.ID == id {
			return copyOf(ws), nil
		}
	}

	return nil, domain.ErrNotFound
}

func (m workspacesRepo) GetByExternalID(
	_ context.Context,
	provider domain.ProviderKind,
	externalID string,
) (*domain.Workspace, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if ws := m.s.findWorkspace(provider, externalID); ws != nil {
		return copyOf(ws), nil
	}

	return nil, domain.ErrNotFound
}

func (m workspacesRepo) List(context.Context) ([]*domain.Workspace, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	out := copyAll(m.s.workspaces)
	slices.SortFunc(out, func(a, b *domain.Workspace) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}

func (m workspacesRepo) Upsert(
	_ context.Context,
	ws *domain.Workspace,
	now time.Time,
) (*domain.Workspace, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if existing := m.s.findWorkspace(ws.Provider, ws.ExternalID); existing != nil {
		existing.Name = ws.Name
		existing.UpdatedAt = now.UTC()

		return copyOf(existing), nil
	}

	created := *ws
	created.ID = m.s.nextID()
	created.CreatedAt = now.UTC()
	created.UpdatedAt = now.UTC()
	m.s.workspaces = append(m.s.workspaces, &created)

	return copyOf(&created), nil
}

func (m workspacesRepo) UpdateSettings(
	_ context.Context,
	id string,
	settings domain.WorkspaceSettings,
	now time.Time,
) (*domain.Workspace, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, ws := range m.s.workspaces {
		if ws.ID == id {
			ws.Settings = settings
			ws.UpdatedAt = now.UTC()

			return copyOf(ws), nil
		}
	}

	return nil, domain.ErrNotFound
}

// findWorkspace は Store 上のワークスペースを (provider, externalId) で引く。
func (s *Store) findWorkspace(provider domain.ProviderKind, externalID string) *domain.Workspace {
	for _, ws := range s.workspaces {
		if ws.Provider == provider && ws.ExternalID == externalID {
			return ws
		}
	}

	return nil
}

type eventsRepo struct{ s *Store }

func (m eventsRepo) Create(_ context.Context, ev *domain.Event) (*domain.Event, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	created := *ev
	created.ID = m.s.nextID()
	m.s.events = append(m.s.events, &created)

	return copyOf(&created), nil
}

func (m eventsRepo) Get(_ context.Context, id string) (*domain.Event, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if ev := m.s.findEvent(id); ev != nil {
		return copyOf(ev), nil
	}

	return nil, domain.ErrNotFound
}

func (m eventsRepo) GetByChannelID(_ context.Context, channelID string) (*domain.Event, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, ev := range m.s.events {
		if ev.Channel.ChannelID == channelID {
			return copyOf(ev), nil
		}
	}

	return nil, domain.ErrNotFound
}

func (m eventsRepo) List(_ context.Context, filter usecase.EventFilter) (usecase.EventPage, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	matched, err := m.s.filterEvents(filter)
	if err != nil {
		return usecase.EventPage{}, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}

	limit = min(limit, maxPageSize)

	page := usecase.EventPage{}
	if len(matched) > limit {
		last := matched[limit-1]
		page.NextCursor = encodeMemCursor(last.StartsAt, last.ID)
		matched = matched[:limit]
	}

	page.Events = matched

	return page, nil
}

func (m eventsRepo) Update(_ context.Context, ev *domain.Event) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	stored := m.s.findEvent(ev.ID)
	if stored == nil {
		return domain.ErrNotFound
	}

	// Update はチャンネル・メッセージ参照と参加者数を対象にしない(Mongo 実装の $set と同じ範囲)。
	stored.Title = ev.Title
	stored.Description = ev.Description
	stored.Location = ev.Location
	stored.StartsAt = ev.StartsAt.UTC()
	stored.EndsAt = utcPtrOf(ev.EndsAt)
	stored.Status = ev.Status
	stored.ReminderPolicy = ev.ReminderPolicy
	stored.EndedAt = utcPtrOf(ev.EndedAt)
	stored.EndReason = ev.EndReason
	stored.UpdatedAt = ev.UpdatedAt.UTC()

	return nil
}

func (m eventsRepo) SetChannel(_ context.Context, id string, channel domain.ChannelRef, now time.Time) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	stored := m.s.findEvent(id)
	if stored == nil {
		return domain.ErrNotFound
	}

	stored.Channel = channel
	stored.UpdatedAt = now.UTC()

	return nil
}

func (m eventsRepo) SetMessages(_ context.Context, id string, messages domain.MessageRefs, now time.Time) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	stored := m.s.findEvent(id)
	if stored == nil {
		return domain.ErrNotFound
	}

	stored.Messages = messages
	stored.UpdatedAt = now.UTC()

	return nil
}

func (m eventsRepo) AddParticipantCount(_ context.Context, id string, delta int, now time.Time) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	stored := m.s.findEvent(id)
	if stored == nil {
		return domain.ErrNotFound
	}

	stored.ParticipantCount += delta
	stored.UpdatedAt = now.UTC()

	return nil
}

func (s *Store) findEvent(id string) *domain.Event {
	for _, ev := range s.events {
		if ev.ID == id {
			return ev
		}
	}

	return nil
}

func (s *Store) filterEvents(filter usecase.EventFilter) ([]*domain.Event, error) {
	var cursorStartsAt time.Time

	var cursorID string

	if filter.Cursor != "" {
		var err error

		cursorStartsAt, cursorID, err = decodeMemCursor(filter.Cursor)
		if err != nil {
			return nil, err
		}
	}

	matched := make([]*domain.Event, 0, len(s.events))

	for _, ev := range s.events {
		if !matchesEventFilter(ev, filter) {
			continue
		}

		if filter.Cursor != "" && !beforeCursor(ev, cursorStartsAt, cursorID) {
			continue
		}

		matched = append(matched, copyOf(ev))
	}

	slices.SortFunc(matched, func(a, b *domain.Event) int {
		if c := b.StartsAt.Compare(a.StartsAt); c != 0 {
			return c
		}

		return strings.Compare(b.ID, a.ID)
	})

	return matched, nil
}

func matchesEventFilter(ev *domain.Event, filter usecase.EventFilter) bool {
	if filter.WorkspaceID != "" && ev.WorkspaceID != filter.WorkspaceID {
		return false
	}

	if len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, ev.Status) {
		return false
	}

	if len(filter.IDs) > 0 && !slices.Contains(filter.IDs, ev.ID) {
		return false
	}

	if filter.StartsAfter != nil && ev.StartsAt.Before(*filter.StartsAfter) {
		return false
	}

	return true
}

func beforeCursor(ev *domain.Event, startsAt time.Time, id string) bool {
	if ev.StartsAt.Before(startsAt) {
		return true
	}

	return ev.StartsAt.Equal(startsAt) && ev.ID < id
}

func encodeMemCursor(startsAt time.Time, id string) string {
	raw := strconv.FormatInt(startsAt.UTC().UnixMilli(), 10) + "|" + id

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeMemCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("decode cursor: %w", err)
	}

	parts := strings.SplitN(string(raw), "|", cursorParts)
	if len(parts) != cursorParts {
		return time.Time{}, "", fmt.Errorf("decode cursor: malformed %q", cursor)
	}

	millis, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("decode cursor: %w", err)
	}

	return time.UnixMilli(millis).UTC(), parts[1], nil
}

type participationsRepo struct{ s *Store }

func (m participationsRepo) Upsert(
	_ context.Context,
	in usecase.ParticipationUpsert,
) (usecase.ParticipationChange, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	now := in.Now.UTC()
	stored := m.s.findParticipation(in.EventID, in.ChatUserID)

	if stored == nil {
		created := &domain.Participation{
			ID:          m.s.nextID(),
			EventID:     in.EventID,
			WorkspaceID: in.WorkspaceID,
			ChatUserID:  in.ChatUserID,
			DisplayName: in.DisplayName,
			Role:        in.Role,
			Status:      domain.ParticipationActive,
			JoinedAt:    now,
			ExpiresAt:   utcPtrOf(in.ExpiresAt),
			PeekCount:   peekIncrement(in.Role),
			UpdatedAt:   now,
		}
		m.s.participations = append(m.s.participations, created)

		return usecase.ParticipationChange{After: copyOf(created)}, nil
	}

	before := copyOf(stored)

	// 参加済みユーザーがチラ見を押しても降格しない(docs/04-domain-model.md §2.5.1)。
	keepsParticipant := stored.Role == domain.RoleParticipant && stored.Status == domain.ParticipationActive

	stored.DisplayName = in.DisplayName
	stored.Status = domain.ParticipationActive
	stored.UpdatedAt = now

	if !keepsParticipant {
		stored.Role = in.Role
		stored.ExpiresAt = utcPtrOf(in.ExpiresAt)
		stored.PeekCount += peekIncrement(in.Role)
	}

	return usecase.ParticipationChange{Before: before, After: copyOf(stored)}, nil
}

func (m participationsRepo) Get(_ context.Context, id string) (*domain.Participation, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, p := range m.s.participations {
		if p.ID == id {
			return copyOf(p), nil
		}
	}

	return nil, domain.ErrNotFound
}

func (m participationsRepo) GetByUser(
	_ context.Context,
	eventID, chatUserID string,
) (*domain.Participation, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if p := m.s.findParticipation(eventID, chatUserID); p != nil {
		return copyOf(p), nil
	}

	return nil, domain.ErrNotFound
}

func (m participationsRepo) SetStatus(
	_ context.Context,
	id string,
	status domain.ParticipationStatus,
	now time.Time,
) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, p := range m.s.participations {
		if p.ID != id {
			continue
		}

		p.Status = status
		p.UpdatedAt = now.UTC()

		if status != domain.ParticipationActive {
			p.ExpiresAt = nil
		}

		return nil
	}

	return domain.ErrNotFound
}

func (m participationsRepo) ListByEvent(
	_ context.Context,
	eventID string,
	statuses ...domain.ParticipationStatus,
) ([]*domain.Participation, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	var out []*domain.Participation

	for _, p := range m.s.participations {
		if p.EventID != eventID {
			continue
		}

		if len(statuses) > 0 && !slices.Contains(statuses, p.Status) {
			continue
		}

		out = append(out, copyOf(p))
	}

	slices.SortFunc(out, func(a, b *domain.Participation) int { return a.JoinedAt.Compare(b.JoinedAt) })

	return out, nil
}

func (m participationsRepo) CountActive(
	_ context.Context,
	eventID string,
	role domain.ParticipationRole,
) (int, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	n := 0

	for _, p := range m.s.participations {
		if p.EventID != eventID || p.Status != domain.ParticipationActive {
			continue
		}

		if role != "" && p.Role != role {
			continue
		}

		n++
	}

	return n, nil
}

func (s *Store) findParticipation(eventID, chatUserID string) *domain.Participation {
	for _, p := range s.participations {
		if p.EventID == eventID && p.ChatUserID == chatUserID {
			return p
		}
	}

	return nil
}

func peekIncrement(role domain.ParticipationRole) int {
	if role == domain.RolePeeker {
		return 1
	}

	return 0
}

type jobsRepo struct{ s *Store }

func (m jobsRepo) Enqueue(_ context.Context, job domain.Job, now time.Time) (*domain.Job, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if job.DedupeKey != "" {
		for _, existing := range m.s.jobs {
			if existing.DedupeKey == job.DedupeKey {
				return nil, domain.ErrAlreadyExists
			}
		}
	}

	created := job
	created.ID = m.s.nextID()
	created.RunAt = job.RunAt.UTC()
	created.Status = domain.JobPending
	created.Attempts = 0
	created.LeaseUntil = nil
	created.LastError = ""
	created.CreatedAt = now.UTC()
	created.UpdatedAt = now.UTC()

	if created.MaxAttempts <= 0 {
		created.MaxAttempts = domain.DefaultMaxAttempts
	}

	m.s.jobs = append(m.s.jobs, &created)

	return copyOf(&created), nil
}

func (m jobsRepo) Get(_ context.Context, id string) (*domain.Job, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if job := m.s.findJob(id); job != nil {
		return copyOf(job), nil
	}

	return nil, domain.ErrNotFound
}

func (m jobsRepo) Claim(_ context.Context, now time.Time, lease time.Duration) (*domain.Job, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	at := now.UTC()

	var target *domain.Job

	for _, job := range m.s.jobs {
		if !claimable(job, at) {
			continue
		}

		if target == nil || job.RunAt.Before(target.RunAt) {
			target = job
		}
	}

	if target == nil {
		return nil, domain.ErrNotFound
	}

	leaseUntil := at.Add(lease)
	target.Status = domain.JobRunning
	target.LeaseUntil = &leaseUntil
	target.Attempts++
	target.UpdatedAt = at

	return copyOf(target), nil
}

func (m jobsRepo) Complete(_ context.Context, id string, now time.Time) error {
	return m.s.updateJob(id, func(job *domain.Job) {
		at := now.UTC()
		job.Status = domain.JobDone
		job.FinishedAt = &at
		job.LeaseUntil = nil
		job.UpdatedAt = at
	})
}

func (m jobsRepo) Retry(_ context.Context, id string, runAt time.Time, lastErr string, now time.Time) error {
	return m.s.updateJob(id, func(job *domain.Job) {
		job.Status = domain.JobPending
		job.RunAt = runAt.UTC()
		job.LastError = lastErr
		job.LeaseUntil = nil
		job.UpdatedAt = now.UTC()
	})
}

func (m jobsRepo) Fail(_ context.Context, id, lastErr string, now time.Time) error {
	return m.s.updateJob(id, func(job *domain.Job) {
		at := now.UTC()
		job.Status = domain.JobFailed
		job.FinishedAt = &at
		job.LastError = lastErr
		job.LeaseUntil = nil
		job.UpdatedAt = at
	})
}

func (m jobsRepo) Reschedule(_ context.Context, id string, runAt time.Time, now time.Time) error {
	return m.s.updateJob(id, func(job *domain.Job) {
		job.Status = domain.JobPending
		job.RunAt = runAt.UTC()
		job.LeaseUntil = nil
		job.UpdatedAt = now.UTC()
		// 実行していないため Claim で増えた再試行回数を戻す。
		job.Attempts--
	})
}

func (m jobsRepo) ExtendLease(_ context.Context, id string, until time.Time, now time.Time) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	job := m.s.findJob(id)
	if job == nil || job.Status != domain.JobRunning {
		return domain.ErrNotFound
	}

	leaseUntil := until.UTC()
	job.LeaseUntil = &leaseUntil
	job.UpdatedAt = now.UTC()

	return nil
}

func (m jobsRepo) CancelByEvent(
	_ context.Context,
	eventID string,
	kinds []domain.JobKind,
	now time.Time,
) (int, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	at := now.UTC()
	canceled := 0

	for _, job := range m.s.jobs {
		if job.EventID != eventID || job.Status != domain.JobPending {
			continue
		}

		if len(kinds) > 0 && !slices.Contains(kinds, job.Kind) {
			continue
		}

		job.Status = domain.JobCanceled
		job.FinishedAt = &at
		job.UpdatedAt = at
		canceled++
	}

	return canceled, nil
}

func (m jobsRepo) ListByEvent(
	_ context.Context,
	eventID string,
	statuses ...domain.JobStatus,
) ([]*domain.Job, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	var out []*domain.Job

	for _, job := range m.s.jobs {
		if job.EventID != eventID {
			continue
		}

		if len(statuses) > 0 && !slices.Contains(statuses, job.Status) {
			continue
		}

		out = append(out, copyOf(job))
	}

	slices.SortFunc(out, func(a, b *domain.Job) int { return a.RunAt.Compare(b.RunAt) })

	return out, nil
}

func (s *Store) findJob(id string) *domain.Job {
	for _, job := range s.jobs {
		if job.ID == id {
			return job
		}
	}

	return nil
}

func (s *Store) updateJob(id string, apply func(*domain.Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.findJob(id)
	if job == nil {
		return domain.ErrNotFound
	}

	apply(job)

	return nil
}

func claimable(job *domain.Job, at time.Time) bool {
	if job.Status == domain.JobPending {
		return !job.RunAt.After(at)
	}

	// リース切れの running は実行中プロセスの異常終了とみなして再取得する。
	return job.Status == domain.JobRunning && job.LeaseUntil != nil && !job.LeaseUntil.After(at)
}

type reminderLogsRepo struct{ s *Store }

func (m reminderLogsRepo) Append(_ context.Context, log *domain.ReminderLog) (*domain.ReminderLog, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	created := *log
	created.ID = m.s.nextID()
	created.ScheduledFor = log.ScheduledFor.UTC()
	created.SentAt = log.SentAt.UTC()
	created.Targets = slices.Clone(log.Targets)
	m.s.reminderLogs = append(m.s.reminderLogs, &created)

	return copyOf(&created), nil
}

func (m reminderLogsRepo) ListByEvent(_ context.Context, eventID string) ([]*domain.ReminderLog, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	var out []*domain.ReminderLog

	for _, log := range m.s.reminderLogs {
		if log.EventID == eventID {
			out = append(out, copyOf(log))
		}
	}

	slices.SortFunc(out, func(a, b *domain.ReminderLog) int { return a.SentAt.Compare(b.SentAt) })

	return out, nil
}

func (m reminderLogsRepo) SentAt(_ context.Context, eventID string, scheduledFor time.Time) (bool, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, log := range m.s.reminderLogs {
		if log.EventID == eventID && log.ScheduledFor.Equal(scheduledFor.UTC()) {
			return true, nil
		}
	}

	return false, nil
}

func (m reminderLogsRepo) SentSequence(_ context.Context, eventID string, sequence int) (bool, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, log := range m.s.reminderLogs {
		if log.EventID == eventID && log.Sequence == sequence {
			return true, nil
		}
	}

	return false, nil
}

type consoleUsersRepo struct{ s *Store }

func (m consoleUsersRepo) Get(_ context.Context, id string) (*domain.ConsoleUser, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, user := range m.s.consoleUsers {
		if user.ID == id {
			return copyOf(user), nil
		}
	}

	return nil, domain.ErrNotFound
}

func (m consoleUsersRepo) GetByGoogleSub(_ context.Context, sub string) (*domain.ConsoleUser, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, user := range m.s.consoleUsers {
		if user.GoogleSub == sub {
			return copyOf(user), nil
		}
	}

	return nil, domain.ErrNotFound
}

func (m consoleUsersRepo) Create(_ context.Context, user *domain.ConsoleUser) (*domain.ConsoleUser, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, existing := range m.s.consoleUsers {
		if existing.GoogleSub == user.GoogleSub {
			return nil, domain.ErrAlreadyExists
		}
	}

	created := *user
	created.ID = m.s.nextID()
	created.CreatedAt = user.CreatedAt.UTC()
	created.LastLoginAt = user.LastLoginAt.UTC()
	m.s.consoleUsers = append(m.s.consoleUsers, &created)

	return copyOf(&created), nil
}

func (m consoleUsersRepo) TouchLogin(_ context.Context, id string, now time.Time) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, user := range m.s.consoleUsers {
		if user.ID == id {
			user.LastLoginAt = now.UTC()

			return nil
		}
	}

	return domain.ErrNotFound
}

type chatIdentitiesRepo struct{ s *Store }

func (m chatIdentitiesRepo) Link(_ context.Context, identity *domain.ChatIdentity) (*domain.ChatIdentity, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, existing := range m.s.chatIdentities {
		if existing.WorkspaceID == identity.WorkspaceID && existing.ChatUserID == identity.ChatUserID {
			return nil, domain.ErrIdentityLinkedElsewhere
		}
	}

	created := *identity
	created.ID = m.s.nextID()
	created.LinkedAt = identity.LinkedAt.UTC()
	m.s.chatIdentities = append(m.s.chatIdentities, &created)

	return copyOf(&created), nil
}

func (m chatIdentitiesRepo) Get(_ context.Context, workspaceID, chatUserID string) (*domain.ChatIdentity, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for _, identity := range m.s.chatIdentities {
		if identity.WorkspaceID == workspaceID && identity.ChatUserID == chatUserID {
			return copyOf(identity), nil
		}
	}

	return nil, domain.ErrNotFound
}

func (m chatIdentitiesRepo) ListByUser(_ context.Context, consoleUserID string) ([]*domain.ChatIdentity, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	var out []*domain.ChatIdentity

	for _, identity := range m.s.chatIdentities {
		if identity.ConsoleUserID == consoleUserID {
			out = append(out, copyOf(identity))
		}
	}

	slices.SortFunc(out, func(a, b *domain.ChatIdentity) int { return a.LinkedAt.Compare(b.LinkedAt) })

	return out, nil
}

func (m chatIdentitiesRepo) Unlink(_ context.Context, id string) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for i, identity := range m.s.chatIdentities {
		if identity.ID == id {
			m.s.chatIdentities = slices.Delete(m.s.chatIdentities, i, i+1)

			return nil
		}
	}

	return domain.ErrNotFound
}

type linkTokensRepo struct{ s *Store }

func (m linkTokensRepo) Create(_ context.Context, token domain.LinkToken) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if _, ok := m.s.linkTokens[token.ID]; ok {
		return domain.ErrAlreadyExists
	}

	stored := token
	stored.ExpiresAt = token.ExpiresAt.UTC()
	m.s.linkTokens[token.ID] = &stored

	return nil
}

func (m linkTokensRepo) Get(_ context.Context, id string) (*domain.LinkToken, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	token, ok := m.s.linkTokens[id]
	if !ok {
		return nil, domain.ErrNotFound
	}

	return copyOf(token), nil
}

func (m linkTokensRepo) Consume(_ context.Context, id string, now time.Time) (*domain.LinkToken, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	token, ok := m.s.linkTokens[id]
	if !ok {
		return nil, domain.ErrNotFound
	}

	at := now.UTC()
	if err := token.Usable(at); err != nil {
		return nil, err
	}

	token.ConsumedAt = &at

	return copyOf(token), nil
}

type sessionsRepo struct{ s *Store }

func (m sessionsRepo) Create(_ context.Context, session domain.Session) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if _, ok := m.s.sessions[session.ID]; ok {
		return domain.ErrAlreadyExists
	}

	stored := session
	m.s.sessions[session.ID] = &stored

	return nil
}

func (m sessionsRepo) Get(_ context.Context, id string) (*domain.Session, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	session, ok := m.s.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}

	return copyOf(session), nil
}

func (m sessionsRepo) Touch(_ context.Context, id string, now time.Time) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	session, ok := m.s.sessions[id]
	if !ok {
		return domain.ErrNotFound
	}

	session.LastSeenAt = now.UTC()

	return nil
}

func (m sessionsRepo) Delete(_ context.Context, id string) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	delete(m.s.sessions, id)

	return nil
}

func (m sessionsRepo) DeleteByUser(_ context.Context, userID string) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	for id, session := range m.s.sessions {
		if session.UserID == userID {
			delete(m.s.sessions, id)
		}
	}

	return nil
}

type oAuthStatesRepo struct{ s *Store }

func (m oAuthStatesRepo) Create(_ context.Context, state domain.OAuthState) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if _, ok := m.s.oauthStates[state.State]; ok {
		return domain.ErrAlreadyExists
	}

	stored := state
	stored.ExpiresAt = state.ExpiresAt.UTC()
	m.s.oauthStates[state.State] = &stored

	return nil
}

func (m oAuthStatesRepo) Consume(_ context.Context, state string, now time.Time) (*domain.OAuthState, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	stored, ok := m.s.oauthStates[state]
	if !ok || !stored.ExpiresAt.After(now.UTC()) {
		return nil, domain.ErrNotFound
	}

	delete(m.s.oauthStates, state)

	return copyOf(stored), nil
}

func copyOf[T any](v *T) *T {
	if v == nil {
		return nil
	}

	c := *v

	return &c
}

func copyAll[T any](in []*T) []*T {
	out := make([]*T, 0, len(in))
	for _, v := range in {
		out = append(out, copyOf(v))
	}

	return out
}

func utcPtrOf(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	v := t.UTC()

	return &v
}

var _ usecase.Repository = (*Store)(nil)
