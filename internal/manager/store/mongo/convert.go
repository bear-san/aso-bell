package mongo

import (
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func encodeReminderPolicy(p domain.ReminderPolicy) reminderPolicyDoc {
	doc := reminderPolicyDoc{Mode: string(p.Mode)}

	for _, off := range p.Offsets {
		doc.Offsets = append(doc.Offsets, domain.FormatDuration(off))
	}

	if p.Mode == domain.ReminderModeInterval {
		doc.Every = domain.FormatDuration(p.Every)
		doc.At = p.At.String()
		doc.FinalOffset = domain.FormatDuration(p.FinalOffset)
	}

	return doc
}

func decodeReminderPolicy(doc reminderPolicyDoc) (domain.ReminderPolicy, error) {
	p := domain.ReminderPolicy{Mode: domain.ReminderMode(doc.Mode)}

	for _, s := range doc.Offsets {
		d, err := time.ParseDuration(s)
		if err != nil {
			return domain.ReminderPolicy{}, fmt.Errorf("decode reminder offset %q: %w", s, err)
		}

		p.Offsets = append(p.Offsets, d)
	}

	if doc.Every != "" {
		d, err := time.ParseDuration(doc.Every)
		if err != nil {
			return domain.ReminderPolicy{}, fmt.Errorf("decode reminder interval %q: %w", doc.Every, err)
		}

		p.Every = d
	}

	if doc.FinalOffset != "" {
		d, err := time.ParseDuration(doc.FinalOffset)
		if err != nil {
			return domain.ReminderPolicy{}, fmt.Errorf("decode reminder final offset %q: %w", doc.FinalOffset, err)
		}

		p.FinalOffset = d
	}

	if doc.At != "" {
		at, err := domain.ParseTimeOfDay(doc.At)
		if err != nil {
			return domain.ReminderPolicy{}, fmt.Errorf("decode reminder time: %w", err)
		}

		p.At = at
	}

	return p, nil
}

func encodeWorkspaceSettings(s domain.WorkspaceSettings) workspaceSettingsDoc {
	return workspaceSettingsDoc{
		RecruitChannelID:      s.RecruitChannelID,
		Timezone:              s.Timezone,
		DefaultReminderPolicy: encodeReminderPolicy(s.DefaultReminderPolicy),
		PeekDuration:          domain.FormatDuration(s.PeekDuration),
		AutoEndGrace:          domain.FormatDuration(s.AutoEndGrace),
		ChannelNamePrefix:     s.ChannelNamePrefix,
		DefaultStartTime:      s.DefaultStartTime.String(),
		Discord: discordSettingsDoc{
			EventCategoryID:   s.Discord.EventCategoryID,
			ArchiveCategoryID: s.Discord.ArchiveCategoryID,
		},
	}
}

func decodeWorkspaceSettings(doc workspaceSettingsDoc) (domain.WorkspaceSettings, error) {
	policy, err := decodeReminderPolicy(doc.DefaultReminderPolicy)
	if err != nil {
		return domain.WorkspaceSettings{}, err
	}

	peek, err := time.ParseDuration(doc.PeekDuration)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("decode peek duration %q: %w", doc.PeekDuration, err)
	}

	grace, err := time.ParseDuration(doc.AutoEndGrace)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("decode auto end grace %q: %w", doc.AutoEndGrace, err)
	}

	startTime, err := domain.ParseTimeOfDay(doc.DefaultStartTime)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("decode default start time: %w", err)
	}

	return domain.WorkspaceSettings{
		RecruitChannelID:      doc.RecruitChannelID,
		Timezone:              doc.Timezone,
		DefaultReminderPolicy: policy,
		PeekDuration:          peek,
		AutoEndGrace:          grace,
		ChannelNamePrefix:     doc.ChannelNamePrefix,
		DefaultStartTime:      startTime,
		Discord: domain.DiscordSettings{
			EventCategoryID:   doc.Discord.EventCategoryID,
			ArchiveCategoryID: doc.Discord.ArchiveCategoryID,
		},
	}, nil
}

func decodeWorkspace(doc workspaceDoc) (*domain.Workspace, error) {
	settings, err := decodeWorkspaceSettings(doc.Settings)
	if err != nil {
		return nil, err
	}

	return &domain.Workspace{
		ID:         doc.ID.Hex(),
		Provider:   domain.ProviderKind(doc.Provider),
		ExternalID: doc.ExternalID,
		Name:       doc.Name,
		Settings:   settings,
		CreatedAt:  doc.CreatedAt.UTC(),
		UpdatedAt:  doc.UpdatedAt.UTC(),
	}, nil
}

