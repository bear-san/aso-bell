package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestDecideTransition(t *testing.T) {
	t.Parallel()

	active := func(role domain.ParticipationRole) *domain.Participation {
		return &domain.Participation{Role: role, Status: domain.ParticipationActive}
	}
	inactive := func(role domain.ParticipationRole, status domain.ParticipationStatus) *domain.Participation {
		return &domain.Participation{Role: role, Status: status}
	}

	tests := []struct {
		name   string
		prev   *domain.Participation
		action domain.ParticipationAction
		want   domain.Transition
	}{
		{
			name:   "first join",
			action: domain.ActionJoin,
			want: domain.Transition{
				Kind:         domain.TransitionJoined,
				Role:         domain.RoleParticipant,
				AddToChannel: true,
			},
		},
		{
			name:   "first peek",
			action: domain.ActionPeek,
			want: domain.Transition{
				Kind:         domain.TransitionPeeked,
				Role:         domain.RolePeeker,
				AddToChannel: true,
			},
		},
		{
			name:   "peeker joins",
			prev:   active(domain.RolePeeker),
			action: domain.ActionJoin,
			want: domain.Transition{
				Kind:          domain.TransitionPromoted,
				Role:          domain.RoleParticipant,
				CancelPeekJob: true,
			},
		},
		{
			name:   "peeker peeks again",
			prev:   active(domain.RolePeeker),
			action: domain.ActionPeek,
			want:   domain.Transition{Kind: domain.TransitionAlreadyPeeking, Role: domain.RolePeeker},
		},
		{
			name:   "participant joins again",
			prev:   active(domain.RoleParticipant),
			action: domain.ActionJoin,
			want:   domain.Transition{Kind: domain.TransitionAlreadyParticipant, Role: domain.RoleParticipant},
		},
		{
			name:   "participant peeks",
			prev:   active(domain.RoleParticipant),
			action: domain.ActionPeek,
			want:   domain.Transition{Kind: domain.TransitionAlreadyParticipant, Role: domain.RoleParticipant},
		},
		{
			name:   "expired peeker rejoins",
			prev:   inactive(domain.RolePeeker, domain.ParticipationExpired),
			action: domain.ActionJoin,
			want: domain.Transition{
				Kind:         domain.TransitionJoined,
				Role:         domain.RoleParticipant,
				AddToChannel: true,
			},
		},
		{
			name:   "expired peeker peeks again",
			prev:   inactive(domain.RolePeeker, domain.ParticipationExpired),
			action: domain.ActionPeek,
			want: domain.Transition{
				Kind:         domain.TransitionPeeked,
				Role:         domain.RolePeeker,
				AddToChannel: true,
			},
		},
		{
			name:   "left participant rejoins",
			prev:   inactive(domain.RoleParticipant, domain.ParticipationLeft),
			action: domain.ActionJoin,
			want: domain.Transition{
				Kind:         domain.TransitionJoined,
				Role:         domain.RoleParticipant,
				AddToChannel: true,
			},
		},
		{
			name:   "removed participant rejoins",
			prev:   inactive(domain.RoleParticipant, domain.ParticipationRemoved),
			action: domain.ActionJoin,
			want: domain.Transition{
				Kind:         domain.TransitionJoined,
				Role:         domain.RoleParticipant,
				AddToChannel: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, domain.DecideTransition(tc.prev, tc.action))
		})
	}
}

func TestParticipationPeekExpired(t *testing.T) {
	t.Parallel()

	now := testNow()
	expiresAt := now.Add(-time.Minute)

	peeker := &domain.Participation{
		Role:      domain.RolePeeker,
		Status:    domain.ParticipationActive,
		ExpiresAt: &expiresAt,
	}
	require.True(t, peeker.IsActive())
	assert.True(t, peeker.PeekExpired(now))

	future := now.Add(time.Hour)
	peeker.ExpiresAt = &future
	assert.False(t, peeker.PeekExpired(now))

	participant := &domain.Participation{Role: domain.RoleParticipant, Status: domain.ParticipationActive}
	assert.False(t, participant.PeekExpired(now))
}
