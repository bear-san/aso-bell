package bot

import (
	"fmt"
	"strings"
	"time"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// フォーム ID。Provider はこの ID をそのまま FormSubmission.form_id で返す。
const (
	FormEventNew  = "event_new"
	FormEventEdit = "event_edit"
)

// フォームのフィールド ID(docs/07-bot-ux.md §2.1)。
const (
	FieldTitle       = "title"
	FieldDate        = "date"
	FieldTime        = "time"
	FieldEnds        = "ends"
	FieldDescription = "description"
	FieldRemind      = "remind"
)

// リマインド選択肢の値。`custom` は「後で /asobell remind で設定する」を意味し、ポリシーを変更しない。
const (
	RemindPresetStandard  = "standard"
	RemindPresetDayBefore = "day_before"
	RemindPresetDaily20   = "daily_20"
	RemindPresetNone      = "none"
	RemindPresetCustom    = "custom"
)

const (
	dateLayout      = "2006-01-02"
	timeLayout      = "15:04"
	remindDailyHour = 20
	oneDay          = hoursPerDay * time.Hour
)

// EventFormInput はフォーム送信の解析結果。ReminderPolicy が nil ならポリシーを変更しない。
type EventFormInput struct {
	Title          string
	Description    string
	StartsAt       time.Time
	EndsAt         *time.Time
	ReminderPolicy *domain.ReminderPolicy
}

// NewEventForm は `/asobell new` で開くフォームを組み立てる。初期値は「明日の既定開始時刻」。
// metadata には実行チャンネル(originChannel)を載せ、送信時にそのまま返してもらう。
func NewEventForm(now time.Time, settings domain.WorkspaceSettings, originChannelID string) (*asobellv1.Form, error) {
	loc, err := settings.Location()
	if err != nil {
		return nil, fmt.Errorf("workspace timezone: %w", err)
	}

	tomorrow := now.In(loc).AddDate(0, 0, 1)

	return &asobellv1.Form{
		FormId:      FormEventNew,
		Title:       "イベントを立ち上げる",
		SubmitLabel: "作成",
		Metadata:    originChannelID,
		Fields: []*asobellv1.FormField{
			titleField(""),
			dateField(tomorrow.Format(dateLayout)),
			timeField(settings.DefaultStartTime.String()),
			endsField(""),
			descriptionField(""),
			remindField(RemindPresetStandard),
		},
	}, nil
}

// EditEventForm は `/asobell edit` で開くフォームを組み立てる。現在値を初期値に入れる。
func EditEventForm(ev *domain.Event, settings domain.WorkspaceSettings) (*asobellv1.Form, error) {
	loc, err := settings.Location()
	if err != nil {
		return nil, fmt.Errorf("workspace timezone: %w", err)
	}

	startsAt := ev.StartsAt.In(loc)

	ends := ""
	if ev.EndsAt != nil {
		ends = ev.EndsAt.In(loc).Format(timeLayout)
	}

	return &asobellv1.Form{
		FormId:      FormEventEdit,
		Title:       "イベントを編集する",
		SubmitLabel: "保存",
		Metadata:    ev.ID,
		Fields: []*asobellv1.FormField{
			titleField(ev.Title),
			dateField(startsAt.Format(dateLayout)),
			timeField(startsAt.Format(timeLayout)),
			endsField(ends),
			descriptionField(ev.Description),
			remindField(RemindPresetCustom),
		},
	}, nil
}

// ParseEventForm はフォームの入力値を検証して解析する。
// 第 2 戻り値はフィールド ID ごとのエラーで、空でなければ Reply.form_errors としてそのまま返す。
func ParseEventForm(
	values map[string]string,
	now time.Time,
	settings domain.WorkspaceSettings,
) (EventFormInput, map[string]string) {
	errs := map[string]string{}

	loc, err := settings.Location()
	if err != nil {
		loc = time.UTC
	}

	in := EventFormInput{
		Title:       strings.TrimSpace(values[FieldTitle]),
		Description: strings.TrimSpace(values[FieldDescription]),
	}

	if in.Title == "" {
		errs[FieldTitle] = "タイトルを入力してください"
	}

	startsAt, ok := parseFormStart(values, now, loc, settings, errs)
	if ok {
		in.StartsAt = startsAt
		in.EndsAt = parseFormEnds(values, startsAt, loc, errs)
	}

	if policy, preset := parseRemindPreset(values[FieldRemind], settings); preset {
		in.ReminderPolicy = policy
	} else {
		errs[FieldRemind] = "リマインドの選択肢が不正です"
	}

	return in, errs
}

func parseFormStart(
	values map[string]string,
	now time.Time,
	loc *time.Location,
	settings domain.WorkspaceSettings,
	errs map[string]string,
) (time.Time, bool) {
	date := strings.TrimSpace(values[FieldDate])
	if date == "" {
		errs[FieldDate] = "日付を選んでください"

		return time.Time{}, false
	}

	clock := strings.TrimSpace(values[FieldTime])
	if clock == "" {
		clock = settings.DefaultStartTime.String()
	}

	startsAt, err := ParseStartTime(date+" "+clock, now, loc, settings.DefaultStartTime)
	if err != nil {
		errs[FieldDate] = "日付または時刻を解釈できませんでした"

		return time.Time{}, false
	}

	if !startsAt.After(now) {
		errs[FieldDate] = "開始日時は未来を指定してください"

		return time.Time{}, false
	}

	return startsAt, true
}

func parseFormEnds(
	values map[string]string,
	startsAt time.Time,
	loc *time.Location,
	errs map[string]string,
) *time.Time {
	raw := strings.TrimSpace(values[FieldEnds])
	if raw == "" {
		return nil
	}

	endsAt, err := ParseEndTime(raw, startsAt, loc)
	if err != nil {
		errs[FieldEnds] = "終了予定は `+3h` `22:00` `9/21 12:00` のいずれかの形式で入力してください"

		return nil
	}

	return &endsAt
}

// parseRemindPreset は選択肢をポリシーへ変換する。`custom` は nil(変更しない)を返す。
func parseRemindPreset(value string, settings domain.WorkspaceSettings) (*domain.ReminderPolicy, bool) {
	switch strings.TrimSpace(value) {
	case "", RemindPresetCustom:
		return nil, true
	case RemindPresetStandard:
		policy := settings.DefaultReminderPolicy
		if policy.Mode == "" {
			policy = domain.DefaultReminderPolicy()
		}

		return &policy, true
	case RemindPresetDayBefore:
		return &domain.ReminderPolicy{
			Mode:    domain.ReminderModeOffsets,
			Offsets: []time.Duration{oneDay, 3 * time.Hour},
		}, true
	case RemindPresetDaily20:
		return &domain.ReminderPolicy{
			Mode:        domain.ReminderModeInterval,
			Every:       oneDay,
			At:          domain.TimeOfDay{Hour: remindDailyHour},
			FinalOffset: domain.DefaultFinalOffset,
		}, true
	case RemindPresetNone:
		return &domain.ReminderPolicy{Mode: domain.ReminderModeNone}, true
	default:
		return nil, false
	}
}

func titleField(initial string) *asobellv1.FormField {
	return &asobellv1.FormField{
		Id:           FieldTitle,
		Label:        "タイトル",
		Type:         asobellv1.FieldType_FIELD_TYPE_TEXT,
		Required:     true,
		MaxLength:    domain.TitleMaxLen,
		InitialValue: initial,
	}
}

func dateField(initial string) *asobellv1.FormField {
	return &asobellv1.FormField{
		Id:           FieldDate,
		Label:        "日付",
		Type:         asobellv1.FieldType_FIELD_TYPE_DATE,
		Required:     true,
		InitialValue: initial,
	}
}

func timeField(initial string) *asobellv1.FormField {
	return &asobellv1.FormField{
		Id:           FieldTime,
		Label:        "開始時刻",
		Type:         asobellv1.FieldType_FIELD_TYPE_TIME,
		Required:     true,
		InitialValue: initial,
	}
}

func endsField(initial string) *asobellv1.FormField {
	return &asobellv1.FormField{
		Id:           FieldEnds,
		Label:        "終了予定",
		Type:         asobellv1.FieldType_FIELD_TYPE_TEXT,
		Placeholder:  "+3h / 22:00 / 9/21 12:00",
		Hint:         "空欄なら未定。開始からの相対でも指定できます",
		InitialValue: initial,
	}
}

func descriptionField(initial string) *asobellv1.FormField {
	return &asobellv1.FormField{
		Id:           FieldDescription,
		Label:        "説明",
		Type:         asobellv1.FieldType_FIELD_TYPE_MULTILINE_TEXT,
		MaxLength:    domain.DescriptionMaxLen,
		InitialValue: initial,
	}
}

func remindField(initial string) *asobellv1.FormField {
	return &asobellv1.FormField{
		Id:           FieldRemind,
		Label:        "リマインド",
		Type:         asobellv1.FieldType_FIELD_TYPE_SELECT,
		InitialValue: initial,
		Options: []*asobellv1.SelectOption{
			{Value: RemindPresetStandard, Label: "標準(7日前/1日前/3時間前)"},
			{Value: RemindPresetDayBefore, Label: "前日と3時間前"},
			{Value: RemindPresetDaily20, Label: "毎日20時"},
			{Value: RemindPresetNone, Label: "なし"},
			{Value: RemindPresetCustom, Label: "カスタム(後で /asobell remind)"},
		},
	}
}
