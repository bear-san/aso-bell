// Package rpc は Manager が公開する gRPC サービスの実装。
package rpc

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// ManagerService は Provider からのインバウンド RPC を受ける(docs/16-grpc.md §2.1)。
type ManagerService struct {
	asobellv1.UnimplementedManagerServiceServer

	dispatcher *bot.Dispatcher
	workspaces *usecase.WorkspaceService
	status     usecase.ProviderStatusPort
	logger     *slog.Logger
}

// NewManagerService は ManagerService を作る。
func NewManagerService(
	dispatcher *bot.Dispatcher,
	workspaces *usecase.WorkspaceService,
	status usecase.ProviderStatusPort,
	logger *slog.Logger,
) *ManagerService {
	if logger == nil {
		logger = slog.Default()
	}

	return &ManagerService{dispatcher: dispatcher, workspaces: workspaces, status: status, logger: logger}
}

// ReportWorkspaces は Provider が把握しているワークスペースを同期する。
func (s *ManagerService) ReportWorkspaces(
	ctx context.Context,
	req *asobellv1.ReportWorkspacesRequest,
) (*asobellv1.ReportWorkspacesResponse, error) {
	reported := make([]usecase.ProviderWorkspace, 0, len(req.GetWorkspaces()))
	for _, ws := range req.GetWorkspaces() {
		reported = append(reported, usecase.ProviderWorkspace{
			ExternalID: ws.GetExternalId(),
			Name:       ws.GetName(),
		})
	}

	synced, err := s.workspaces.Sync(ctx, s.status.State().Kind, reported)
	if err != nil {
		return nil, s.fail(ctx, "report workspaces", err)
	}

	refs := make([]*asobellv1.WorkspaceRef, 0, len(synced))
	for _, ws := range synced {
		refs = append(refs, &asobellv1.WorkspaceRef{WorkspaceId: ws.ID, ExternalId: ws.ExternalID})
	}

	return &asobellv1.ReportWorkspacesResponse{Workspaces: refs}, nil
}

// HandleCommand はスラッシュコマンドを処理する。
func (s *ManagerService) HandleCommand(
	ctx context.Context,
	req *asobellv1.HandleCommandRequest,
) (*asobellv1.HandleCommandResponse, error) {
	reply, err := s.dispatcher.Command(ctx, req.GetCommand())
	if err != nil {
		return nil, s.fail(ctx, "handle command", err)
	}

	return &asobellv1.HandleCommandResponse{Reply: reply}, nil
}

// HandleAction はボタン押下を処理する。
func (s *ManagerService) HandleAction(
	ctx context.Context,
	req *asobellv1.HandleActionRequest,
) (*asobellv1.HandleActionResponse, error) {
	reply, err := s.dispatcher.Action(ctx, req.GetAction())
	if err != nil {
		return nil, s.fail(ctx, "handle action", err)
	}

	return &asobellv1.HandleActionResponse{Reply: reply}, nil
}

// HandleFormSubmit はフォーム送信を処理する。
func (s *ManagerService) HandleFormSubmit(
	ctx context.Context,
	req *asobellv1.HandleFormSubmitRequest,
) (*asobellv1.HandleFormSubmitResponse, error) {
	reply, err := s.dispatcher.FormSubmit(ctx, req.GetSubmission())
	if err != nil {
		return nil, s.fail(ctx, "handle form submit", err)
	}

	return &asobellv1.HandleFormSubmitResponse{Reply: reply}, nil
}

// fail は内部エラーをログに残し、gRPC ステータスへ変換する。
// Provider へ内部の詳細を返しても対処のしようがないため、文言は短く保つ。
func (s *ManagerService) fail(ctx context.Context, op string, err error) error {
	s.logger.ErrorContext(ctx, op+" failed", "error", err)

	return status.Error(codeOf(err), op+" failed")
}

func codeOf(err error) codes.Code {
	var invalid *domain.ValidationError

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return codes.NotFound
	case errors.Is(err, domain.ErrForbidden):
		return codes.PermissionDenied
	case errors.Is(err, domain.ErrProviderUnavailable):
		return codes.Unavailable
	case errors.As(err, &invalid):
		return codes.InvalidArgument
	default:
		return codes.Internal
	}
}
