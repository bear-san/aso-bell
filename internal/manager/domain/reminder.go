package domain

import (
	"slices"
	"time"
)

// ReminderMode はリマインドの生成方式(docs/04-domain-model.md §2.4)。
type ReminderMode string

// リマインドの生成方式。
const (
	ReminderModeOffsets  ReminderMode = "offsets"
	ReminderModeInterval ReminderMode = "interval"
	ReminderModeNone     ReminderMode = "none"
)

const (
	// MaxReminders は 1 イベントあたりに生成できるリマインドの上限。
	MaxReminders = 30
	// MinReminderInterval は interval モードの最小間隔。
	MinReminderInterval = time.Hour
)

const oneDay = 24 * time.Hour

// 既定のリマインドポリシー(開始 1 週間前 / 前日 / 3 時間前)。
const (
	defaultOffsetWeek    = 7 * oneDay
	defaultOffsetDay     = oneDay
	defaultOffsetSameDay = 3 * time.Hour
)

// DefaultFinalOffset は interval モードで最後に送るリマインドの開始前オフセット既定値。
const DefaultFinalOffset = defaultOffsetSameDay

// ReminderPolicy はイベントのリマインド投稿ペース。
type ReminderPolicy struct {
	Mode        ReminderMode
	Offsets     []time.Duration
	Every       time.Duration
	At          TimeOfDay
	FinalOffset time.Duration
}

// DefaultReminderPolicy は新規イベントの既定ポリシーを返す。
func DefaultReminderPolicy() ReminderPolicy {
	return ReminderPolicy{
		Mode:    ReminderModeOffsets,
		Offsets: []time.Duration{defaultOffsetWeek, defaultOffsetDay, defaultOffsetSameDay},
	}
}

// Validate はポリシー単体の不変条件を検査する。生成件数の上限は開始日時に依存するため Schedule で検査する。
func (p ReminderPolicy) Validate(field string) error {
	v := &ValidationError{}

	switch p.Mode {
	case ReminderModeNone:
	case ReminderModeOffsets:
		p.validateOffsets(v, field)
	case ReminderModeInterval:
		p.validateInterval(v, field)
	default:
		v.addf(field+".mode", "unknown mode %q", string(p.Mode))
	}

	return v.err()
}

// Schedule はリマインドを投稿する時刻を昇順で返す。now 以前の時刻は含まない。
func (p ReminderPolicy) Schedule(now, startsAt time.Time, loc *time.Location) ([]time.Time, error) {
	if err := p.Validate("reminder_policy"); err != nil {
		return nil, err
	}

	var times []time.Time

	switch p.Mode {
	case ReminderModeNone:
		return nil, nil
	case ReminderModeOffsets:
		times = p.offsetTimes(now, startsAt)
	case ReminderModeInterval:
		times = p.intervalTimes(now, startsAt, loc)
	}

	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	times = slices.CompactFunc(times, time.Time.Equal)

	if len(times) > MaxReminders {
		v := &ValidationError{}
		v.addf("reminder_policy", "generates %d reminders, limit is %d", len(times), MaxReminders)

		return nil, v.err()
	}

	return times, nil
}

func (p ReminderPolicy) validateOffsets(v *ValidationError, field string) {
	if len(p.Offsets) == 0 {
		v.addf(field+".offsets", "must not be empty")

		return
	}

	if len(p.Offsets) > MaxReminders {
		v.addf(field+".offsets", "must not exceed %d entries", MaxReminders)
	}

	seen := make(map[time.Duration]bool, len(p.Offsets))

	for _, off := range p.Offsets {
		if off <= 0 {
			v.addf(field+".offsets", "must be positive, got %s", off)
		}

		if seen[off] {
			v.addf(field+".offsets", "duplicated entry %s", off)
		}

		seen[off] = true
	}
}

func (p ReminderPolicy) validateInterval(v *ValidationError, field string) {
	if p.Every < MinReminderInterval {
		v.addf(field+".every", "must be at least %s", MinReminderInterval)
	}

	if !p.At.Valid() {
		v.addf(field+".at", "must be HH:MM")
	}

	if p.FinalOffset < 0 {
		v.addf(field+".final_offset", "must not be negative")
	}
}

func (p ReminderPolicy) offsetTimes(now, startsAt time.Time) []time.Time {
	times := make([]time.Time, 0, len(p.Offsets))

	for _, off := range p.Offsets {
		if t := startsAt.Add(-off); t.After(now) {
			times = append(times, t)
		}
	}

	return times
}

func (p ReminderPolicy) intervalTimes(now, startsAt time.Time, loc *time.Location) []time.Time {
	final := startsAt.Add(-p.FinalOffset)

	var times []time.Time

	for t := p.firstInterval(now, loc); t.Before(final); t = advance(t, p.Every, loc) {
		if t.After(now) {
			times = append(times, t)
		}
		// 上限超過は Schedule で検査するため、無限ループを避ける分だけ生成したら打ち切る。
		if len(times) > MaxReminders {
			break
		}
	}

	if final.After(now) {
		times = append(times, final)
	}

	return times
}

func (p ReminderPolicy) firstInterval(now time.Time, loc *time.Location) time.Time {
	candidate := p.At.On(now, loc)
	if candidate.Before(now) {
		candidate = candidate.AddDate(0, 0, 1)
	}

	return candidate
}

// advance は間隔が 24 時間の倍数のときだけ暦日で進める。夏時間のある TZ でも `at` の壁時計時刻を保つため。
func advance(t time.Time, every time.Duration, loc *time.Location) time.Time {
	if every%oneDay == 0 {
		return t.In(loc).AddDate(0, 0, int(every/oneDay))
	}

	return t.Add(every)
}
