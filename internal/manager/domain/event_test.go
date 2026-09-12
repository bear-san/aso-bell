package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

const testWorkspaceID = "66e0a1b2c3d4e5f607182930"

func validEventParams() domain.NewEventParams {
	return domain.NewEventParams{
		WorkspaceID:     testWorkspaceID,
		Title:           " ボドゲ会 ",
		Description:     "18時集合",
		Location:        "渋谷",
		StartsAt:        time.Date(2026, time.September, 20, 19, 0, 0, 0, time.UTC),
		Organizer:       domain.ChatUserRef{UserID: "U0123", DisplayName: "kentaro"},
		OriginChannelID: "C0123",
		ReminderPolicy:  domain.DefaultReminderPolicy(),
		CreatedVia:      domain.CreatedViaChat,
		CreatedBy:       "U0123",
	}
}

func testNow() time.Time {
	return time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
}

func TestNewEvent(t *testing.T) {
	t.Parallel()

	ev, err := domain.NewEvent(validEventParams(), testNow())
	require.NoError(t, err)

	assert.Equal(t, "ボドゲ会", ev.Title)
	assert.Equal(t, domain.EventStatusOpen, ev.Status)
	assert.True(t, ev.IsOpen())
	assert.Nil(t, ev.EndsAt)
	assert.Equal(t, testNow(), ev.CreatedAt)
	assert.Equal(t, testNow(), ev.UpdatedAt)
}

func TestNewEventValidation(t *testing.T) {
	t.Parallel()

	past := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, time.September, 19, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		mutate    func(p *domain.NewEventParams)
		wantField string
	}{
		{
			name:      "empty title",
			mutate:    func(p *domain.NewEventParams) { p.Title = "   " },
			wantField: "title",
		},
		{
			name:      "long title",
			mutate:    func(p *domain.NewEventParams) { p.Title = strings.Repeat("あ", domain.TitleMaxLen+1) },
			wantField: "title",
		},
		{
			name: "long description",
			mutate: func(p *domain.NewEventParams) {
				p.Description = strings.Repeat("あ", domain.DescriptionMaxLen+1)
			},
			wantField: "description",
		},
		{
			name:      "long location",
			mutate:    func(p *domain.NewEventParams) { p.Location = strings.Repeat("あ", domain.LocationMaxLen+1) },
			wantField: "location",
		},
		{
			name:      "bad workspace id",
			mutate:    func(p *domain.NewEventParams) { p.WorkspaceID = "ws" },
			wantField: "workspace_id",
		},
		{
			name:      "no organizer",
			mutate:    func(p *domain.NewEventParams) { p.Organizer = domain.ChatUserRef{} },
			wantField: "organizer",
		},
		{
			name:      "starts in the past",
			mutate:    func(p *domain.NewEventParams) { p.StartsAt = past },
			wantField: "starts_at",
		},
		{
			name:      "ends before start",
			mutate:    func(p *domain.NewEventParams) { p.EndsAt = &later },
			wantField: "ends_at",
		},
		{
			name:      "unknown origin",
			mutate:    func(p *domain.NewEventParams) { p.CreatedVia = "api" },
			wantField: "created_via",
		},
		{
			name:      "invalid reminder policy",
			mutate:    func(p *domain.NewEventParams) { p.ReminderPolicy = domain.ReminderPolicy{Mode: "weekly"} },
			wantField: "reminder_policy.mode",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			params := validEventParams()
			tc.mutate(&params)

			ev, err := domain.NewEvent(params, testNow())
			require.Nil(t, ev)

			var verr *domain.ValidationError

			require.ErrorAs(t, err, &verr)
			assert.True(t, verr.Has(tc.wantField), "want field %q in %v", tc.wantField, verr.Fields)
			assert.Contains(t, verr.Error(), tc.wantField)
		})
	}
}

