package discord

import "github.com/bwmarrin/discordgo"

// サブコマンドとオプションの名前(docs/07-bot-ux.md §1.2)。
// Manager の bot パッケージと同じ名前を使う必要があるが、Provider は Manager を import できないため
// ここで定義する。これは gRPC 越しの取り決めであり、変更時は docs/07 と両方を直す。
const (
	subNew     = "new"
	subEdit    = "edit"
	subRemind  = "remind"
	subEnd     = "end"
	subCancel  = "cancel"
	subLeave   = "leave"
	subInfo    = "info"
	subList    = "list"
	subRecruit = "recruit"
	subLogin   = "login"
	subUnlink  = "unlink"
	subHelp    = "help"
)

// オプション名。
const (
	optTitle       = "title"
	optWhen        = "when"
	optEnds        = "ends"
	optDescription = "description"
	optRemind      = "remind"
	optPace        = "pace"
	optAction      = "action"
)

// titleMaxLength はタイトルの上限。Manager 側のドメイン制約と揃える(docs/04 §2.1)。
const titleMaxLength = 80

// CommandDefinition は登録するスラッシュコマンドを返す。
// Contexts と IntegrationTypes で Guild 内に限定する。DM から実行されると guild_id が無く、
// ワークスペースを特定できないため(docs/06-provider.md §7)。
func CommandDefinition(name string) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:             name,
		Description:      "遊びの予定を立ち上げて管理します",
		Type:             discordgo.ChatApplicationCommand,
		Contexts:         &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
		IntegrationTypes: &[]discordgo.ApplicationIntegrationType{discordgo.ApplicationIntegrationGuildInstall},
		Options: []*discordgo.ApplicationCommandOption{
			sub(subNew, "イベントを立ち上げる",
				text(optTitle, "イベント名", true, titleMaxLength),
				text(optWhen, "開始日時(例 2026-09-20 19:00、9/20 19:00、明日 19:00)", true, 0),
				text(optEnds, "終了予定(例 22:00、+3h)", false, 0),
				text(optDescription, "説明", false, 0),
				text(optRemind, "リマインド(例 7d,1d,3h、every 2d 20:00、off)", false, 0),
			),
			sub(subEdit, "イベントを編集する",
				text(optTitle, "イベント名", false, titleMaxLength),
				text(optWhen, "開始日時", false, 0),
				text(optEnds, "終了予定", false, 0),
				text(optDescription, "説明", false, 0),
			),
			sub(subRemind, "リマインドペースを設定する",
				text(optPace, "例 7d,1d,3h / every 2d 20:00 / off", true, 0),
			),
			sub(subEnd, "イベントを終了する"),
			sub(subCancel, "イベントを中止する"),
			sub(subLeave, "イベントから抜ける"),
			sub(subInfo, "イベント情報と参加者を表示する"),
			sub(subList, "募集中のイベント一覧を表示する"),
			sub(subRecruit, "募集チャンネルを設定する", choices(optAction, "set または clear", "set", "clear")),
			sub(subLogin, "WebConsole との連携 URL を受け取る"),
			sub(subUnlink, "WebConsole との連携を解除する"),
			sub(subHelp, "使い方を表示する"),
		},
	}
}

func sub(name, description string, options ...*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionSubCommand,
		Name:        name,
		Description: description,
		Options:     options,
	}
}

// text は文字列オプションを作る。Discord に日付型が無いため、日時も文字列で受けて Manager が解釈する。
func text(name, description string, required bool, maxLength int) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionString,
		Name:        name,
		Description: description,
		Required:    required,
		MaxLength:   maxLength,
	}
}

func choices(name, description string, values ...string) *discordgo.ApplicationCommandOption {
	opt := text(name, description, true, 0)
	for _, value := range values {
		opt.Choices = append(opt.Choices, &discordgo.ApplicationCommandOptionChoice{Name: value, Value: value})
	}

	return opt
}
