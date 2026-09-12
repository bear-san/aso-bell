package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestJobConstructorsSetDedupeKeys(t *testing.T) {
	t.Parallel()

	runAt := testNow()
	eventID := testWorkspaceID

	reminder := domain.NewReminderJob(eventID, 3, runAt)
	assert.Equal(t, domain.JobReminder, reminder.Kind)
	assert.Equal(t, "reminder:"+eventID+":3", reminder.DedupeKey)
	assert.Equal(t, 3, reminder.Payload.Sequence)
	assert.Equal(t, runAt, reminder.Payload.ScheduledFor)
	assert.Equal(t, domain.JobPending, reminder.Status)
	assert.Equal(t, domain.DefaultMaxAttempts, reminder.MaxAttempts)

	peek := domain.NewPeekExpireJob(eventID, "p1", 2, runAt)
	assert.Equal(t, domain.JobPeekExpire, peek.Kind)
	assert.Equal(t, "peek:p1:2", peek.DedupeKey)
	assert.Equal(t, "p1", peek.Payload.ParticipationID)

	autoEnd := domain.NewAutoEndJob(eventID, runAt)
	assert.Equal(t, domain.JobEventAutoEnd, autoEnd.Kind)
	assert.Equal(t, "auto_end:"+eventID, autoEnd.DedupeKey)

	archive := domain.NewArchiveChannelJob(eventID, runAt)
	assert.Equal(t, domain.JobArchiveChannel, archive.Kind)
	assert.Equal(t, "archive:"+eventID, archive.DedupeKey)
}

func TestBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{attempts: 0, want: 30 * time.Second},
		{attempts: 1, want: 30 * time.Second},
		{attempts: 2, want: time.Minute},
		{attempts: 3, want: 2 * time.Minute},
		{attempts: 4, want: 4 * time.Minute},
		{attempts: 5, want: 8 * time.Minute},
		{attempts: 6, want: 16 * time.Minute},
		{attempts: 7, want: 30 * time.Minute},
		{attempts: 20, want: 30 * time.Minute},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, domain.Backoff(tc.attempts), "attempts=%d", tc.attempts)
	}
}

func TestJobExhausted(t *testing.T) {
	t.Parallel()

	job := domain.NewAutoEndJob(testWorkspaceID, testNow())
	job.Attempts = domain.DefaultMaxAttempts - 1
	assert.False(t, job.Exhausted())

	job.Attempts = domain.DefaultMaxAttempts
	assert.True(t, job.Exhausted())
}
