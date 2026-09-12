package fake

import (
	"context"
	"sync"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// Responder は adapter.Responder のインメモリ実装。Runtime の ACK 順序を検証するために使う。
type Responder struct {
	mu        sync.Mutex
	acks      []*asobellv1.Reply
	followups []*asobellv1.Reply
	forms     []*asobellv1.Form
	// order は呼び出し順("ack" / "followup" / "form")。
	order    []string
	failures map[string][]error
}

// NewResponder は Responder を作る。
func NewResponder() *Responder {
	return &Responder{failures: map[string][]error{}}
}

// FailNext は method("Ack" / "Followup" / "OpenForm")の次の呼び出しで err を返させる。
func (r *Responder) FailNext(method string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.failures[method] = append(r.failures[method], err)
}

// Ack は即時応答を記録する。
func (r *Responder) Ack(_ context.Context, reply *asobellv1.Reply) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.acks = append(r.acks, reply)
	r.order = append(r.order, "ack")

	return r.failLocked("Ack")
}

// Followup は追加応答を記録する。
func (r *Responder) Followup(_ context.Context, reply *asobellv1.Reply) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.followups = append(r.followups, reply)
	r.order = append(r.order, "followup")

	return r.failLocked("Followup")
}

// OpenForm はフォームを開いた記録を残す。
func (r *Responder) OpenForm(_ context.Context, form *asobellv1.Form) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.forms = append(r.forms, form)
	r.order = append(r.order, "form")

	return r.failLocked("OpenForm")
}

// Acks は Ack に渡された Reply を返す。
func (r *Responder) Acks() []*asobellv1.Reply {
	return r.snapshot(func() []*asobellv1.Reply { return r.acks })
}

// Followups は Followup に渡された Reply を返す。
func (r *Responder) Followups() []*asobellv1.Reply {
	return r.snapshot(func() []*asobellv1.Reply { return r.followups })
}

// Forms は OpenForm に渡された Form を返す。
func (r *Responder) Forms() []*asobellv1.Form {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]*asobellv1.Form(nil), r.forms...)
}

// Order は応答メソッドの呼び出し順を返す。
func (r *Responder) Order() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.order...)
}

func (r *Responder) snapshot(get func() []*asobellv1.Reply) []*asobellv1.Reply {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]*asobellv1.Reply(nil), get()...)
}

// 型の取り違えを起動時に検出する。
var _ adapter.Responder = (*Responder)(nil)

func (r *Responder) failLocked(method string) error {
	queued := r.failures[method]
	if len(queued) == 0 {
		return nil
	}

	r.failures[method] = queued[1:]

	return queued[0]
}
