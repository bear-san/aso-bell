package fake

import (
	"context"
	"slices"
	"sync"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
)

// ManagerServer は ManagerService のインメモリ実装。Provider Runtime のテストで bufconn 上に起動する。
// 受け取った Command / Action / FormSubmission を記録し、設定した Reply を返す(docs/06-provider.md §8)。
type ManagerServer struct {
	asobellv1.UnimplementedManagerServiceServer

	mu         sync.Mutex
	commands   []*asobellv1.Command
	actions    []*asobellv1.Action
	forms      []*asobellv1.FormSubmission
	reported   [][]*asobellv1.WorkspaceInfo
	reply      *asobellv1.Reply
	failures   map[string][]error
	workspaces []*asobellv1.WorkspaceRef
}

// NewManagerServer は ManagerServer を作る。既定では空の Reply を返す。
func NewManagerServer() *ManagerServer {
	return &ManagerServer{failures: map[string][]error{}}
}

// SetReply はインバウンド RPC が返す Reply を差し替える。
func (s *ManagerServer) SetReply(reply *asobellv1.Reply) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reply = reply
}

// SetWorkspaceRefs は ReportWorkspaces の応答を差し替える。
func (s *ManagerServer) SetWorkspaceRefs(refs ...*asobellv1.WorkspaceRef) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.workspaces = refs
}

// FailNext は method の次の呼び出しで err を返させる。
func (s *ManagerServer) FailNext(method string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failures[method] = append(s.failures[method], err)
}

// Commands は受け取ったコマンドを返す。
func (s *ManagerServer) Commands() []*asobellv1.Command {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.commands)
}

// Actions は受け取ったボタン押下を返す。
func (s *ManagerServer) Actions() []*asobellv1.Action {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.actions)
}

// FormSubmissions は受け取ったフォーム送信を返す。
func (s *ManagerServer) FormSubmissions() []*asobellv1.FormSubmission {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.forms)
}

// Reported は ReportWorkspaces で受け取った内容を呼び出しごとに返す。
func (s *ManagerServer) Reported() [][]*asobellv1.WorkspaceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.reported)
}

// ReportWorkspaces はワークスペースの報告を記録する。
func (s *ManagerServer) ReportWorkspaces(
	_ context.Context,
	req *asobellv1.ReportWorkspacesRequest,
) (*asobellv1.ReportWorkspacesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failLocked("ReportWorkspaces"); err != nil {
		return nil, err
	}

	s.reported = append(s.reported, req.GetWorkspaces())

	refs := s.workspaces
	if refs == nil {
		refs = make([]*asobellv1.WorkspaceRef, 0, len(req.GetWorkspaces()))
		for _, ws := range req.GetWorkspaces() {
			refs = append(refs, &asobellv1.WorkspaceRef{ExternalId: ws.GetExternalId()})
		}
	}

	return &asobellv1.ReportWorkspacesResponse{Workspaces: refs}, nil
}

// HandleCommand はコマンドを記録し、設定した Reply を返す。
func (s *ManagerServer) HandleCommand(
	_ context.Context,
	req *asobellv1.HandleCommandRequest,
) (*asobellv1.HandleCommandResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failLocked("HandleCommand"); err != nil {
		return nil, err
	}

	s.commands = append(s.commands, req.GetCommand())

	return &asobellv1.HandleCommandResponse{Reply: s.reply}, nil
}

// HandleAction はボタン押下を記録し、設定した Reply を返す。
func (s *ManagerServer) HandleAction(
	_ context.Context,
	req *asobellv1.HandleActionRequest,
) (*asobellv1.HandleActionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failLocked("HandleAction"); err != nil {
		return nil, err
	}

	s.actions = append(s.actions, req.GetAction())

	return &asobellv1.HandleActionResponse{Reply: s.reply}, nil
}

// HandleFormSubmit はフォーム送信を記録し、設定した Reply を返す。
func (s *ManagerServer) HandleFormSubmit(
	_ context.Context,
	req *asobellv1.HandleFormSubmitRequest,
) (*asobellv1.HandleFormSubmitResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failLocked("HandleFormSubmit"); err != nil {
		return nil, err
	}

	s.forms = append(s.forms, req.GetSubmission())

	return &asobellv1.HandleFormSubmitResponse{Reply: s.reply}, nil
}

func (s *ManagerServer) failLocked(method string) error {
	queued := s.failures[method]
	if len(queued) == 0 {
		return nil
	}

	s.failures[method] = queued[1:]

	return queued[0]
}