func TestEventApply(t *testing.T) {
	t.Parallel()

	ev, err := domain.NewEvent(validEventParams(), testNow())
	require.NoError(t, err)

	title := "ボドゲ会(延期)"
	startsAt := time.Date(2026, time.September, 27, 19, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(3 * time.Hour)
	updatedNow := testNow().Add(time.Hour)

	require.NoError(t, ev.Apply(domain.EventUpdate{
		Title:    &title,
		StartsAt: &startsAt,
		EndsAt:   &endsAt,
	}, updatedNow))

	assert.Equal(t, title, ev.Title)
	assert.Equal(t, startsAt, ev.StartsAt)
	require.NotNil(t, ev.EndsAt)
	assert.Equal(t, endsAt, *ev.EndsAt)
	assert.Equal(t, updatedNow, ev.UpdatedAt)
}

func TestEventApplyAllowsPastStart(t *testing.T) {
	t.Parallel()

	ev, err := domain.NewEvent(validEventParams(), testNow())
	require.NoError(t, err)

	past := testNow().Add(-time.Hour)
	require.NoError(t, ev.Apply(domain.EventUpdate{StartsAt: &past}, testNow()))
	assert.Equal(t, past, ev.StartsAt)
}

func TestEventApplyClearEndsAt(t *testing.T) {
	t.Parallel()

	params := validEventParams()
	endsAt := params.StartsAt.Add(2 * time.Hour)
	params.EndsAt = &endsAt

	ev, err := domain.NewEvent(params, testNow())
	require.NoError(t, err)
	require.NotNil(t, ev.EndsAt)

	require.NoError(t, ev.Apply(domain.EventUpdate{ClearEndsAt: true}, testNow()))
	assert.Nil(t, ev.EndsAt)
}

func TestEventApplyRejectsInvalidUpdate(t *testing.T) {
	t.Parallel()

	ev, err := domain.NewEvent(validEventParams(), testNow())
	require.NoError(t, err)

	before := *ev
	empty := ""
	applyErr := ev.Apply(domain.EventUpdate{Title: &empty}, testNow().Add(time.Hour))

	var verr *domain.ValidationError

	require.ErrorAs(t, applyErr, &verr)
	assert.True(t, verr.Has("title"))
	assert.Equal(t, before, *ev)
}

func TestEventApplyRejectsClosedEvent(t *testing.T) {
	t.Parallel()

	ev, err := domain.NewEvent(validEventParams(), testNow())
	require.NoError(t, err)
	require.True(t, ev.End(domain.EndReasonManual, testNow()))

	title := "変更"
	require.ErrorIs(t, ev.Apply(domain.EventUpdate{Title: &title}, testNow()), domain.ErrEventClosed)
}

func TestEventEndIsIdempotent(t *testing.T) {
	t.Parallel()

	ev, err := domain.NewEvent(validEventParams(), testNow())
	require.NoError(t, err)

	endedAt := testNow().Add(time.Hour)
	require.True(t, ev.End(domain.EndReasonManual, endedAt))
	assert.Equal(t, domain.EventStatusEnded, ev.Status)
	assert.Equal(t, domain.EndReasonManual, ev.EndReason)
	require.NotNil(t, ev.EndedAt)
	assert.Equal(t, endedAt, *ev.EndedAt)

	assert.False(t, ev.End(domain.EndReasonAuto, endedAt.Add(time.Hour)))
	assert.Equal(t, domain.EndReasonManual, ev.EndReason)
	assert.Equal(t, endedAt, *ev.EndedAt)
}

func TestEventCancel(t *testing.T) {
	t.Parallel()

	ev, err := domain.NewEvent(validEventParams(), testNow())
	require.NoError(t, err)

	require.True(t, ev.Cancel(domain.EndReasonProvisionFailed, testNow()))
	assert.Equal(t, domain.EventStatusCanceled, ev.Status)
	assert.False(t, ev.IsOpen())
	assert.False(t, ev.Cancel(domain.EndReasonCanceled, testNow()))
}

func TestEventAutoEndAt(t *testing.T) {
	t.Parallel()

	params := validEventParams()

	ev, err := domain.NewEvent(params, testNow())
	require.NoError(t, err)
	assert.Equal(t, params.StartsAt.Add(24*time.Hour), ev.AutoEndAt(24*time.Hour))

	endsAt := params.StartsAt.Add(2 * time.Hour)
	params.EndsAt = &endsAt

	withEnd, err := domain.NewEvent(params, testNow())
	require.NoError(t, err)
	assert.Equal(t, endsAt, withEnd.AutoEndAt(24*time.Hour))
}
