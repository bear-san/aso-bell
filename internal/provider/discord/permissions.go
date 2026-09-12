package discord

import "github.com/bwmarrin/discordgo"

// PermissionPinMessages は 2025-08 に MANAGE_MESSAGES から分離されたピン留め権限。
// discordgo に定数が無いため自前で定義する(→ docs/research/discord.md §2-7)。
const PermissionPinMessages int64 = 1 << 51

// memberPermissions は参加者へ付与するチャンネル権限。
// Bot が自分の持たない権限を overwrite に含めると Discord が 403 を返すため、必要最小限に絞る。
const memberPermissions = discordgo.PermissionViewChannel |
	discordgo.PermissionSendMessages |
	discordgo.PermissionReadMessageHistory

// botPermissions は Bot 自身へ付与するチャンネル権限。ピン留めのために PIN_MESSAGES を足す。
const botPermissions = memberPermissions | PermissionPinMessages

// InvitePermissions は Bot 招待 URL に載せる権限ビット(docs/06-provider.md §7)。
const InvitePermissions = memberPermissions |
	discordgo.PermissionManageChannels |
	discordgo.PermissionManageRoles |
	PermissionPinMessages