func encodeMessageRef(ref *domain.MessageRef) *messageRefDoc {
	if ref == nil {
		return nil
	}

	return &messageRefDoc{ChannelID: ref.ChannelID, MessageID: ref.MessageID}
}

func decodeMessageRef(doc *messageRefDoc) *domain.MessageRef {
	if doc == nil {
		return nil
	}

	return &domain.MessageRef{ChannelID: doc.ChannelID, MessageID: doc.MessageID}
}

func encodeMessageRefs(refs domain.MessageRefs) messageRefsDoc {
	return messageRefsDoc{
		Announcement:        encodeMessageRef(refs.Announcement),
		RecruitAnnouncement: encodeMessageRef(refs.RecruitAnnouncement),
		Summary:             encodeMessageRef(refs.Summary),
	}
}

func decodeMessageRefs(doc messageRefsDoc) domain.MessageRefs {
	return domain.MessageRefs{
		Announcement:        decodeMessageRef(doc.Announcement),
		RecruitAnnouncement: decodeMessageRef(doc.RecruitAnnouncement),
		Summary:             decodeMessageRef(doc.Summary),
	}
}

func encodeEvent(ev *domain.Event) (eventDoc, error) {
	workspaceID, err := objectID(ev.WorkspaceID)
	if err != nil {
		return eventDoc{}, err
	}

	doc := eventDoc{
		WorkspaceID:     workspaceID,
		Title:           ev.Title,
		Description:     ev.Description,
		Location:        ev.Location,
		StartsAt:        utc(ev.StartsAt),
		EndsAt:          utcPtr(ev.EndsAt),
		Status:          string(ev.Status),
		Organizer:       chatUserRefDoc{UserID: ev.Organizer.UserID, DisplayName: ev.Organizer.DisplayName},
		OriginChannelID: ev.OriginChannelID,
		Channel: channelRefDoc{
			ChannelID: ev.Channel.ChannelID,
			Name:      ev.Channel.Name,
			Archived:  ev.Channel.Archived,
		},
		Messages:         encodeMessageRefs(ev.Messages),
		ReminderPolicy:   encodeReminderPolicy(ev.ReminderPolicy),
		ParticipantCount: ev.ParticipantCount,
		EndedAt:          utcPtr(ev.EndedAt),
		EndReason:        string(ev.EndReason),
		CreatedVia:       string(ev.CreatedVia),
		CreatedBy:        ev.CreatedBy,
		CreatedAt:        utc(ev.CreatedAt),
		UpdatedAt:        utc(ev.UpdatedAt),
	}

	if ev.ID != "" {
		id, idErr := objectID(ev.ID)
		if idErr != nil {
			return eventDoc{}, idErr
		}

		doc.ID = id
	}

	return doc, nil
}

func decodeEvent(doc eventDoc) (*domain.Event, error) {
	policy, err := decodeReminderPolicy(doc.ReminderPolicy)
	if err != nil {
		return nil, err
	}

	return &domain.Event{
		ID:              doc.ID.Hex(),
		WorkspaceID:     doc.WorkspaceID.Hex(),
		Title:           doc.Title,
		Description:     doc.Description,
		Location:        doc.Location,
		StartsAt:        doc.StartsAt.UTC(),
		EndsAt:          utcPtr(doc.EndsAt),
		Status:          domain.EventStatus(doc.Status),
		Organizer:       domain.ChatUserRef{UserID: doc.Organizer.UserID, DisplayName: doc.Organizer.DisplayName},
		OriginChannelID: doc.OriginChannelID,
		Channel: domain.ChannelRef{
			ChannelID: doc.Channel.ChannelID,
			Name:      doc.Channel.Name,
			Archived:  doc.Channel.Archived,
		},
		Messages:         decodeMessageRefs(doc.Messages),
		ReminderPolicy:   policy,
		ParticipantCount: doc.ParticipantCount,
		EndedAt:          utcPtr(doc.EndedAt),
		EndReason:        domain.EndReason(doc.EndReason),
		CreatedVia:       domain.CreatedVia(doc.CreatedVia),
		CreatedBy:        doc.CreatedBy,
		CreatedAt:        doc.CreatedAt.UTC(),
		UpdatedAt:        doc.UpdatedAt.UTC(),
	}, nil
}

func decodeParticipation(doc participationDoc) *domain.Participation {
	return &domain.Participation{
		ID:          doc.ID.Hex(),
		EventID:     doc.EventID.Hex(),
		WorkspaceID: doc.WorkspaceID.Hex(),
		ChatUserID:  doc.ChatUserID,
		DisplayName: doc.DisplayName,
		Role:        domain.ParticipationRole(doc.Role),
		Status:      domain.ParticipationStatus(doc.Status),
		JoinedAt:    doc.JoinedAt.UTC(),
		ExpiresAt:   utcPtr(doc.ExpiresAt),
		PeekCount:   doc.PeekCount,
		UpdatedAt:   doc.UpdatedAt.UTC(),
	}
}

