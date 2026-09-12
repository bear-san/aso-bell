package storetest

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

const (
	testLease           = 2 * time.Minute
	concurrentClaimers  = 20
	concurrentJoinCalls = 10
)

func testParticipations(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	parts := repo.Participations()
	ws := SeedWorkspace(t, repo, "T0123")
	ev := SeedEvent(t, repo, ws.ID, Now().Add(7*24*time.Hour))

	join := usecase.ParticipationUpsert{
		EventID:     ev.ID,
		WorkspaceID: ws.ID,
		ChatUserID:  "U0123",
		DisplayName: "kentaro",
		Role:        domain.RoleParticipant,
		Now:         Now(),
	}

	first, err := parts.Upsert(ctx, join)
	require.NoError(t, err)
	assert.Nil(t, first.Before)
	require.NotNil(t, first.After)
	assert.Equal(t, domain.RoleParticipant, first.After.Role)
	assert.Equal(t, domain.ParticipationActive, first.After.Status)
	assert.Equal(t, 0, first.After.PeekCount)
	assert.Nil(t, first.After.ExpiresAt)

	again, err := parts.Upsert(ctx, join)
	require.NoError(t, err)
	require.NotNil(t, again.Before)
	assert.Equal(t, domain.RoleParticipant, again.Before.Role)
	assert.Equal(t, first.After.ID, again.After.ID)

	expiresAt := Now().Add(time.Hour)
	peek := usecase.ParticipationUpsert{
		EventID:     ev.ID,
		WorkspaceID: ws.ID,
		ChatUserID:  "U0456",
		DisplayName: "taro",
		Role:        domain.RolePeeker,
		ExpiresAt:   &expiresAt,
		Now:         Now(),
	}

	peeked, err := parts.Upsert(ctx, peek)
	require.NoError(t, err)
	assert.Nil(t, peeked.Before)
	assert.Equal(t, domain.RolePeeker, peeked.After.Role)
	assert.Equal(t, 1, peeked.After.PeekCount)
	require.NotNil(t, peeked.After.ExpiresAt)
	assert.Equal(t, expiresAt, *peeked.After.ExpiresAt)

	promote := peek
	promote.Role = domain.RoleParticipant
	promote.ExpiresAt = nil
	promote.Now = Now().Add(time.Minute)

	promoted, err := parts.Upsert(ctx, promote)
	require.NoError(t, err)
	require.NotNil(t, promoted.Before)
	assert.Equal(t, domain.RolePeeker, promoted.Before.Role)
	assert.Equal(t, domain.RoleParticipant, promoted.After.Role)
	assert.Nil(t, promoted.After.ExpiresAt)
	assert.Equal(t, 1, promoted.After.PeekCount)

	// 参加済みユーザーがチラ見を押しても降格しない(docs/05-database.md §3.4)。
	demote := peek
	demote.Now = Now().Add(2 * time.Minute)

	notDemoted, err := parts.Upsert(ctx, demote)
	require.NoError(t, err)
	assert.Equal(t, domain.RoleParticipant, notDemoted.After.Role)
	assert.Equal(t, 1, notDemoted.After.PeekCount)
	assert.Nil(t, notDemoted.After.ExpiresAt)

	byUser, err := parts.GetByUser(ctx, ev.ID, "U0456")
	require.NoError(t, err)
	assert.Equal(t, promoted.After.ID, byUser.ID)

	byID, err := parts.Get(ctx, promoted.After.ID)
	require.NoError(t, err)
	assert.Equal(t, "U0456", byID.ChatUserID)

	_, err = parts.GetByUser(ctx, ev.ID, "U9999")
	require.ErrorIs(t, err, domain.ErrNotFound)

	count, err := parts.CountActive(ctx, ev.ID, domain.RoleParticipant)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	require.NoError(t, parts.SetStatus(ctx, byUser.ID, domain.ParticipationLeft, Now().Add(time.Hour)))

	left, err := parts.Get(ctx, byUser.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ParticipationLeft, left.Status)
	assert.Nil(t, left.ExpiresAt)

	active, err := parts.ListByEvent(ctx, ev.ID, domain.ParticipationActive)
	require.NoError(t, err)
	assert.Len(t, active, 1)

	all, err := parts.ListByEvent(ctx, ev.ID)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	testConcurrentJoin(t, repo, ws.ID, ev.ID)
}

