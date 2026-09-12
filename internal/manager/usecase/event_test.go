package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

const (
	day       = 24 * time.Hour
	tenDays   = 10 * day
	organizer = "U0123"
)

func TestCreateEventProvisionsChannelAndMessages(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	assert.Equal(t, domain.EventStatusOpen, ev.Status)
	assert.Equal(t, 1, ev.ParticipantCount)
	require.NotEmpty(t, ev.Channel.ChannelID)
	assert.Contains(t, ev.Channel.Name, "ev-0923")

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.True(t, channel.IsPrivate)
	assert.Contains(t, channel.Members, organizer)

	require.NotNil(t, ev.Messages.Announcement)
	require.NotNil(t, ev.Messages.Summary)
	assert.Nil(t, ev.Messages.RecruitAnnouncement)
	assert.Equal(t, "C0ORIGIN", ev.Messages.Announcement.ChannelID)
	assert.Equal(t, ev.Channel.ChannelID, ev.Messages.Summary.ChannelID)

	for _, post := range h.fake.Posts() {
		assert.True(t, post.Pinned, "立ち上げ・概要メッセージはピン留めする")
	}

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, ev.Channel.ChannelID, stored.Channel.ChannelID)
	require.NotNil(t, stored.Messages.Summary)

	part, err := h.repo.Participations().GetByUser(t.Context(), ev.ID, organizer)
	require.NoError(t, err)
	assert.Equal(t, domain.RoleParticipant, part.Role)

	// 既定ポリシーは 7 日前 / 1 日前 / 3 時間前。開始まで 10 日あるためすべて未来。
	assert.Len(t, h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobPending), 3)
	assert.Len(t, h.jobsOfKind(t, ev.ID, domain.JobEventAutoEnd, domain.JobPending), 1)
}

func TestCreateEventPostsToRecruitChannel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.updateSettings(t, func(s *domain.WorkspaceSettings) { s.RecruitChannelID = "C0RECRUIT" })

	ev := h.createEvent(t, testNow().Add(tenDays))

	require.NotNil(t, ev.Messages.RecruitAnnouncement)
	assert.Equal(t, "C0RECRUIT", ev.Messages.RecruitAnnouncement.ChannelID)

	posts := h.fake.PostsIn("C0RECRUIT")
	require.Len(t, posts, 1)
	// 募集チャンネルの告知はピン留めしない。
	assert.False(t, posts[0].Pinned)
}

func TestCreateEventSkipsRecruitPostWhenSameAsOrigin(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.updateSettings(t, func(s *domain.WorkspaceSettings) { s.RecruitChannelID = "C0ORIGIN" })

	ev := h.createEvent(t, testNow().Add(tenDays))

	assert.Nil(t, ev.Messages.RecruitAnnouncement)
	assert.Len(t, h.fake.PostsIn("C0ORIGIN"), 1)
}

func TestCreateEventRejectedWhenProviderOffline(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.status.state.Status = domain.ProviderStatusOffline

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.ErrorIs(t, err, domain.ErrProviderUnavailable)
}

func TestCreateEventRetriesTakenChannelName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.fake.FailNext("CreatePrivateChannel", rpcerr.New(codes.AlreadyExists, rpcerr.ReasonNameTaken, "taken", nil))

	ev := h.createEvent(t, testNow().Add(tenDays))

	assert.Len(t, h.fake.CallsOf("CreatePrivateChannel"), 2)
	assert.Contains(t, ev.Channel.Name, "-2")
}

func TestCreateEventCancelsWhenProvisionFails(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.fake.FailNext("PostMessage", rpcerr.New(codes.PermissionDenied, rpcerr.ReasonBotPermission, "no perm", nil))

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.ErrorIs(t, err, usecase.ErrBotPermission)

	page, err := h.repo.Events().List(t.Context(), usecase.EventFilter{})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)

	failed := page.Events[0]
	assert.Equal(t, domain.EventStatusCanceled, failed.Status)
	assert.Equal(t, domain.EndReasonProvisionFailed, failed.EndReason)
	// チャンネルは作成済みなので後片付けをアーカイブジョブに任せる。
	assert.Len(t, h.jobsOfKind(t, failed.ID, domain.JobArchiveChannel, domain.JobPending), 1)
}

