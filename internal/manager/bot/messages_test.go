package bot_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func testEvent() *domain.Event {
	endsAt := time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC)

	return &domain.Event{
		ID:               "66e0a1b2c3d4e5f607182930",
		Title:            "ボドゲ会",
		Description:      "初心者歓迎",
		Location:         "渋谷",
		StartsAt:         time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
		EndsAt:           &endsAt,
		Status:           domain.EventStatusOpen,
		Organizer:        domain.ChatUserRef{UserID: "U001", DisplayName: "けんたろ"},
		Channel:          domain.ChannelRef{ChannelID: "C100", Name: "ev-0920-ボドゲ会"},
		ParticipantCount: 3,
	}
}

func TestAnnouncement(t *testing.T) {
	t.Parallel()

	msg := bot.Announcement(testEvent(), time.Hour)

	assert.Contains(t, msg.GetText(), "🔔 **ボドゲ会** の参加者を募集中!")
	assert.Contains(t, msg.GetText(), "📅 {{time:1789898400}} 〜 {{time:1789909200}}")
	assert.Contains(t, msg.GetText(), "📍 渋谷")
	assert.Contains(t, msg.GetText(), "👤 主催: {{user:U001}}")
	assert.Contains(t, msg.GetText(), "🙋 参加者: 3 人")
	assert.Contains(t, msg.GetText(), "初心者歓迎")
	assert.Contains(t, msg.GetText(), "連絡は {{channel:C100}} で行います。")

	require.Len(t, msg.GetButtons(), 2)
	assert.Equal(t, "asobell:join:66e0a1b2c3d4e5f607182930", msg.GetButtons()[0].GetActionId())
	assert.Equal(t, asobellv1.ButtonStyle_BUTTON_STYLE_PRIMARY, msg.GetButtons()[0].GetStyle())
	assert.Equal(t, "チラ見(1時間)", msg.GetButtons()[1].GetLabel())
	assert.False(t, msg.GetButtons()[1].GetDisabled())
}

func TestAnnouncementWhenClosed(t *testing.T) {
	t.Parallel()

	ev := testEvent()
	ev.Status = domain.EventStatusCanceled

	msg := bot.Announcement(ev, time.Hour)

	assert.Contains(t, msg.GetText(), "🚫 中止になりました")
	assert.NotContains(t, msg.GetText(), "募集中")

	for _, button := range msg.GetButtons() {
		assert.True(t, button.GetDisabled())
	}
}

func TestAnnouncementEscapesUserContent(t *testing.T) {
	t.Parallel()

	ev := testEvent()
	ev.Title = "{{user:U999}} を呼ぶ会"
	ev.Description = ""
	ev.Location = ""

	msg := bot.Announcement(ev, time.Hour)

	assert.NotContains(t, msg.GetText(), "{{user:U999}}")
	assert.NotContains(t, msg.GetText(), "📍")
}

func TestSummary(t *testing.T) {
	t.Parallel()

	msg := bot.Summary(testEvent())

	assert.Contains(t, msg.GetText(), "📌 **ボドゲ会**")
	assert.Contains(t, msg.GetText(), "/asobell leave")
	assert.Empty(t, msg.GetButtons())
}

func TestReminder(t *testing.T) {
	t.Parallel()

	ev := testEvent()
	now := ev.StartsAt.Add(-3 * time.Hour)

	msg := bot.Reminder(ev, now, []string{"U001", "U002"})

	assert.Contains(t, msg.GetText(), "まであと 3 時間!")
	assert.Contains(t, msg.GetText(), "📍 渋谷")
	assert.Contains(t, msg.GetText(), "{{user:U001}} {{user:U002}}")
}

func TestRecruitReminderHasButtons(t *testing.T) {
	t.Parallel()

	ev := testEvent()
	msg := bot.RecruitReminder(ev, ev.StartsAt.Add(-7*24*time.Hour), time.Hour)

	assert.Contains(t, msg.GetText(), "まであと 7 日!参加者 3 人")
	assert.Len(t, msg.GetButtons(), 2)
}

