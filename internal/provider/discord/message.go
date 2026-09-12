package discord

import (
	"github.com/bwmarrin/discordgo"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/render"
	"github.com/bear-san/aso-bell/internal/shared/markup"
)

// Discord の制約(→ docs/research/discord.md §3)。
const (
	// customIDLimit は custom_id の上限。
	customIDLimit = 100
	// labelLimit はボタンラベルの上限。
	labelLimit = 80
	// buttonsPerRow は 1 行あたりのボタン数。
	buttonsPerRow = 5
	// maxRows はメッセージあたりの Action Row 数。
	maxRows = 5
)

// content は Message を Discord の本文へ描画する。
func content(msg *asobellv1.Message) string {
	return render.Text(render.DiscordSyntax{}, msg, render.DiscordTextLimit)
}

// flags は Message の指定に応じたメッセージフラグを返す。
func flags(msg *asobellv1.Message) discordgo.MessageFlags {
	if msg.GetSuppressPreview() {
		return discordgo.MessageFlagsSuppressEmbeds
	}

	return 0
}

// components はボタンを Action Row へ並べる。上限を超えた分は落とす。
// Discord は上限超過をエラーにするため、投稿そのものを失敗させないよう描画側で切る。
func components(buttons []*asobellv1.Button) []discordgo.MessageComponent {
	rows := make([]discordgo.MessageComponent, 0, maxRows)

	for i := 0; i < len(buttons) && len(rows) < maxRows; i += buttonsPerRow {
		row := discordgo.ActionsRow{}

		for _, button := range buttons[i:min(i+buttonsPerRow, len(buttons))] {
			row.Components = append(row.Components, discordgo.Button{
				CustomID: markup.Truncate(button.GetActionId(), customIDLimit),
				Label:    markup.Truncate(button.GetLabel(), labelLimit),
				Style:    buttonStyle(button.GetStyle()),
				Disabled: button.GetDisabled(),
			})
		}

		rows = append(rows, row)
	}

	return rows
}

func buttonStyle(style asobellv1.ButtonStyle) discordgo.ButtonStyle {
	switch style {
	case asobellv1.ButtonStyle_BUTTON_STYLE_PRIMARY:
		return discordgo.PrimaryButton
	case asobellv1.ButtonStyle_BUTTON_STYLE_DANGER:
		return discordgo.DangerButton
	case asobellv1.ButtonStyle_BUTTON_STYLE_DEFAULT, asobellv1.ButtonStyle_BUTTON_STYLE_UNSPECIFIED:
		return discordgo.SecondaryButton
	default:
		return discordgo.SecondaryButton
	}
}

// messageSend は投稿用のペイロードを組み立てる。
func messageSend(msg *asobellv1.Message) *discordgo.MessageSend {
	return &discordgo.MessageSend{
		Content:    content(msg),
		Components: components(msg.GetButtons()),
		Flags:      flags(msg),
	}
}

// messageEdit は更新用のペイロードを組み立てる。ボタンは常に差し替える(終了時に無効化するため)。
func messageEdit(channelID, messageID string, msg *asobellv1.Message) *discordgo.MessageEdit {
	text := content(msg)
	rows := components(msg.GetButtons())

	return &discordgo.MessageEdit{
		Channel:    channelID,
		ID:         messageID,
		Content:    &text,
		Components: &rows,
	}
}

// webhookParams はインタラクションへの追加応答を組み立てる。
func webhookParams(msg *asobellv1.Message, ephemeral bool) *discordgo.WebhookParams {
	params := &discordgo.WebhookParams{
		Content:    content(msg),
		Components: components(msg.GetButtons()),
		Flags:      flags(msg),
	}

	if ephemeral {
		params.Flags |= discordgo.MessageFlagsEphemeral
	}

	return params
}
