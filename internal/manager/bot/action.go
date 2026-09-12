package bot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// ErrUnknownAction は解釈できない action_id を表す。
var ErrUnknownAction = errors.New("unknown action")

// actionPrefix はボタンの action_id / custom_id の名前空間(docs/16-grpc.md common.proto Button)。
const actionPrefix = "asobell"

const actionIDParts = 3

// ActionID は `asobell:<action>:<eventId>` 形式のボタン ID を組み立てる。
func ActionID(action domain.ParticipationAction, eventID string) string {
	return strings.Join([]string{actionPrefix, string(action), eventID}, ":")
}

// ParseActionID はボタン ID から操作とイベント ID を取り出す。
func ParseActionID(id string) (domain.ParticipationAction, string, error) {
	parts := strings.Split(strings.TrimSpace(id), ":")
	if len(parts) != actionIDParts || parts[0] != actionPrefix || parts[2] == "" {
		return "", "", fmt.Errorf("%w: %q", ErrUnknownAction, id)
	}

	action := domain.ParticipationAction(parts[1])
	if action != domain.ActionJoin && action != domain.ActionPeek {
		return "", "", fmt.Errorf("%w: %q", ErrUnknownAction, id)
	}

	return action, parts[2], nil
}
