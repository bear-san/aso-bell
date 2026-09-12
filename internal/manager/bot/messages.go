package bot

import (
	"fmt"
	"strings"
	"time"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/shared/markup"
)

// ボタンのラベル(docs/07-bot-ux.md §3.1)。
const (
	labelJoin = "参加"
	labelPeek = "チラ見(%s)"
)

const hoursPerDay = 24

// Announcement は originChannel / 募集チャンネルへ投稿する立ち上げメッセージを組み立てる。
// 終了・中止後は先頭行を置き換え、ボタンを押せなくする(docs/07-bot-ux.md §3.1)。
func Announcement(ev *domain.Event, peek time.Duration) *asobellv1.Message {
	lines := []string{announcementHeader(ev)}
	lines = append(lines, scheduleLine(ev))
	lines = appendIfNotEmpty(lines, locationLine(ev))
	lines = append(lines, "👤 主催: "+markup.User(ev.Organizer.UserID))
	lines = append(lines, fmt.Sprintf("🙋 参加者: %d 人", ev.ParticipantCount))

	if ev.Description != "" {
		lines = append(lines, "", markup.Escape(ev.Description))
	}

	if ev.Channel.ChannelID != "" {
		lines = append(lines, "", "連絡は "+markup.Channel(ev.Channel.ChannelID)+" で行います。")
	}

	return &asobellv1.Message{
		Text:    strings.Join(lines, "\n"),
		Buttons: participationButtons(ev, peek),
	}
}

// Summary はイベントチャンネルにピン留めする概要メッセージを組み立てる(docs/07-bot-ux.md §3.2)。
func Summary(ev *domain.Event) *asobellv1.Message {
	lines := []string{"📌 " + markup.Bold(markup.Escape(ev.Title)), scheduleLine(ev)}
	lines = appendIfNotEmpty(lines, locationLine(ev))
	lines = append(lines, "👤 主催: "+markup.User(ev.Organizer.UserID))

	if ev.Description != "" {
		lines = append(lines, markup.Escape(ev.Description))
	}

	lines = append(lines,
		"",
		"主催者は `/asobell edit` `/asobell remind` `/asobell end` `/asobell cancel` で管理できます。",
		"参加をやめるときは `/asobell leave`。",
	)

	return &asobellv1.Message{Text: strings.Join(lines, "\n")}
}

// Reminder はイベントチャンネル向けのリマインドを組み立てる。参加者をメンションする(docs/07-bot-ux.md §3.5)。
func Reminder(ev *domain.Event, now time.Time, participantIDs []string) *asobellv1.Message {
	lines := []string{
		fmt.Sprintf("⏰ %s まであと %s!", markup.Bold(markup.Escape(ev.Title)), FormatRemaining(ev.StartsAt.Sub(now))),
		startLineWithLocation(ev),
	}

	if mentions := mentionList(participantIDs); mentions != "" {
		lines = append(lines, mentions)
	}

	return &asobellv1.Message{Text: strings.Join(lines, "\n")}
}

// RecruitReminder は募集チャンネル向けのリマインドを組み立てる。参加ボタンを添える。
func RecruitReminder(ev *domain.Event, now time.Time, peek time.Duration) *asobellv1.Message {
	lines := []string{
		fmt.Sprintf(
			"⏰ %s まであと %s!参加者 %d 人",
			markup.Bold(markup.Escape(ev.Title)),
			FormatRemaining(ev.StartsAt.Sub(now)),
			ev.ParticipantCount,
		),
		startLineWithLocation(ev),
		"👤 主催: " + markup.User(ev.Organizer.UserID),
	}

	return &asobellv1.Message{
		Text:    strings.Join(lines, "\n"),
		Buttons: participationButtons(ev, peek),
	}
}

// Closing はイベントチャンネルへ投稿する終了・中止メッセージを組み立てる(docs/07-bot-ux.md §3.6)。
func Closing(ev *domain.Event) *asobellv1.Message {
	title := markup.Bold(markup.Escape(ev.Title))

	text := fmt.Sprintf("✅ %s は終了しました。おつかれさまでした!このチャンネルはアーカイブされます。", title)
	if ev.Status == domain.EventStatusCanceled {
		text = fmt.Sprintf("🚫 %s は中止になりました。このチャンネルはアーカイブされます。", title)
	}

	return &asobellv1.Message{Text: text}
}

// Transition はイベントチャンネルへ投稿する参加状況の変化を組み立てる(docs/07-bot-ux.md §3.3)。
// チラ見の期限切れは投稿しないため、この関数は nil を返す。
func Transition(kind domain.TransitionKind, userID string, count int, expiresAt time.Time) *asobellv1.Message {
	user := markup.User(userID)

	switch kind {
	case domain.TransitionJoined:
		return text(fmt.Sprintf("🙋 %s が参加しました!(参加者 %d 人)", user, count))
	case domain.TransitionPeeked:
		return text(fmt.Sprintf("👀 %s がチラ見中です(%s まで)", user, markup.Time(expiresAt)))
	case domain.TransitionPromoted:
		return text(fmt.Sprintf("🙋 %s がチラ見から参加に切り替えました!(参加者 %d 人)", user, count))
	case domain.TransitionAlreadyParticipant, domain.TransitionAlreadyPeeking:
		return nil
	default:
		return nil
	}
}

