package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ドメイン層のセンチネルエラー。ユースケース層はこれらを gRPC ステータスへ変換する(docs/08-api.md §5)。
var (
	ErrNotFound                = errors.New("not found")
	ErrAlreadyExists           = errors.New("already exists")
	ErrEventClosed             = errors.New("event is closed")
	ErrForbidden               = errors.New("forbidden")
	ErrIdentityRequired        = errors.New("chat identity required")
	ErrIdentityLinkedElsewhere = errors.New("chat identity linked to another account")
	ErrProviderUnavailable     = errors.New("provider unavailable")
	ErrLinkTokenExpired        = errors.New("link token expired")
	ErrLinkTokenConsumed       = errors.New("link token already consumed")
	ErrOrganizerCannotLeave    = errors.New("organizer cannot leave")
)

// FieldError は 1 フィールドの検証エラー。Field は API のフィールドパス(`reminder_policy.every` など)。
type FieldError struct {
	Field   string
	Message string
}

// ValidationError は複数フィールドの検証エラーをまとめたもの。
type ValidationError struct {
	Fields []FieldError
}

// Error は全フィールドのエラーを連結した文字列を返す。
func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Field+": "+f.Message)
	}

	return "validation failed: " + strings.Join(parts, ", ")
}

// Has は指定したフィールドのエラーが含まれるかを返す。
func (e *ValidationError) Has(field string) bool {
	for _, f := range e.Fields {
		if f.Field == field {
			return true
		}
	}

	return false
}

func (e *ValidationError) addf(field, format string, args ...any) {
	e.Fields = append(e.Fields, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
}

func (e *ValidationError) err() error {
	if len(e.Fields) == 0 {
		return nil
	}

	return e
}

// merge は入れ子の検証エラーのフィールドを取り込む。検証エラー以外は field 単体のエラーとして記録する。
func (e *ValidationError) merge(field string, err error) {
	if err == nil {
		return
	}

	var verr *ValidationError
	if errors.As(err, &verr) {
		e.Fields = append(e.Fields, verr.Fields...)

		return
	}

	e.addf(field, "%s", err.Error())
}
