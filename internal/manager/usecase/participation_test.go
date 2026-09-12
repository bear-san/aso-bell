package usecase_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

const guest = "U0GUEST"

func newParticipations(h *harness) *usecase.ParticipationService {
	return usecase.NewParticipationService(h.deps)
}

func apply(
	t *testing.T,
	h *harness,
	ev *domain.Event,
	action domain.ParticipationAction,
) usecase.ParticipationResult {
	t.Helper()

	res, err := newParticipations(h).Apply(t.Context(), usecase.ParticipationInput{
		EventID:     ev.ID,
		ChatUserID:  guest,
		DisplayName: "ゲスト",
		Action:      action,
	})
	require.NoError(t, err)

	return res
}

func TestParticipationTransitionTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		before   []domain.ParticipationAction
		action   domain.ParticipationAction
		wantKind domain.TransitionKind
		wantRole domain.ParticipationRole
	}{
		{"なし→参加", nil, domain.ActionJoin, domain.TransitionJoined, domain.RoleParticipant},
		{"なし→チラ見", nil, domain.ActionPeek, domain.TransitionPeeked, domain.RolePeeker},
		{
			"チラ見中→参加",
			[]domain.ParticipationAction{domain.ActionPeek},
			domain.ActionJoin,
			domain.TransitionPromoted,
			domain.RoleParticipant,
		},
		{
			"チラ見中→チラ見",
			[]domain.ParticipationAction{domain.ActionPeek},
			domain.ActionPeek,
			domain.TransitionAlreadyPeeking,
			domain.RolePeeker,
		},
		{
			"参加中→参加",
			[]domain.ParticipationAction{domain.ActionJoin},
			domain.ActionJoin,
			domain.TransitionAlreadyParticipant,
			domain.RoleParticipant,
		},
		{
			"参加中→チラ見",
			[]domain.ParticipationAction{domain.ActionJoin},
			domain.ActionPeek,
			domain.TransitionAlreadyParticipant,
			domain.RoleParticipant,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			ev := h.createEvent(t, testNow().Add(tenDays))

			for _, before := range tc.before {
				apply(t, h, ev, before)
			}

			res := apply(t, h, ev, tc.action)

			assert.Equal(t, tc.wantKind, res.Transition.Kind)
			require.NotNil(t, res.Participation)
			assert.Equal(t, tc.wantRole, res.Participation.Role)
			assert.Equal(t, domain.ParticipationActive, res.Participation.Status)
		})
	}
}

func TestJoinAddsMemberAndCountsUp(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	res := apply(t, h, ev, domain.ActionJoin)

	assert.Equal(t, 2, res.Event.ParticipantCount)
	assert.Zero(t, res.ExpiresAt)

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.Contains(t, channel.Members, guest)

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, stored.ParticipantCount)

	posts := h.fake.PostsIn(ev.Channel.ChannelID)
	require.NotEmpty(t, posts)
	assert.Contains(t, posts[len(posts)-1].Message.GetText(), "参加しました")
}

func TestPeekSchedulesExpireJob(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	res := apply(t, h, ev, domain.ActionPeek)

	assert.Equal(t, domain.RolePeeker, res.Participation.Role)
	assert.True(t, testNow().Add(h.workspac.Settings.PeekDuration).Equal(res.ExpiresAt))
	// チラ見は参加者数に数えない。
	assert.Equal(t, 1, res.Event.ParticipantCount)

	jobs := h.jobsOfKind(t, ev.ID, domain.JobPeekExpire, domain.JobPending)
	require.Len(t, jobs, 1)
	assert.True(t, res.ExpiresAt.Equal(jobs[0].RunAt))
	assert.Equal(t, res.Participation.ID, jobs[0].Payload.ParticipationID)
}

func TestPromoteCancelsPeekExpireJob(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	apply(t, h, ev, domain.ActionPeek)
	res := apply(t, h, ev, domain.ActionJoin)

	assert.Equal(t, domain.TransitionPromoted, res.Transition.Kind)
	assert.Nil(t, res.Participation.ExpiresAt)
	assert.Equal(t, 2, res.Event.ParticipantCount)

	assert.Empty(t, h.jobsOfKind(t, ev.ID, domain.JobPeekExpire, domain.JobPending))
	assert.Len(t, h.jobsOfKind(t, ev.ID, domain.JobPeekExpire, domain.JobCanceled), 1)
}

func TestRepeatedPressesHaveNoExtraEffect(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	apply(t, h, ev, domain.ActionJoin)
	before := len(h.fake.Calls())

	res := apply(t, h, ev, domain.ActionJoin)

	assert.Equal(t, domain.TransitionAlreadyParticipant, res.Transition.Kind)
	assert.Equal(t, 2, res.Event.ParticipantCount)
	assert.Len(t, h.fake.Calls(), before, "参加済みの再押下では Provider を呼ばない")
}

func TestConcurrentJoinCountsOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	svc := newParticipations(h)

	const pressers = 8

	var wg sync.WaitGroup

	joined := make([]domain.TransitionKind, pressers)

	for i := range pressers {
		wg.Go(func() {
			res, err := svc.Apply(t.Context(), usecase.ParticipationInput{
				EventID:    ev.ID,
				ChatUserID: guest,
				Action:     domain.ActionJoin,
			})
			if assert.NoError(t, err) {
				joined[i] = res.Transition.Kind
			}
		})
	}

	wg.Wait()

	var newly int

	for _, kind := range joined {
		if kind == domain.TransitionJoined {
			newly++
		}
	}

	assert.Equal(t, 1, newly, "新規参加として扱われるのは 1 回だけ")

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, stored.ParticipantCount)
}