// Left は退出メッセージを組み立てる。
func Left(userID string, count int) *asobellv1.Message {
	return text(fmt.Sprintf("👋 %s が抜けました(参加者 %d 人)", markup.User(userID), count))
}

// RemovedByOrganizer は WebConsole からの除外メッセージを組み立てる。
func RemovedByOrganizer(userID string) *asobellv1.Message {
	return text(fmt.Sprintf("👋 %s は主催者により参加を解除されました", markup.User(userID)))
}

// ActionReply はボタン押下に対する本人向けの返信を組み立てる(docs/07-bot-ux.md §3.4)。
func ActionReply(
	kind domain.TransitionKind,
	channelID string,
	expiresAt time.Time,
	peek time.Duration,
) *asobellv1.Message {
	channel := markup.Channel(channelID)

	switch kind {
	case domain.TransitionJoined, domain.TransitionPromoted:
		return text(fmt.Sprintf("参加しました! %s で連絡を待ってね", channel))
	case domain.TransitionPeeked:
		return text(fmt.Sprintf(
			"%s を %s だけ覗けます(%s まで)。気に入ったら「参加」を押してね",
			channel, FormatDuration(peek), markup.Time(expiresAt),
		))
	case domain.TransitionAlreadyParticipant:
		return text("すでに参加しています")
	case domain.TransitionAlreadyPeeking:
		return text(fmt.Sprintf("チラ見中です(%s まで)。参加するなら「参加」を押してね", markup.Time(expiresAt)))
	default:
		return text("処理しました")
	}
}

// LinkInvitation は `/asobell login` の返信を組み立てる。トークンを含むため ephemeral / DM 以外へ投稿しない。
func LinkInvitation(linkURL string) *asobellv1.Message {
	return &asobellv1.Message{
		Text: "🔗 WebConsole と連携するには、10 分以内に次の URL を開いてください" +
			"(この URL はあなた専用で一度だけ使えます)\n" + linkURL,
		SuppressPreview: true,
	}
}

// AlreadyLinked は連携済みユーザーへの返信を組み立てる。
func AlreadyLinked(email, consoleURL string) *asobellv1.Message {
	return &asobellv1.Message{
		Text: fmt.Sprintf(
			"✅ すでに %s と連携済みです。WebConsole: %s\n連携を解除するには /asobell unlink",
			markup.Escape(email), consoleURL,
		),
		SuppressPreview: true,
	}
}

// Help は使い方を組み立てる(docs/07-bot-ux.md §7)。
func Help() *asobellv1.Message {
	return text(strings.Join([]string{
		"あそベル の使い方",
		"/asobell new                … イベントを立ち上げる(このチャンネルに募集を投稿)",
		"/asobell list               … 募集中のイベント一覧",
		"/asobell recruit set|clear  … このチャンネルを募集チャンネルにする / 解除",
		"/asobell login              … WebConsole と連携する URL を受け取る",
		"/asobell unlink             … 連携を解除する",
		"-- イベントチャンネル内で --",
		"/asobell info               … イベント情報",
		"/asobell edit               … 内容を編集(主催者)",
		"/asobell remind 7d,1d,3h    … リマインド設定(主催者)",
		"/asobell remind every 2d 20:00",
		"/asobell remind off",
		"/asobell end                … 終了(主催者)",
		"/asobell cancel             … 中止(主催者)",
		"/asobell leave              … 参加をやめる",
	}, "\n"))
}

// EventInfo はイベント情報と参加者一覧を組み立てる(`/asobell info`)。
func EventInfo(ev *domain.Event, participants, peekers []string) *asobellv1.Message {
	lines := []string{
		"📌 " + markup.Bold(markup.Escape(ev.Title)),
		scheduleLine(ev),
	}
	lines = appendIfNotEmpty(lines, locationLine(ev))
	lines = append(lines, "👤 主催: "+markup.User(ev.Organizer.UserID))
	lines = appendIfNotEmpty(lines, markup.Escape(ev.Description))
	lines = append(lines, fmt.Sprintf("🙋 参加 %d 人", len(participants)))
	lines = appendIfNotEmpty(lines, mentionList(participants))

	if len(peekers) > 0 {
		lines = append(lines, fmt.Sprintf("👀 チラ見 %d 人", len(peekers)))
	}

	return text(strings.Join(lines, "\n"))
}

