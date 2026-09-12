package domain

import "time"

// ParticipationRole は参加者の役割。
type ParticipationRole string

// 参加者の役割。
const (
	RoleParticipant ParticipationRole = "participant"
	RolePeeker      ParticipationRole = "peeker"
)

// ParticipationStatus は参加レコードの状態。
type ParticipationStatus string

// 参加レコードの状態。
const (
	ParticipationActive  ParticipationStatus = "active"
	ParticipationExpired ParticipationStatus = "expired"
	ParticipationRemoved ParticipationStatus = "removed"
	ParticipationLeft    ParticipationStatus = "left"
)

// ParticipationAction はユーザーがボタンやコマンドで要求した操作。
type ParticipationAction string

// 参加に関する操作。
const (
	ActionJoin ParticipationAction = "join"
	ActionPeek ParticipationAction = "peek"
)

// TransitionKind は操作の結果として起きる遷移の種別(docs/04-domain-model.md §2.5.1)。
type TransitionKind string

// 遷移の種別。
const (
	TransitionJoined             TransitionKind = "joined"
	TransitionPeeked             TransitionKind = "peeked"
	TransitionPromoted           TransitionKind = "promoted"
	TransitionAlreadyParticipant TransitionKind = "already_participant"
	TransitionAlreadyPeeking     TransitionKind = "already_peeking"
)

// Transition は遷移の種別と、それに伴って必要な副作用を表す。
type Transition struct {
	Kind          TransitionKind
	Role          ParticipationRole
	AddToChannel  bool
	CancelPeekJob bool
}

// Participation は 1 イベント・1 チャットユーザーの参加状態。`(eventId, chatUserId)` で一意。
type Participation struct {
	ID          string
	EventID     string
	WorkspaceID string
	ChatUserID  string
	DisplayName string
	Role        ParticipationRole
	Status      ParticipationStatus
	JoinedAt    time.Time
	ExpiresAt   *time.Time
	PeekCount   int

	UpdatedAt time.Time
}

// DecideTransition は現在の参加レコード(無ければ nil)と操作から、起こすべき遷移を決める。
func DecideTransition(prev *Participation, action ParticipationAction) Transition {
	if prev == nil || prev.Status != ParticipationActive {
		return newEntryTransition(action)
	}

	switch prev.Role {
	case RoleParticipant:
		return Transition{Kind: TransitionAlreadyParticipant, Role: RoleParticipant}
	case RolePeeker:
		if action == ActionJoin {
			return Transition{Kind: TransitionPromoted, Role: RoleParticipant, CancelPeekJob: true}
		}

		return Transition{Kind: TransitionAlreadyPeeking, Role: RolePeeker}
	default:
		return newEntryTransition(action)
	}
}

// IsActive はチャンネルに在室しているべき状態かを返す。
func (p *Participation) IsActive() bool {
	return p.Status == ParticipationActive
}

// PeekExpired はチラ見の期限が now までに到来しているかを返す。
func (p *Participation) PeekExpired(now time.Time) bool {
	return p.Role == RolePeeker && p.ExpiresAt != nil && !p.ExpiresAt.After(now)
}

func newEntryTransition(action ParticipationAction) Transition {
	if action == ActionPeek {
		return Transition{Kind: TransitionPeeked, Role: RolePeeker, AddToChannel: true}
	}

	return Transition{Kind: TransitionJoined, Role: RoleParticipant, AddToChannel: true}
}
