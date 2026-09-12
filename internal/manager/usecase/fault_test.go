package usecase_test

import (
	"context"
	"sync"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// 失敗を仕込める操作の名前。リポジトリのメソッドと 1 対 1 に対応する。
const (
	opWorkspacesGet    = "workspaces.get"
	opWorkspacesUpsert = "workspaces.upsert"
	opEventsCreate     = "events.create"
	opEventsGet        = "events.get"
	opEventsUpdate     = "events.update"
	opEventsSetChannel = "events.setChannel"
	opEventsAddCount   = "events.addParticipantCount"
	opPartsUpsert      = "participations.upsert"
	opPartsGet         = "participations.get"
	opPartsGetByUser   = "participations.getByUser"
	opPartsSetStatus   = "participations.setStatus"
	opPartsListByEvent = "participations.listByEvent"
	opJobsEnqueue      = "jobs.enqueue"
	opJobsCancel       = "jobs.cancelByEvent"
	opJobsListByEvent  = "jobs.listByEvent"
	opLogsAppend       = "reminderLogs.append"
	opLogsSentAt       = "reminderLogs.sentAt"
	opLogsSentSeq      = "reminderLogs.sentSequence"
	opIdentitiesByUser = "chatIdentities.listByUser"
	opTokensCreate     = "linkTokens.create"
	opTokensGet        = "linkTokens.get"
)

// faultRepo は memstore を包み、仕込んだ操作だけを 1 度失敗させるテスト用リポジトリ。
// インメモリ実装では永続化の失敗を再現できないが、ユースケースが失敗をそのまま呼び出し元へ
// 伝えることは検証したいため、境界で差し込む。
type faultRepo struct {
	usecase.Repository

	mu     sync.Mutex
	queued map[string]error
}

func newFaultRepo(inner usecase.Repository) *faultRepo {
	return &faultRepo{Repository: inner, queued: map[string]error{}}
}

// failOn は op の次の呼び出しで err を返させる。
func (r *faultRepo) failOn(op string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.queued[op] = err
}

func (r *faultRepo) check(op string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	err, ok := r.queued[op]
	if !ok {
		return nil
	}

	delete(r.queued, op)

	return err
}

func (r *faultRepo) Workspaces() usecase.WorkspaceRepository {
	return faultWorkspaces{WorkspaceRepository: r.Repository.Workspaces(), repo: r}
}

func (r *faultRepo) Events() usecase.EventRepository {
	return faultEvents{EventRepository: r.Repository.Events(), repo: r}
}

func (r *faultRepo) Participations() usecase.ParticipationRepository {
	return faultParticipations{ParticipationRepository: r.Repository.Participations(), repo: r}
}

func (r *faultRepo) Jobs() usecase.JobRepository {
	return faultJobs{JobRepository: r.Repository.Jobs(), repo: r}
}

func (r *faultRepo) ReminderLogs() usecase.ReminderLogRepository {
	return faultLogs{ReminderLogRepository: r.Repository.ReminderLogs(), repo: r}
}

func (r *faultRepo) ChatIdentities() usecase.ChatIdentityRepository {
	return faultIdentities{ChatIdentityRepository: r.Repository.ChatIdentities(), repo: r}
}

func (r *faultRepo) LinkTokens() usecase.LinkTokenRepository {
	return faultTokens{LinkTokenRepository: r.Repository.LinkTokens(), repo: r}
}

type faultWorkspaces struct {
	usecase.WorkspaceRepository

	repo *faultRepo
}

func (r faultWorkspaces) Get(ctx context.Context, id string) (*domain.Workspace, error) {
	if err := r.repo.check(opWorkspacesGet); err != nil {
		return nil, err
	}

	return r.WorkspaceRepository.Get(ctx, id)
}

func (r faultWorkspaces) Upsert(
	ctx context.Context,
	ws *domain.Workspace,
	now time.Time,
) (*domain.Workspace, error) {
	if err := r.repo.check(opWorkspacesUpsert); err != nil {
		return nil, err
	}

	return r.WorkspaceRepository.Upsert(ctx, ws, now)
}

type faultEvents struct {
	usecase.EventRepository

	repo *faultRepo
}

func (r faultEvents) Create(ctx context.Context, ev *domain.Event) (*domain.Event, error) {
	if err := r.repo.check(opEventsCreate); err != nil {
		return nil, err
	}

	return r.EventRepository.Create(ctx, ev)
}

func (r faultEvents) Get(ctx context.Context, id string) (*domain.Event, error) {
	if err := r.repo.check(opEventsGet); err != nil {
		return nil, err
	}

	return r.EventRepository.Get(ctx, id)
}

func (r faultEvents) Update(ctx context.Context, ev *domain.Event) error {
	if err := r.repo.check(opEventsUpdate); err != nil {
		return err
	}

	return r.EventRepository.Update(ctx, ev)
}

func (r faultEvents) SetChannel(
	ctx context.Context,
	id string,
	channel domain.ChannelRef,
	now time.Time,
) error {
	if err := r.repo.check(opEventsSetChannel); err != nil {
		return err
	}

	return r.EventRepository.SetChannel(ctx, id, channel, now)
}

func (r faultEvents) AddParticipantCount(ctx context.Context, id string, delta int, now time.Time) error {
	if err := r.repo.check(opEventsAddCount); err != nil {
		return err
	}

	return r.EventRepository.AddParticipantCount(ctx, id, delta, now)
}

type faultParticipations struct {
	usecase.ParticipationRepository

	repo *faultRepo
}

func (r faultParticipations) Upsert(
	ctx context.Context,
	in usecase.ParticipationUpsert,
) (usecase.ParticipationChange, error) {
	if err := r.repo.check(opPartsUpsert); err != nil {
		return usecase.ParticipationChange{}, err
	}

	return r.ParticipationRepository.Upsert(ctx, in)
}

func (r faultParticipations) Get(ctx context.Context, id string) (*domain.Participation, error) {
	if err := r.repo.check(opPartsGet); err != nil {
		return nil, err
	}

	return r.ParticipationRepository.Get(ctx, id)
}

func (r faultParticipations) GetByUser(
	ctx context.Context,
	eventID, chatUserID string,
) (*domain.Participation, error) {
	if err := r.repo.check(opPartsGetByUser); err != nil {
		return nil, err
	}

	return r.ParticipationRepository.GetByUser(ctx, eventID, chatUserID)
}

func (r faultParticipations) SetStatus(
	ctx context.Context,
	id string,
	status domain.ParticipationStatus,
	now time.Time,
) error {
	if err := r.repo.check(opPartsSetStatus); err != nil {
		return err
	}

	return r.ParticipationRepository.SetStatus(ctx, id, status, now)
}

func (r faultParticipations) ListByEvent(
	ctx context.Context,
	eventID string,
	statuses ...domain.ParticipationStatus,
) ([]*domain.Participation, error) {
	if err := r.repo.check(opPartsListByEvent); err != nil {
		return nil, err
	}

	return r.ParticipationRepository.ListByEvent(ctx, eventID, statuses...)
}

type faultJobs struct {
	usecase.JobRepository

	repo *faultRepo
}

func (r faultJobs) Enqueue(ctx context.Context, job domain.Job, now time.Time) (*domain.Job, error) {
	if err := r.repo.check(opJobsEnqueue); err != nil {
		return nil, err
	}

	return r.JobRepository.Enqueue(ctx, job, now)
}

func (r faultJobs) CancelByEvent(
	ctx context.Context,
	eventID string,
	kinds []domain.JobKind,
	now time.Time,
) (int, error) {
	if err := r.repo.check(opJobsCancel); err != nil {
		return 0, err
	}

	return r.JobRepository.CancelByEvent(ctx, eventID, kinds, now)
}

func (r faultJobs) ListByEvent(
	ctx context.Context,
	eventID string,
	statuses ...domain.JobStatus,
) ([]*domain.Job, error) {
	if err := r.repo.check(opJobsListByEvent); err != nil {
		return nil, err
	}

	return r.JobRepository.ListByEvent(ctx, eventID, statuses...)
}

type faultLogs struct {
	usecase.ReminderLogRepository

	repo *faultRepo
}

func (r faultLogs) Append(ctx context.Context, log *domain.ReminderLog) (*domain.ReminderLog, error) {
	if err := r.repo.check(opLogsAppend); err != nil {
		return nil, err
	}

	return r.ReminderLogRepository.Append(ctx, log)
}

func (r faultLogs) SentAt(ctx context.Context, eventID string, scheduledFor time.Time) (bool, error) {
	if err := r.repo.check(opLogsSentAt); err != nil {
		return false, err
	}

	return r.ReminderLogRepository.SentAt(ctx, eventID, scheduledFor)
}

func (r faultLogs) SentSequence(ctx context.Context, eventID string, sequence int) (bool, error) {
	if err := r.repo.check(opLogsSentSeq); err != nil {
		return false, err
	}

	return r.ReminderLogRepository.SentSequence(ctx, eventID, sequence)
}

type faultIdentities struct {
	usecase.ChatIdentityRepository

	repo *faultRepo
}

func (r faultIdentities) ListByUser(ctx context.Context, consoleUserID string) ([]*domain.ChatIdentity, error) {
	if err := r.repo.check(opIdentitiesByUser); err != nil {
		return nil, err
	}

	return r.ChatIdentityRepository.ListByUser(ctx, consoleUserID)
}

type faultTokens struct {
	usecase.LinkTokenRepository

	repo *faultRepo
}

func (r faultTokens) Create(ctx context.Context, token domain.LinkToken) error {
	if err := r.repo.check(opTokensCreate); err != nil {
		return err
	}

	return r.LinkTokenRepository.Create(ctx, token)
}

func (r faultTokens) Get(ctx context.Context, id string) (*domain.LinkToken, error) {
	if err := r.repo.check(opTokensGet); err != nil {
		return nil, err
	}

	return r.LinkTokenRepository.Get(ctx, id)
}