func decodeJob(doc jobDoc) *domain.Job {
	job := &domain.Job{
		ID:        doc.ID.Hex(),
		Kind:      domain.JobKind(doc.Kind),
		RunAt:     doc.RunAt.UTC(),
		DedupeKey: doc.DedupeKey,
		Payload: domain.JobPayload{
			EventID:         doc.Payload.EventID,
			ParticipationID: doc.Payload.ParticipationID,
			Sequence:        doc.Payload.Sequence,
			ScheduledFor:    doc.Payload.ScheduledFor.UTC(),
		},
		Status:      domain.JobStatus(doc.Status),
		Attempts:    doc.Attempts,
		MaxAttempts: doc.MaxAttempts,
		LeaseUntil:  utcPtr(doc.LeaseUntil),
		LastError:   doc.LastError,
		CreatedAt:   doc.CreatedAt.UTC(),
		UpdatedAt:   doc.UpdatedAt.UTC(),
		FinishedAt:  utcPtr(doc.FinishedAt),
	}

	if doc.EventID != nil {
		job.EventID = doc.EventID.Hex()
	}

	return job
}

func decodeReminderLog(doc reminderLogDoc) *domain.ReminderLog {
	log := &domain.ReminderLog{
		ID:           doc.ID.Hex(),
		EventID:      doc.EventID.Hex(),
		Sequence:     doc.Sequence,
		ScheduledFor: doc.ScheduledFor.UTC(),
		SentAt:       doc.SentAt.UTC(),
	}

	for _, t := range doc.Targets {
		log.Targets = append(log.Targets, domain.ReminderTarget{
			Kind:      domain.ReminderTargetKind(t.Kind),
			ChannelID: t.ChannelID,
			MessageID: t.MessageID,
			OK:        t.OK,
			Error:     t.Error,
		})
	}

	return log
}

func decodeConsoleUser(doc consoleUserDoc) *domain.ConsoleUser {
	return &domain.ConsoleUser{
		ID:          doc.ID.Hex(),
		GoogleSub:   doc.GoogleSub,
		Email:       doc.Email,
		Name:        doc.Name,
		Picture:     doc.Picture,
		CreatedVia:  domain.AccountOrigin(doc.CreatedVia),
		CreatedAt:   doc.CreatedAt.UTC(),
		LastLoginAt: doc.LastLoginAt.UTC(),
	}
}

func decodeChatIdentity(doc chatIdentityDoc) *domain.ChatIdentity {
	return &domain.ChatIdentity{
		ID:                  doc.ID.Hex(),
		ConsoleUserID:       doc.ConsoleUserID.Hex(),
		WorkspaceID:         doc.WorkspaceID.Hex(),
		Provider:            domain.ProviderKind(doc.Provider),
		WorkspaceExternalID: doc.WorkspaceExternalID,
		ChatUserID:          doc.ChatUserID,
		DisplayName:         doc.DisplayName,
		LinkedAt:            doc.LinkedAt.UTC(),
	}
}

func decodeLinkToken(doc linkTokenDoc) *domain.LinkToken {
	return &domain.LinkToken{
		ID:          doc.ID,
		WorkspaceID: doc.WorkspaceID.Hex(),
		ChatUserID:  doc.ChatUserID,
		DisplayName: doc.DisplayName,
		ExpiresAt:   doc.ExpiresAt.UTC(),
		ConsumedAt:  utcPtr(doc.ConsumedAt),
	}
}

func decodeProvider(doc providerDoc) *domain.ProviderState {
	return &domain.ProviderState{
		Kind:    domain.ProviderKind(doc.Kind),
		Address: doc.Address,
		Capabilities: domain.Capabilities{
			Forms:         doc.Capabilities.Forms,
			Ephemeral:     doc.Capabilities.Ephemeral,
			DirectMessage: doc.Capabilities.DirectMessage,
		},
		Version:     doc.Version,
		BotUserID:   doc.BotUserID,
		Connected:   doc.Connected,
		Status:      domain.ProviderStatus(doc.Status),
		Failures:    doc.Failures,
		FirstSeenAt: doc.FirstSeenAt.UTC(),
		LastSeenAt:  doc.LastSeenAt.UTC(),
		UpdatedAt:   doc.UpdatedAt.UTC(),
	}
}

func objectIDs(ids []string) ([]bson.ObjectID, error) {
	out := make([]bson.ObjectID, 0, len(ids))

	for _, id := range ids {
		oid, err := objectID(id)
		if err != nil {
			return nil, err
		}

		out = append(out, oid)
	}

	return out, nil
}
