package bot

import (
	"errors"
	"fmt"
	"strings"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
)

// ErrUnknownCommand は知らないサブコマンドを受け取ったことを表す。
var ErrUnknownCommand = errors.New("unknown command")

// Subcommand は `/asobell` のサブコマンド(docs/07-bot-ux.md §1)。
type Subcommand string

// サブコマンド。
const (
	SubcommandNew     Subcommand = "new"
	SubcommandEdit    Subcommand = "edit"
	SubcommandRemind  Subcommand = "remind"
	SubcommandEnd     Subcommand = "end"
	SubcommandCancel  Subcommand = "cancel"
	SubcommandLeave   Subcommand = "leave"
	SubcommandInfo    Subcommand = "info"
	SubcommandList    Subcommand = "list"
	SubcommandRecruit Subcommand = "recruit"
	SubcommandLogin   Subcommand = "login"
	SubcommandUnlink  Subcommand = "unlink"
	SubcommandHelp    Subcommand = "help"
)

// コマンド引数のキー。Discord のオプション名と Slack の位置引数の両方をこの名前に揃える。
const (
	ArgTitle       = "title"
	ArgWhen        = "when"
	ArgEnds        = "ends"
	ArgDescription = "description"
	ArgRemind      = "remind"
	ArgPace        = "pace"
	ArgAction      = "action"
)

// 募集チャンネル設定の操作。
const (
	RecruitActionSet   = "set"
	RecruitActionClear = "clear"
)

// Command は正規化されたコマンド。Provider ごとの入力形式の差はここで吸収する。
type Command struct {
	Sub         Subcommand
	Args        map[string]string
	WorkspaceID string
	ExternalID  string
	ChannelID   string
	UserID      string
	RawText     string
}

// ParseCommand は Provider から届いたコマンドを正規化する。
// Discord のように name とオプションが埋まっていればそれを使い、Slack のように本文だけならサブコマンドを切り出す。
func ParseCommand(cmd *asobellv1.Command) (Command, error) {
	args := make(map[string]string, len(cmd.GetArgs()))
	for k, v := range cmd.GetArgs() {
		if value := strings.TrimSpace(v); value != "" {
			args[strings.ToLower(strings.TrimSpace(k))] = value
		}
	}

	name := strings.ToLower(strings.TrimSpace(cmd.GetName()))
	raw := normalizeInput(cmd.GetRawText())

	if name == "" {
		var rest string

		name, rest = splitSubcommand(raw)
		mergePositionalArgs(args, Subcommand(name), rest)
	}

	sub := Subcommand(name)
	if name == "" {
		sub = SubcommandHelp
	} else if !sub.Valid() {
		return Command{}, fmt.Errorf("%w: %q", ErrUnknownCommand, name)
	}

	return Command{
		Sub:         sub,
		Args:        args,
		WorkspaceID: cmd.GetWorkspace().GetWorkspaceId(),
		ExternalID:  cmd.GetWorkspace().GetExternalId(),
		ChannelID:   cmd.GetChannelId(),
		UserID:      cmd.GetUserId(),
		RawText:     raw,
	}, nil
}

// Valid は既知のサブコマンドかを返す。
func (s Subcommand) Valid() bool {
	switch s {
	case SubcommandNew, SubcommandEdit, SubcommandRemind, SubcommandEnd, SubcommandCancel,
		SubcommandLeave, SubcommandInfo, SubcommandList, SubcommandRecruit, SubcommandLogin,
		SubcommandUnlink, SubcommandHelp:
		return true
	default:
		return false
	}
}

// NeedsEventChannel はイベントチャンネル内でのみ実行できるサブコマンドかを返す(docs/07-bot-ux.md §1)。
func (s Subcommand) NeedsEventChannel() bool {
	switch s {
	case SubcommandEdit, SubcommandRemind, SubcommandEnd, SubcommandCancel, SubcommandLeave, SubcommandInfo:
		return true
	case SubcommandNew, SubcommandList, SubcommandRecruit, SubcommandLogin, SubcommandUnlink, SubcommandHelp:
		return false
	default:
		return false
	}
}

// OrganizerOnly は主催者だけが実行できるサブコマンドかを返す(docs/07-bot-ux.md §6)。
func (s Subcommand) OrganizerOnly() bool {
	switch s {
	case SubcommandEdit, SubcommandRemind, SubcommandEnd, SubcommandCancel:
		return true
	case SubcommandNew, SubcommandLeave, SubcommandInfo, SubcommandList, SubcommandRecruit,
		SubcommandLogin, SubcommandUnlink, SubcommandHelp:
		return false
	default:
		return false
	}
}

// Arg は引数を返す。未指定なら空文字。
func (c Command) Arg(key string) string {
	return c.Args[key]
}

func splitSubcommand(raw string) (string, string) {
	text := strings.TrimSpace(raw)

	// Provider は本文からコマンド名を除いて渡すが、そのまま転送する実装でも動くようにしておく。
	if after, ok := strings.CutPrefix(text, "/asobell"); ok {
		text = strings.TrimSpace(after)
	}

	name, rest, _ := strings.Cut(text, " ")

	return strings.ToLower(name), strings.TrimSpace(rest)
}

// mergePositionalArgs は Slack の位置引数を Discord のオプション名へ寄せる。
func mergePositionalArgs(args map[string]string, sub Subcommand, rest string) {
	if rest == "" {
		return
	}

	switch sub {
	case SubcommandRemind:
		setIfAbsent(args, ArgPace, rest)
	case SubcommandRecruit:
		action, _, _ := strings.Cut(rest, " ")
		setIfAbsent(args, ArgAction, strings.ToLower(action))
	case SubcommandNew, SubcommandEdit, SubcommandEnd, SubcommandCancel, SubcommandLeave,
		SubcommandInfo, SubcommandList, SubcommandLogin, SubcommandUnlink, SubcommandHelp:
	}
}

func setIfAbsent(args map[string]string, key, value string) {
	if _, ok := args[key]; !ok {
		args[key] = value
	}
}
