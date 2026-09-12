package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	maxHour         = 23
	maxMinute       = 59
	timeOfDayParts  = 2
	timeOfDayDigits = 2
)

// TimeOfDay は `HH:MM` 形式の時刻。ワークスペースのタイムゾーンで解釈する。
type TimeOfDay struct {
	Hour   int
	Minute int
}

// ParseTimeOfDay は `19:00` のような 24 時間表記を解釈する。
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != timeOfDayParts || len(parts[0]) != timeOfDayDigits || len(parts[1]) != timeOfDayDigits {
		return TimeOfDay{}, fmt.Errorf("parse time of day %q: want HH:MM", s)
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return TimeOfDay{}, fmt.Errorf("parse time of day %q: %w", s, err)
	}

	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return TimeOfDay{}, fmt.Errorf("parse time of day %q: %w", s, err)
	}

	tod := TimeOfDay{Hour: hour, Minute: minute}
	if !tod.Valid() {
		return TimeOfDay{}, fmt.Errorf("parse time of day %q: out of range", s)
	}

	return tod, nil
}

// Valid は時・分が範囲内かを返す。
func (t TimeOfDay) Valid() bool {
	return t.Hour >= 0 && t.Hour <= maxHour && t.Minute >= 0 && t.Minute <= maxMinute
}

// String は `HH:MM` 形式に整形する。
func (t TimeOfDay) String() string {
	return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
}

// On は指定した日の同じ暦日における、この時刻の time.Time を loc で返す。
func (t TimeOfDay) On(day time.Time, loc *time.Location) time.Time {
	local := day.In(loc)

	return time.Date(local.Year(), local.Month(), local.Day(), t.Hour, t.Minute, 0, 0, loc)
}