func TestCreateEventRejectsPastStart(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(-time.Hour)))
	require.Error(t, err)
	assert.Empty(t, h.fake.CallsOf("CreatePrivateChannel"))
}

func TestUpdateEventRefreshesMessagesAndReminders(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	before := h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobPending)
	require.Len(t, before, 3)

	title := "ボドゲ会(場所変更)"
	policy := domain.ReminderPolicy{Mode: domain.ReminderModeOffsets, Offsets: []time.Duration{day}}

	updated, err := h.events.Update(t.Context(), usecase.UpdateEventInput{
		EventID: ev.ID,
		Update:  domain.EventUpdate{Title: &title, ReminderPolicy: &policy},
		Actor:   usecase.Actor{ChatUserID: organizer},
	})
	require.NoError(t, err)
	assert.Equal(t, title, updated.Title)

	after := h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobPending)
	require.Len(t, after, 1, "取り消したあとに新しいペースで積み直す")
	assert.Equal(t, updated.StartsAt.Add(-day), after[0].RunAt)

	canceled := h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobCanceled)
	assert.Len(t, canceled, 3)

	posts := h.fake.PostsIn("C0ORIGIN")
	require.Len(t, posts, 1)
	assert.Contains(t, posts[0].Message.GetText(), title)
}

func TestUpdateEventDeniedForNonOrganizer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	title := "乗っ取り"
	_, err := h.events.Update(t.Context(), usecase.UpdateEventInput{
		EventID: ev.ID,
		Update:  domain.EventUpdate{Title: &title},
		Actor:   usecase.Actor{ChatUserID: "U9999"},
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestUpdateEventAllowedFromConsole(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	title := "運営が修正"
	updated, err := h.events.Update(t.Context(), usecase.UpdateEventInput{
		EventID: ev.ID,
		Update:  domain.EventUpdate{Title: &title},
		Actor:   usecase.Actor{Console: true},
	})
	require.NoError(t, err)
	assert.Equal(t, title, updated.Title)
}

func TestCloseEventEndsAndSchedulesArchive(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	ended, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{ChatUserID: organizer},
	})
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusEnded, ended.Status)

	assert.Empty(t, h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobPending))
	assert.Empty(t, h.jobsOfKind(t, ev.ID, domain.JobEventAutoEnd, domain.JobPending))
	assert.Len(t, h.jobsOfKind(t, ev.ID, domain.JobArchiveChannel, domain.JobPending), 1)

	announcement := h.fake.PostsIn("C0ORIGIN")
	require.Len(t, announcement, 1)
	assert.Contains(t, announcement[0].Message.GetText(), "終了しました")
	assert.False(t, announcement[0].Pinned)

	for _, button := range announcement[0].Message.GetButtons() {
		assert.True(t, button.GetDisabled())
	}

	closing := h.fake.PostsIn(ev.Channel.ChannelID)
	require.Len(t, closing, 2)
	assert.Contains(t, closing[1].Message.GetText(), "おつかれさまでした")
}

func TestCloseEventIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	in := usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{ChatUserID: organizer},
	}

	_, err := h.events.Close(t.Context(), in)
	require.NoError(t, err)

	posts := len(h.fake.Posts())

	again, err := h.events.Close(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusEnded, again.Status)
	assert.Len(t, h.fake.Posts(), posts, "2 回目は副作用を起こさない")
}

func TestCancelEventPostsCanceledNotice(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	canceled, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonCanceled,
		Actor:   usecase.Actor{ChatUserID: organizer},
	})
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusCanceled, canceled.Status)

	closing := h.fake.PostsIn(ev.Channel.ChannelID)
	require.Len(t, closing, 2)
	assert.Contains(t, closing[1].Message.GetText(), "中止になりました")
}