// EventList は募集中のイベント一覧を組み立てる(`/asobell list`)。
func EventList(events []*domain.Event, now time.Time) *asobellv1.Message {
	if len(events) == 0 {
		return text("募集中のイベントはありません。`/asobell new` で立ち上げてみてね")
	}

	lines := make([]string, 0, len(events)+1)
	lines = append(lines, "📋 募集中のイベント")

	for _, ev := range events {
		line := fmt.Sprintf(
			"・%s %s(あと %s / 参加 %d 人)",
			markup.Bold(markup.Escape(ev.Title)),
			markup.Time(ev.StartsAt),
			FormatRemaining(ev.StartsAt.Sub(now)),
			ev.ParticipantCount,
		)
		if ev.Channel.ChannelID != "" {
			line += " " + markup.Channel(ev.Channel.ChannelID)
		}

		lines = append(lines, line)
	}

	return text(strings.Join(lines, "\n"))
}

// Created はイベント作成の完了を本人へ知らせる文言を組み立てる(docs/07-bot-ux.md §2.2)。
func Created(ev *domain.Event) *asobellv1.Message {
	if ev.Channel.ChannelID == "" {
		return text(fmt.Sprintf("%s を作成しました", markup.Bold(markup.Escape(ev.Title))))
	}

	return text(fmt.Sprintf("作成しました %s", markup.Channel(ev.Channel.ChannelID)))
}

// Error は定型のエラー返信を組み立てる。
func Error(message string) *asobellv1.Message {
	return text(message)
}

// FormatRemaining は残り時間を最大単位 1 つで丸める(docs/07-bot-ux.md §3.5)。
func FormatRemaining(d time.Duration) string {
	switch {
	case d >= hoursPerDay*time.Hour:
		return fmt.Sprintf("%d 日", int(d.Hours())/hoursPerDay)
	case d >= time.Hour:
		return fmt.Sprintf("%d 時間", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%d 分", int(d.Minutes()))
	default:
		return "まもなく"
	}
}

// FormatDuration は「1 時間」「30 分」のように日本語の所要時間へ整形する。
func FormatDuration(d time.Duration) string {
	switch {
	case d%(hoursPerDay*time.Hour) == 0 && d >= hoursPerDay*time.Hour:
		return fmt.Sprintf("%d日間", int(d.Hours())/hoursPerDay)
	case d%time.Hour == 0 && d >= time.Hour:
		return fmt.Sprintf("%d時間", int(d.Hours()))
	case d%time.Minute == 0 && d >= time.Minute:
		return fmt.Sprintf("%d分", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d秒", int(d.Seconds()))
	}
}

func announcementHeader(ev *domain.Event) string {
	title := markup.Bold(markup.Escape(ev.Title))

	switch ev.Status {
	case domain.EventStatusEnded:
		return fmt.Sprintf("✅ 終了しました: %s", title)
	case domain.EventStatusCanceled:
		return fmt.Sprintf("🚫 中止になりました: %s", title)
	case domain.EventStatusOpen:
		return fmt.Sprintf("🔔 %s の参加者を募集中!", title)
	default:
		return fmt.Sprintf("🔔 %s の参加者を募集中!", title)
	}
}

func participationButtons(ev *domain.Event, peek time.Duration) []*asobellv1.Button {
	disabled := !ev.IsOpen()

	return []*asobellv1.Button{
		{
			ActionId: ActionID(domain.ActionJoin, ev.ID),
			Label:    labelJoin,
			Style:    asobellv1.ButtonStyle_BUTTON_STYLE_PRIMARY,
			Disabled: disabled,
		},
		{
			ActionId: ActionID(domain.ActionPeek, ev.ID),
			Label:    fmt.Sprintf(labelPeek, FormatDuration(peek)),
			Style:    asobellv1.ButtonStyle_BUTTON_STYLE_DEFAULT,
			Disabled: disabled,
		},
	}
}

func scheduleLine(ev *domain.Event) string {
	line := "📅 " + markup.Time(ev.StartsAt)
	if ev.EndsAt != nil {
		line += " 〜 " + markup.Time(*ev.EndsAt)
	}

	return line
}

func startLineWithLocation(ev *domain.Event) string {
	line := "📅 " + markup.Time(ev.StartsAt)
	if ev.Location != "" {
		line += " 📍 " + markup.Escape(ev.Location)
	}

	return line
}

func locationLine(ev *domain.Event) string {
	if ev.Location == "" {
		return ""
	}

	return "📍 " + markup.Escape(ev.Location)
}

func mentionList(userIDs []string) string {
	mentions := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		mentions = append(mentions, markup.User(id))
	}

	return strings.Join(mentions, " ")
}

func appendIfNotEmpty(lines []string, line string) []string {
	if line == "" {
		return lines
	}

	return append(lines, line)
}

func text(s string) *asobellv1.Message {
	return &asobellv1.Message{Text: s}
}
