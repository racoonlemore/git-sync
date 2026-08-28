// Package engine contém o motor de sincronização do git-sync (seam 1).
//
// O motor implementa a política de sincronização descrita na spec e nos
// ADRs: a cada ciclo tenta pull → add → commit → push, aplicando a
// política unificada "o remoto sempre vence" (ADR 0001/0002) tanto na
// falha de pull (conflito de conteúdo) quanto na rejeição de push
// (divergência do remoto), e mantendo a branch limitada a MaxCommits.
//
// A lógica de domínio (SyncCycle/PullCycle) é independente de
// filesystem-watch e de temporizadores, o que permite exercitá-la em
// testes contra repositórios git reais e temporários. O laço Run apenas
// conecta o watcher e os timers a essas operações.
package engine

import (
	"fmt"
	"log"
	"time"

	"github.com/racoonlemore/git-sync/internal/gitops"
)

// Parâmetros fixos do daemon (não configuráveis, ver spec/CONTEXT.md).
const (
	// Debounce agrupa mudanças em rajada num único ciclo de sync.
	Debounce = 5 * time.Minute
	// PullInterval é o intervalo do pull periódico, independente de mudanças.
	PullInterval = time.Hour
	// MaxCommits é o limite de commits mantidos na branch.
	MaxCommits = 20
)

// Repo é a superfície de git da qual o motor depende. É satisfeita por
// *gitops.Repo; declará-la como interface mantém o motor testável e
// documenta exatamente o que ele usa.
type Repo interface {
	CurrentBranch() (string, error)
	Pull() error
	Fetch() error
	ResetHardToRemote(branch string) error
	HasChanges() (bool, error)
	StageAll() error
	HasStagedChanges() (bool, error)
	Commit(message string) error
	CommitCount() (int, error)
	Trim(branch string, n int) error
	Push(branch string) (rejected bool, err error)
	PushForce(branch string) (rejected bool, err error)
}

var _ Repo = (*gitops.Repo)(nil)

// Engine executa a política de sincronização sobre um repositório.
type Engine struct {
	repo       Repo
	userName   string
	maxCommits int
	// now permite tempo determinístico nos testes (mensagem de commit).
	now func() time.Time
	// logf recebe eventos observáveis do ciclo.
	logf func(format string, args ...any)
}

// Option customiza a construção do Engine.
type Option func(*Engine)

// WithClock injeta uma fonte de tempo (usado em testes).
func WithClock(now func() time.Time) Option {
	return func(e *Engine) { e.now = now }
}

// WithLogger injeta uma função de log.
func WithLogger(logf func(format string, args ...any)) Option {
	return func(e *Engine) { e.logf = logf }
}

// WithMaxCommits sobrescreve o limite de commits (usado em testes).
func WithMaxCommits(n int) Option {
	return func(e *Engine) { e.maxCommits = n }
}

// New cria um Engine para o repositório dado. userName é o nome usado na
// mensagem de commit; deve vir de gitops.Repo.UserName (pré-condição já
// validada por setup/validate).
func New(repo Repo, userName string, opts ...Option) *Engine {
	e := &Engine{
		repo:       repo,
		userName:   userName,
		maxCommits: MaxCommits,
		now:        time.Now,
		logf:       log.Printf,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// SyncCycle executa um ciclo completo de sincronização acionado pelo
// debounce: pull → (add/commit/push) com trim quando necessário. Aplica
// "o remoto sempre vence" em conflitos de pull e rejeições de push.
func (e *Engine) SyncCycle() error {
	branch, err := e.repo.CurrentBranch()
	if err != nil {
		return err
	}

	// 1. Tenta incorporar o que o remoto tem. Qualquer falha de pull é
	// tratada como conflito de conteúdo: o remoto prevalece.
	if err := e.repo.Pull(); err != nil {
		e.logf("pull falhou (%v); aplicando remoto-vence: fetch + reset --hard", err)
		return e.resetToRemote(branch)
	}

	// 2. Prepara mudanças locais.
	if err := e.repo.StageAll(); err != nil {
		return err
	}
	staged, err := e.repo.HasStagedChanges()
	if err != nil {
		return err
	}
	if !staged {
		// Nada mudou localmente; o pull já convergiu o estado.
		return nil
	}

	// 3. Commit das mudanças locais.
	if err := e.repo.Commit(e.commitMessage()); err != nil {
		return err
	}

	// 4. Trim: ao ultrapassar o limite, reescreve a história e força push.
	count, err := e.repo.CommitCount()
	if err != nil {
		return err
	}
	if count > e.maxCommits {
		e.logf("branch %s excedeu %d commits (%d); reescrevendo história", branch, e.maxCommits, count)
		if err := e.repo.Trim(branch, e.maxCommits); err != nil {
			return err
		}
		return e.pushOrReset(branch, true)
	}

	// 5. Push normal.
	return e.pushOrReset(branch, false)
}

// PullCycle é o pull periódico (horário). Só puxa quando a working tree
// está limpa, para não descartar mudanças locais ainda no meio do
// debounce; nesse caso deixa o SyncCycle cuidar delas. Em falha de pull,
// aplica "o remoto sempre vence".
func (e *Engine) PullCycle() error {
	branch, err := e.repo.CurrentBranch()
	if err != nil {
		return err
	}
	dirty, err := e.repo.HasChanges()
	if err != nil {
		return err
	}
	if dirty {
		e.logf("pull horário adiado: há mudanças locais pendentes")
		return nil
	}
	if err := e.repo.Pull(); err != nil {
		e.logf("pull horário falhou (%v); aplicando remoto-vence", err)
		return e.resetToRemote(branch)
	}
	return nil
}

// pushOrReset envia a branch (normal ou --force) e, se for rejeitada por
// divergência, aplica "o remoto sempre vence" e abandona o ciclo.
func (e *Engine) pushOrReset(branch string, force bool) error {
	var rejected bool
	var err error
	if force {
		rejected, err = e.repo.PushForce(branch)
	} else {
		rejected, err = e.repo.Push(branch)
	}
	if err != nil {
		return err
	}
	if rejected {
		e.logf("push rejeitado por divergência; aplicando remoto-vence e abandonando ciclo")
		return e.resetToRemote(branch)
	}
	return nil
}

// resetToRemote descarta o estado local e assume o do remoto.
func (e *Engine) resetToRemote(branch string) error {
	if err := e.repo.Fetch(); err != nil {
		return err
	}
	return e.repo.ResetHardToRemote(branch)
}

// commitMessage produz "sync dd-mm-yyyy hh:mm (nome do usuário git)".
func (e *Engine) commitMessage() string {
	return fmt.Sprintf("sync %s (%s)", e.now().Format("02-01-2006 15:04"), e.userName)
}