func TestClosing(t *testing.T) {
	t.Parallel()

	ev := testEvent()
	ev.Status = domain.EventStatusEnded
	assert.Contains(t, bot.Closing(ev).GetText(), "✅ **ボドゲ会** は終了しました")

	ev.Status = domain.EventStatusCanceled
	assert.Contains(t, bot.Closing(ev).GetText(), "🚫 **ボドゲ会** は中止になりました")
}

func TestTransition(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		kind domain.TransitionKind
		want string
	}{
		{"参加", domain.TransitionJoined, "🙋 {{user:U002}} が参加しました!(参加者 4 人)"},
		{"チラ見", domain.TransitionPeeked, "👀 {{user:U002}} がチラ見中です({{time:1789210800}} まで)"},
		{"昇格", domain.TransitionPromoted, "🙋 {{user:U002}} がチラ見から参加に切り替えました!(参加者 4 人)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg := bot.Transition(tc.kind, "U002", 4, expiresAt)
			require.NotNil(t, msg)
			assert.Equal(t, tc.want, msg.GetText())
		})
	}

	assert.Nil(t, bot.Transition(domain.TransitionAlreadyParticipant, "U002", 4, expiresAt))
	assert.Nil(t, bot.Transition(domain.TransitionAlreadyPeeking, "U002", 4, expiresAt))
}

func TestLeftAndRemoved(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "👋 {{user:U002}} が抜けました(参加者 2 人)", bot.Left("U002", 2).GetText())
	assert.Equal(t, "👋 {{user:U002}} は主催者により参加を解除されました", bot.RemovedByOrganizer("U002").GetText())
}

func TestActionReply(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)

	assert.Contains(t, bot.ActionReply(domain.TransitionJoined, "C100", time.Time{}, time.Hour).GetText(),
		"参加しました! {{channel:C100}} で連絡を待ってね")
	assert.Contains(t, bot.ActionReply(domain.TransitionPeeked, "C100", expiresAt, time.Hour).GetText(),
		"{{channel:C100}} を 1時間 だけ覗けます({{time:1789210800}} まで)")
	assert.Equal(t, "すでに参加しています",
		bot.ActionReply(domain.TransitionAlreadyParticipant, "C100", time.Time{}, time.Hour).GetText())
	assert.Contains(t, bot.ActionReply(domain.TransitionAlreadyPeeking, "C100", expiresAt, time.Hour).GetText(),
		"チラ見中です({{time:1789210800}} まで)")
}

func TestLinkMessagesSuppressPreview(t *testing.T) {
	t.Parallel()

	invitation := bot.LinkInvitation("https://asobell.example/link/abc")
	assert.True(t, invitation.GetSuppressPreview())
	assert.Contains(t, invitation.GetText(), "10 分以内")
	assert.Contains(t, invitation.GetText(), "https://asobell.example/link/abc")

	linked := bot.AlreadyLinked("me@example.com", "https://asobell.example")
	assert.True(t, linked.GetSuppressPreview())
	assert.Contains(t, linked.GetText(), "すでに me@example.com と連携済みです")
}

func TestHelpListsEverySubcommand(t *testing.T) {
	t.Parallel()

	help := bot.Help().GetText()

	for _, sub := range []string{"new", "list", "recruit", "login", "unlink", "info", "edit", "remind", "end", "cancel", "leave"} {
		assert.Contains(t, help, "/asobell "+sub)
	}
}

func TestFormatRemaining(t *testing.T) {
	t.Parallel()

	tests := []struct {
		give time.Duration
		want string
	}{
		{7 * 24 * time.Hour, "7 日"},
		{47 * time.Hour, "1 日"},
		{23 * time.Hour, "23 時間"},
		{45 * time.Minute, "45 分"},
		{30 * time.Second, "まもなく"},
		{-time.Minute, "まもなく"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, bot.FormatRemaining(tc.give))
		})
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1時間", bot.FormatDuration(time.Hour))
	assert.Equal(t, "90分", bot.FormatDuration(90*time.Minute))
	assert.Equal(t, "1日間", bot.FormatDuration(24*time.Hour))
	assert.Equal(t, "30秒", bot.FormatDuration(30*time.Second))
}
