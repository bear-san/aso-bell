package discord

import (
	"context"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"google.golang.org/protobuf/types/known/timestamppb"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// responder は 1 つのインタラクションへの応答ハンドル。
// Discord のインタラクションは 3 秒以内に 1 度だけ応答でき、以後は編集か追加投稿になる。
type responder struct {
	session     *discordgo.Session
	interaction *discordgo.Interaction
	// deferType は Ack で返す応答種別。コマンドは ephemeral の遅延応答、ボタンは元メッセージの遅延更新。
	deferType discordgo.InteractionResponseType
	// editsDeferred は Followup で遅延応答を編集するか(コマンド)、追加投稿するか(ボタン)。
	editsDeferred bool

	once sync.Once
}

// Ack は遅延応答を返す。Manager の応答を待つと 3 秒に間に合わないため、先に受領だけ伝える。
func (r *responder) Ack(ctx context.Context, reply *asobellv1.Reply) error {
	var err error

	r.once.Do(func() {
		err = r.session.InteractionRespond(r.interaction, &discordgo.InteractionResponse{
			Type: r.deferType,
			Data: r.ackData(reply),
		}, discordgo.WithContext(ctx))
	})

	return convert(err)
}

// ackData は遅延応答に添えるデータ。遅延応答に指定できるフラグは EPHEMERAL だけ。
func (r *responder) ackData(reply *asobellv1.Reply) *discordgo.InteractionResponseData {
	if r.deferType != discordgo.InteractionResponseDeferredChannelMessageWithSource {
		return nil
	}

	_ = reply

	return &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}
}

// Followup は Ack 後の返信を送る。
func (r *responder) Followup(ctx context.Context, reply *asobellv1.Reply) error {
	ephemeral := reply.GetVisibility() != asobellv1.Visibility_VISIBILITY_CHANNEL
	params := webhookParams(reply.GetMessage(), ephemeral)

	if r.editsDeferred {
		_, err := r.session.InteractionResponseEdit(r.interaction, &discordgo.WebhookEdit{
			Content:    &params.Content,
			Components: &params.Components,
		}, discordgo.WithContext(ctx))

		return convert(err)
	}

	_, err := r.session.FollowupMessageCreate(r.interaction, true, params, discordgo.WithContext(ctx))

	return convert(err)
}

// OpenForm は常に失敗する。Discord のモーダルは日付入力を持てず、v1 では forms=false を申告している。
func (r *responder) OpenForm(context.Context, *asobellv1.Form) error {
	return adapter.ErrUnsupported
}

// onInteraction は受信したインタラクションを Runtime へ渡す。
func (a *Adapter) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ctx, sink := a.inbound()
	if sink == nil {
		return
	}

	switch i.Type { //nolint:exhaustive // 応答するのはコマンドとボタンだけ。ほかは Discord 側が無視する
	case discordgo.InteractionApplicationCommand:
		sink.OnCommand(ctx, adapter.Command{
			Payload: commandPayload(i.Interaction),
			Responder: &responder{
				session:       s,
				interaction:   i.Interaction,
				deferType:     discordgo.InteractionResponseDeferredChannelMessageWithSource,
				editsDeferred: true,
			},
		})
	case discordgo.InteractionMessageComponent:
		sink.OnAction(ctx, adapter.Action{
			Payload: actionPayload(i.Interaction),
			Responder: &responder{
				session:     s,
				interaction: i.Interaction,
				deferType:   discordgo.InteractionResponseDeferredMessageUpdate,
			},
		})
	}
}

func commandPayload(i *discordgo.Interaction) *asobellv1.Command {
	name, args, raw := subcommand(i.ApplicationCommandData())

	return &asobellv1.Command{
		Workspace:  workspaceRef(i.GuildID),
		ChannelId:  i.ChannelID,
		UserId:     userID(i),
		Name:       name,
		Args:       args,
		RawText:    raw,
		ReceivedAt: timestamppb.New(adapter.Now()),
	}
}

func actionPayload(i *discordgo.Interaction) *asobellv1.Action {
	var messageID string
	if i.Message != nil {
		messageID = i.Message.ID
	}

	return &asobellv1.Action{
		Workspace:  workspaceRef(i.GuildID),
		ChannelId:  i.ChannelID,
		MessageId:  messageID,
		UserId:     userID(i),
		ActionId:   i.MessageComponentData().CustomID,
		ReceivedAt: timestamppb.New(adapter.Now()),
	}
}

// subcommand はサブコマンド名とオプションを取り出す。ログ用に元の入力も組み立てる。
func subcommand(data discordgo.ApplicationCommandInteractionData) (string, map[string]string, string) {
	for _, opt := range data.Options {
		if opt.Type != discordgo.ApplicationCommandOptionSubCommand {
			continue
		}

		args := make(map[string]string, len(opt.Options))
		parts := make([]string, 0, len(opt.Options)+1)
		parts = append(parts, opt.Name)

		for _, arg := range opt.Options {
			value := strings.TrimSpace(arg.StringValue())
			if value == "" {
				continue
			}

			args[arg.Name] = value
			parts = append(parts, arg.Name+":"+value)
		}

		return opt.Name, args, strings.Join(parts, " ")
	}

	return "", nil, ""
}

// userID は実行者の ID を返す。Guild 限定コマンドのため通常は Member に入っている。
func userID(i *discordgo.Interaction) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}

	if i.User != nil {
		return i.User.ID
	}

	return ""
}

func workspaceRef(guildID string) *asobellv1.WorkspaceRef {
	return &asobellv1.WorkspaceRef{ExternalId: guildID}
}