func testConcurrentJoin(t *testing.T, repo usecase.Repository, workspaceID, eventID string) {
	t.Helper()

	ctx := t.Context()
	parts := repo.Participations()
	join := usecase.ParticipationUpsert{
		EventID:     eventID,
		WorkspaceID: workspaceID,
		ChatUserID:  "U0789",
		DisplayName: "hanako",
		Role:        domain.RoleParticipant,
		Now:         Now(),
	}

	var (
		wg          sync.WaitGroup
		newlyJoined atomic.Int64
	)

	wg.Add(concurrentJoinCalls)

	for range concurrentJoinCalls {
		go func() {
			defer wg.Done()

			change, err := parts.Upsert(ctx, join)
			if err == nil && change.Before == nil {
				newlyJoined.Add(1)
			}
		}()
	}

	wg.Wait()

	// 連打しても「新規参加」と判定されるのは 1 回だけで、副作用も 1 回に収まる。
	assert.Equal(t, int64(1), newlyJoined.Load())

	count, err := parts.CountActive(ctx, eventID, domain.RoleParticipant)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func testJobs(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	jobs := repo.Jobs()
	ws := SeedWorkspace(t, repo, "T0123")
	ev := SeedEvent(t, repo, ws.ID, Now().Add(7*24*time.Hour))

	job, err := jobs.Enqueue(ctx, domain.NewReminderJob(ev.ID, 1, Now().Add(-time.Minute)), Now())
	require.NoError(t, err)
	assert.Equal(t, domain.JobPending, job.Status)
	assert.Equal(t, domain.DefaultMaxAttempts, job.MaxAttempts)
	assert.Equal(t, ev.ID, job.EventID)

	_, err = jobs.Enqueue(ctx, domain.NewReminderJob(ev.ID, 1, Now()), Now())
	require.ErrorIs(t, err, domain.ErrAlreadyExists)

	claimed, err := jobs.Claim(ctx, Now(), testLease)
	require.NoError(t, err)
	assert.Equal(t, job.ID, claimed.ID)
	assert.Equal(t, domain.JobRunning, claimed.Status)
	assert.Equal(t, 1, claimed.Attempts)
	require.NotNil(t, claimed.LeaseUntil)
	assert.Equal(t, Now().Add(testLease), *claimed.LeaseUntil)

	_, err = jobs.Claim(ctx, Now(), testLease)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// リースが切れたジョブは再取得できる。
	expired, err := jobs.Claim(ctx, Now().Add(testLease+time.Second), testLease)
	require.NoError(t, err)
	assert.Equal(t, job.ID, expired.ID)
	assert.Equal(t, 2, expired.Attempts)

	require.NoError(t, jobs.ExtendLease(ctx, job.ID, Now().Add(time.Hour), Now()))

	extended, err := jobs.Get(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, extended.LeaseUntil)
	assert.Equal(t, Now().Add(time.Hour), *extended.LeaseUntil)

	require.NoError(t, jobs.Retry(ctx, job.ID, Now().Add(time.Minute), "boom", Now()))

	retried, err := jobs.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobPending, retried.Status)
	assert.Equal(t, "boom", retried.LastError)
	assert.Nil(t, retried.LeaseUntil)

	require.NoError(t, jobs.Reschedule(ctx, job.ID, Now().Add(time.Hour), Now()))

	rescheduled, err := jobs.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, Now().Add(time.Hour), rescheduled.RunAt)
	// 実行していないため再試行回数は戻す。
	assert.Equal(t, 1, rescheduled.Attempts)

	require.NoError(t, jobs.Complete(ctx, job.ID, Now().Add(2*time.Hour)))

	done, err := jobs.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobDone, done.Status)
	require.NotNil(t, done.FinishedAt)

	failing, err := jobs.Enqueue(ctx, domain.NewArchiveChannelJob(ev.ID, Now()), Now())
	require.NoError(t, err)
	require.NoError(t, jobs.Fail(ctx, failing.ID, "permanent", Now()))

	failed, err := jobs.Get(ctx, failing.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobFailed, failed.Status)
	assert.Equal(t, "permanent", failed.LastError)

	require.ErrorIs(t, jobs.Complete(ctx, "66e0a1b2c3d4e5f607182930", Now()), domain.ErrNotFound)

	testCancelJobsByEvent(t, repo, ev.ID)
	testConcurrentClaim(t, repo, ev.ID)
}

func testCancelJobsByEvent(t *testing.T, repo usecase.Repository, eventID string) {
	t.Helper()

	ctx := t.Context()
	jobs := repo.Jobs()

	for seq := range 3 {
		_, err := jobs.Enqueue(ctx, domain.NewReminderJob(eventID, seq+10, Now().Add(time.Hour)), Now())
		require.NoError(t, err)
	}

	autoEnd, err := jobs.Enqueue(ctx, domain.NewAutoEndJob(eventID, Now().Add(time.Hour)), Now())
	require.NoError(t, err)

	canceled, err := jobs.CancelByEvent(ctx, eventID, []domain.JobKind{domain.JobReminder}, Now())
	require.NoError(t, err)
	assert.Equal(t, 3, canceled)

	pending, err := jobs.ListByEvent(ctx, eventID, domain.JobPending)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, autoEnd.ID, pending[0].ID)

	all, err := jobs.ListByEvent(ctx, eventID)
	require.NoError(t, err)
	assert.Len(t, all, 6)
}

func testConcurrentClaim(t *testing.T, repo usecase.Repository, eventID string) {
	t.Helper()

	ctx := t.Context()
	jobs := repo.Jobs()

	target, err := jobs.Enqueue(ctx, domain.NewPeekExpireJob(eventID, "p1", 1, Now().Add(-time.Minute)), Now())
	require.NoError(t, err)

	var (
		wg        sync.WaitGroup
		succeeded atomic.Int64
	)

	wg.Add(concurrentClaimers)

	for range concurrentClaimers {
		go func() {
			defer wg.Done()

			claimed, claimErr := jobs.Claim(ctx, Now(), testLease)
			switch {
			case claimErr == nil && claimed.ID == target.ID:
				succeeded.Add(1)
			case claimErr != nil && !errors.Is(claimErr, domain.ErrNotFound):
				t.Error(claimErr)
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int64(1), succeeded.Load())
}
