package discord

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// Discord の JSON エラーコード(→ docs/research/discord.md §2-10)。
const (
	codeUnknownChannel   = 10003
	codeUnknownMessage   = 10008
	codeUnknownUser      = 10013
	codeUnknownMember    = 10007
	codeMaxPins          = 30003
	codeOpeningDMTooFast = 40003
	codeCannotSendToUser = 50007
)

// convert は discordgo のエラーを Adapter のセンチネルへ変換する(docs/16-grpc.md §3)。
// 判定は JSON エラーコードを優先し、無ければ HTTP ステータスで決める。
func convert(err error) error {
	if err == nil {
		return nil
	}

	var rateLimit *discordgo.RateLimitError
	if errors.As(err, &rateLimit) {
		return adapter.ErrRateLimited
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return adapter.ErrUnavailable
	}

	if errors.Is(err, discordgo.ErrUnauthorized) {
		return adapter.ErrPermission
	}

	var netErr *url.Error
	if errors.As(err, &netErr) {
		return adapter.ErrUnavailable
	}

	var rest *discordgo.RESTError
	if !errors.As(err, &rest) {
		return byMessage(err)
	}

	if mapped := byJSONCode(rest); mapped != nil {
		return mapped
	}

	return byHTTPStatus(rest, err)
}

// maxRetriesMessage は discordgo が 502 を再試行し尽くしたときの文言。
// 構造化されたエラーを返さないため文言で判定する(restapi.go の StatusBadGateway 分岐)。
const maxRetriesMessage = "Exceeded Max retries"

// byMessage は型で判別できない discordgo のエラーを扱う。判定できなければそのまま返す。
func byMessage(err error) error {
	if strings.Contains(err.Error(), maxRetriesMessage) {
		return adapter.ErrUnavailable
	}

	return err
}

func byJSONCode(rest *discordgo.RESTError) error {
	if rest.Message == nil {
		return nil
	}

	switch rest.Message.Code {
	case codeUnknownChannel, codeUnknownMessage, codeUnknownUser, codeUnknownMember:
		return adapter.ErrNotFound
	case codeMaxPins:
		return adapter.ErrPinLimit
	case codeCannotSendToUser:
		return adapter.ErrDMBlocked
	case codeOpeningDMTooFast:
		return adapter.ErrRateLimited
	default:
		return nil
	}
}

func byHTTPStatus(rest *discordgo.RESTError, original error) error {
	if rest.Response == nil {
		return original
	}

	switch {
	case rest.Response.StatusCode == http.StatusForbidden:
		return adapter.ErrPermission
	case rest.Response.StatusCode == http.StatusNotFound:
		return adapter.ErrNotFound
	case rest.Response.StatusCode == http.StatusTooManyRequests:
		return adapter.ErrRateLimited
	case rest.Response.StatusCode == http.StatusUnauthorized:
		return adapter.ErrPermission
	case rest.Response.StatusCode >= http.StatusInternalServerError:
		return adapter.ErrUnavailable
	default:
		return original
	}
}