func TestLeaveRemovesMemberAndCountsDown(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	apply(t, h, ev, domain.ActionJoin)

	part, err := newParticipations(h).Leave(t.Context(), ev.ID, guest)
	require.NoError(t, err)
	assert.Equal(t, domain.ParticipationLeft, part.Status)

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.NotContains(t, channel.Members, guest)

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, stored.ParticipantCount)

	posts := h.fake.PostsIn(ev.Channel.ChannelID)
	assert.Contains(t, posts[len(posts)-1].Message.GetText(), "抜けました")
}

func TestOrganizerCannotLeave(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	_, err := newParticipations(h).Leave(t.Context(), ev.ID, organizer)

	require.ErrorIs(t, err, domain.ErrOrganizerCannotLeave)
}

func TestLeaveRequiresActiveParticipation(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	_, err := newParticipations(h).Leave(t.Context(), ev.ID, guest)

	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRemoveByConsole(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	apply(t, h, ev, domain.ActionPeek)

	svc := newParticipations(h)
	require.NoError(t, svc.Remove(t.Context(), ev.ID, guest))

	part, err := h.repo.Participations().GetByUser(t.Context(), ev.ID, guest)
	require.NoError(t, err)
	assert.Equal(t, domain.ParticipationRemoved, part.Status)

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.NotContains(t, channel.Members, guest)

	// チラ見は参加者数に含まれないため、除外しても減らない。
	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, stored.ParticipantCount)

	// 二重除外は何もしない。
	before := len(h.fake.Calls())
	require.NoError(t, svc.Remove(t.Context(), ev.ID, guest))
	assert.Len(t, h.fake.Calls(), before)
}

func TestRemoveRejectsOfflineProvider(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	apply(t, h, ev, domain.ActionJoin)

	h.status.state.Status = domain.ProviderStatusOffline

	err := newParticipations(h).Remove(t.Context(), ev.ID, guest)

	require.ErrorIs(t, err, domain.ErrProviderUnavailable)
}

func TestApplyRejectsClosedEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	_, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{ChatUserID: organizer},
	})
	require.NoError(t, err)

	_, err = newParticipations(h).Apply(t.Context(), usecase.ParticipationInput{
		EventID:    ev.ID,
		ChatUserID: guest,
		Action:     domain.ActionJoin,
	})

	require.ErrorIs(t, err, domain.ErrEventClosed)
}

func TestExpirePeekRemovesMember(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	res := apply(t, h, ev, domain.ActionPeek)

	h.clock.Advance(h.workspac.Settings.PeekDuration + time.Minute)

	part, expired, err := newParticipations(h).ExpirePeek(t.Context(), res.Participation.ID)
	require.NoError(t, err)
	assert.True(t, expired)
	assert.Equal(t, domain.ParticipationExpired, part.Status)

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.NotContains(t, channel.Members, guest)
}

func TestExpirePeekIsNoOpBeforeDeadline(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	res := apply(t, h, ev, domain.ActionPeek)

	_, expired, err := newParticipations(h).ExpirePeek(t.Context(), res.Participation.ID)
	require.NoError(t, err)
	assert.False(t, expired)

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.Contains(t, channel.Members, guest)
}

func TestExpirePeekIsNoOpAfterPromotion(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	res := apply(t, h, ev, domain.ActionPeek)
	apply(t, h, ev, domain.ActionJoin)

	h.clock.Advance(h.workspac.Settings.PeekDuration + time.Minute)

	part, expired, err := newParticipations(h).ExpirePeek(t.Context(), res.Participation.ID)
	require.NoError(t, err)
	assert.False(t, expired)
	assert.Equal(t, domain.RoleParticipant, part.Role)

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.Contains(t, channel.Members, guest)
}

func TestExpirePeekIgnoresMissingParticipation(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, expired, err := newParticipations(h).ExpirePeek(t.Context(), "66e0a1b2c3d4e5f607182930")

	require.NoError(t, err)
	assert.False(t, expired)
}

func TestRejoinAfterExpireIsTreatedAsNew(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	first := apply(t, h, ev, domain.ActionPeek)

	h.clock.Advance(h.workspac.Settings.PeekDuration + time.Minute)

	_, expired, err := newParticipations(h).ExpirePeek(t.Context(), first.Participation.ID)
	require.NoError(t, err)
	require.True(t, expired)

	res := apply(t, h, ev, domain.ActionPeek)

	assert.Equal(t, domain.TransitionPeeked, res.Transition.Kind)
	assert.Equal(t, domain.ParticipationActive, res.Participation.Status)
	// peekCount が変わるため dedupeKey が衝突せず、新しい期限切れジョブが積まれる。
	jobs := h.jobsOfKind(t, ev.ID, domain.JobPeekExpire, domain.JobPending)
	require.Len(t, jobs, 2)
	assert.True(t, res.Participation.ExpiresAt.Equal(jobs[1].RunAt))
}
