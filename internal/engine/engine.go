// Package engine contém o motor de sincronização do git-sync (seam 1).
//
// O motor implementa a política de sincronização descrita na spec e nos
// ADRs: a cada ciclo tenta pull → add → commit → push, aplicando a
// política unificada "o remoto sempre vence" (ADR 0001/0002) tanto na
// falha de pull (conflito de conteúdo) quanto na rejeição de push
// (divergência do remoto), e mantendo a branch abaixo de MaxCommits.
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
	Debounce = 3 * time.Minute
	// PullInterval é o intervalo do pull periódico, independente de mudanças.
	PullInterval = time.Hour
	// MaxCommits é o limite de commits da branch: ao chegar nele, o trim
	// descarta a metade mais antiga e mantém os MaxCommits/2 mais recentes.
	// A folga faz a reescrita da história (que obriga as outras máquinas a
	// reconciliar) acontecer uma vez a cada MaxCommits/2 syncs, e não a
	// cada sync (ADR 0005).
	MaxCommits = 100
)

// LogFileName é o arquivo de log de execução local, na raiz do repositório.
// É deliberadamente ignorado pelo git (não vai ao remoto) e pelo watcher
// (escrever nele não pode disparar um novo ciclo de sync).
const LogFileName = ".git-sync-log"

// Repo é a superfície de git da qual o motor depende. É satisfeita por
// *gitops.Repo; declará-la como interface mantém o motor testável e
// documenta exatamente o que ele usa.
type Repo interface {
	CurrentBranch() (string, error)
	Pull() error
	Fetch() error
	ResetHardToRemote(branch string) error
	HasChanges() (bool, error)
	Stash() error
	StashPop() (conflict bool, err error)
	StashDrop() error
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

// WithMaxCommits sobrescreve o limite de commits (usado em testes); o trim
// mantém a metade, como com MaxCommits.
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

	// 1. Guarda as mudanças locais para o pull rodar sobre uma working tree
	// limpa (mantendo o pull-first da spec). Isso é obrigatório para ser
	// agnóstico à versão do git: um `git pull` sobre árvore suja tem
	// comportamento que varia entre versões — algumas atualizam só os
	// arquivos necessários, outras tentam fast-forwardar a working tree
	// inteira e falham com "Cannot fast-forward your working tree" (exit
	// 128) se qualquer arquivo estiver sujo. Com stash, o pull sempre vê
	// uma árvore limpa e se comporta igual em qualquer versão.
	dirty, err := e.repo.HasChanges()
	if err != nil {
		return err
	}
	if dirty {
		if err := e.repo.Stash(); err != nil {
			return err
		}
	}

	// 2. Incorpora o que o remoto tem (pull-first). Se o pull falhar, a
	// branch local assume o remoto (fetch + reset --hard), mas as mudanças
	// guardadas NÃO são descartadas aqui: elas seguem para o passo 3, que
	// só as descarta se conflitarem. O caso comum é o trim de outra
	// máquina: a história reescrita não tem ancestral comum com a local, e
	// o pull falha com "refusing to merge unrelated histories" a cada
	// ciclo; descartar o stash nesse ponto perdia toda edição local, mesmo
	// em arquivos que o remoto nem tocou (ADR 0001 só descarta conflito).
	if err := e.repo.Pull(); err != nil {
		e.logf("pull falhou (%v); assumindo o remoto (fetch + reset --hard) e reaplicando mudanças locais", err)
		if err := e.resetToRemote(branch); err != nil {
			// Remoto inacessível (ex.: sem rede): devolve as mudanças
			// locais à working tree para o próximo ciclo tentar de novo.
			if dirty {
				if _, popErr := e.repo.StashPop(); popErr != nil {
					return fmt.Errorf("%w (e as mudanças locais ficaram no stash: %v)", err, popErr)
				}
			}
			return err
		}
	}

	// 3. Reaplica as mudanças locais sobre o estado recém-integrado. Se
	// colidirem (o mesmo arquivo mudou no remoto), o remoto vence.
	if dirty {
		conflict, err := e.repo.StashPop()
		if err != nil {
			return err
		}
		if conflict {
			e.logf("conflito ao reaplicar mudanças locais; aplicando remoto-vence")
			_ = e.repo.StashDrop()
			return e.resetToRemote(branch)
		}
	}

	// 4. Prepara mudanças locais.
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

	// 5. Commit das mudanças locais.
	if err := e.repo.Commit(e.commitMessage()); err != nil {
		return err
	}

	// 6. Trim: ao chegar no limite, mantém a metade mais recente e força push.
	count, err := e.repo.CommitCount()
	if err != nil {
		return err
	}
	if count >= e.maxCommits {
		keep := e.maxCommits / 2
		e.logf("branch %s chegou a %d commits (limite %d); reescrevendo história com os %d mais recentes", branch, count, e.maxCommits, keep)
		if err := e.repo.Trim(branch, keep); err != nil {
			return err
		}
		return e.pushOrReset(branch, true)
	}

	// 7. Push normal.
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
