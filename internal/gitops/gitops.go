// Package gitops encapsula as operações de git usadas pelo git-sync,
// executando o binário `git` como subprocesso. Todas as operações rodam
// dentro de um diretório de repositório específico (campo Dir), o que
// permite que os testes trabalhem contra repositórios git reais e
// temporários sem depender de um repositório global.
package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultRemote é o remoto usado quando nenhum outro é informado.
const DefaultRemote = "origin"

// Repo representa um repositório git local no qual o git-sync atua.
type Repo struct {
	// Dir é o caminho (idealmente absoluto) do repositório.
	Dir string
	// Remote é o nome do remoto de sincronização (por padrão "origin").
	Remote string
	// Env são variáveis de ambiente extras injetadas nos subprocessos git
	// (ex: SSH_AUTH_SOCK para o agent in-process). Nil = herda o ambiente do processo.
	Env []string
}

// Open cria um Repo apontando para dir usando o remoto padrão.
func Open(dir string) *Repo {
	return &Repo{Dir: dir, Remote: DefaultRemote}
}

// git executa `git <args...>` dentro do repositório e devolve stdout.
// Em caso de falha, o erro carrega o stderr para diagnóstico.
func (r *Repo) git(args ...string) (string, error) {
	return r.gitInput(nil, args...)
}

// gitInput é como git, mas permite alimentar stdin (usado por commit-tree).
func (r *Repo) gitInput(stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &GitError{
			Args:   args,
			Stderr: strings.TrimSpace(stderr.String()),
			Err:    err,
		}
	}
	return stdout.String(), nil
}

// GitError descreve a falha de um comando git.
type GitError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, e.Stderr)
}

func (e *GitError) Unwrap() error { return e.Err }

// IsRepo indica se Dir é a raiz (ou parte) de um repositório git.
func (r *Repo) IsRepo() bool {
	out, err := r.git("rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// HasRemote indica se há ao menos um remoto configurado.
func (r *Repo) HasRemote() (bool, error) {
	out, err := r.git("remote")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// UserName devolve o `git config user.name`, preferindo o valor local do
// repositório e recorrendo ao global. Retorna erro se nenhum estiver
// configurado, pois o nome compõe a mensagem de commit.
func (r *Repo) UserName() (string, error) {
	if name := r.configValue("--local", "user.name"); name != "" {
		return name, nil
	}
	if name := r.configValue("--global", "user.name"); name != "" {
		return name, nil
	}
	return "", errors.New("git config user.name não configurado (nem local nem global)")
}

// configValue lê uma chave de config no escopo dado; devolve "" se ausente.
func (r *Repo) configValue(scope, key string) string {
	out, err := r.git("config", scope, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// CurrentBranch devolve o nome da branch atualmente em checkout.
func (r *Repo) CurrentBranch() (string, error) {
	out, err := r.git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// HasChanges indica se há mudanças (working tree ou índice) a serem commitadas.
func (r *Repo) HasChanges() (bool, error) {
	out, err := r.git("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// StageAll adiciona todas as mudanças ao índice (inclusive remoções).
func (r *Repo) StageAll() error {
	_, err := r.git("add", "-A")
	return err
}

// HasStagedChanges indica se há algo staged pronto para virar commit.
func (r *Repo) HasStagedChanges() (bool, error) {
	// `diff --cached --quiet` sai com código 1 quando há diferenças staged.
	_, err := r.git("diff", "--cached", "--quiet")
	if err == nil {
		return false, nil
	}
	var ge *GitError
	if errors.As(err, &ge) {
		var ee *exec.ExitError
		if errors.As(ge.Err, &ee) && ee.ExitCode() == 1 {
			return true, nil
		}
	}
	return false, err
}

// Commit cria um commit com a mensagem informada.
func (r *Repo) Commit(message string) error {
	_, err := r.git("commit", "-m", message)
	return err
}

// Pull executa `git pull` (merge, sem abrir editor). Retorna erro em
// qualquer falha — o chamador interpreta a falha como conflito de conteúdo
// e aplica a política "o remoto sempre vence".
func (r *Repo) Pull() error {
	_, err := r.git("pull", "--no-edit", "--no-rebase")
	return err
}

// Stash guarda as mudanças locais (tracked e untracked) fora da working
// tree, deixando-a limpa para um `pull`. Deve ser chamado apenas quando há
// mudanças (ver HasChanges); `git stash` sem mudanças é um no-op silencioso
// que atrapalharia o pareamento com StashPop.
func (r *Repo) Stash() error {
	_, err := r.git("stash", "push", "--include-untracked", "-m", "git-sync")
	return err
}

// StashPop reaplica o stash mais recente sobre a working tree. Retorna
// conflict=true quando a reaplicação colide (o mesmo arquivo mudou no
// remoto enquanto estava guardado); nesse caso o git preserva a entrada de
// stash e deixa caminhos não-mesclados, cabendo ao chamador aplicar "o
// remoto vence" (reset + StashDrop). A detecção usa `ls-files --unmerged`,
// que independe do idioma das mensagens do git.
func (r *Repo) StashPop() (conflict bool, err error) {
	if _, err = r.git("stash", "pop"); err == nil {
		return false, nil
	}
	out, uErr := r.git("ls-files", "--unmerged")
	if uErr == nil && strings.TrimSpace(out) != "" {
		return true, nil
	}
	return false, err
}

// StashDrop remove a entrada de stash mais recente. Funciona mesmo quando a
// working tree ainda está com um conflito de pop pendente.
func (r *Repo) StashDrop() error {
	_, err := r.git("stash", "drop")
	return err
}

// Fetch atualiza as refs do remoto sem tocar na working tree.
func (r *Repo) Fetch() error {
	_, err := r.git("fetch", r.Remote)
	return err
}

// ResetHardToRemote força a working tree e a branch a coincidirem com o
// estado do remoto, descartando quaisquer mudanças locais. Deve ser
// chamado após Fetch.
func (r *Repo) ResetHardToRemote(branch string) error {
	_, err := r.git("reset", "--hard", r.Remote+"/"+branch)
	return err
}

// Push envia a branch para o remoto. O retorno rejected é true quando o
// push foi recusado por divergência (non-fast-forward), caso em que o
// chamador aplica "o remoto sempre vence".
func (r *Repo) Push(branch string) (rejected bool, err error) {
	return r.push(branch, false)
}

// PushForce envia a branch com --force (usado após o trim de histórico).
// Também pode ser rejeitado por divergência do remoto.
func (r *Repo) PushForce(branch string) (rejected bool, err error) {
	return r.push(branch, true)
}

func (r *Repo) push(branch string, force bool) (bool, error) {
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, r.Remote, branch)
	_, err := r.git(args...)
	if err == nil {
		return false, nil
	}
	if isRejection(err) {
		return true, nil
	}
	return false, err
}

// isRejection reconhece a assinatura de um push recusado por divergência.
func isRejection(err error) bool {
	var ge *GitError
	if !errors.As(err, &ge) {
		return false
	}
	s := strings.ToLower(ge.Stderr)
	return strings.Contains(s, "rejected") ||
		strings.Contains(s, "non-fast-forward") ||
		strings.Contains(s, "fetch first") ||
		strings.Contains(s, "failed to push")
}

// CommitCount devolve o número de commits alcançáveis a partir do HEAD.
func (r *Repo) CommitCount() (int, error) {
	out, err := r.git("rev-list", "--count", "HEAD")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("contagem de commits inválida %q: %w", out, err)
	}
	return n, nil
}

// Trim reescreve a branch atual para conter apenas os n commits mais
// recentes, criando uma nova raiz órfã. Preserva exatamente as árvores
// (conteúdo) de cada commit mantido — ideal para arquivos binários, pois
// não há reaplicação de patches. Não faz push; o chamador é responsável
// por `push --force`.
func (r *Repo) Trim(branch string, n int) error {
	if n < 1 {
		return fmt.Errorf("limite de commits inválido: %d", n)
	}
	// SHAs dos n commits mais recentes, do mais novo para o mais antigo.
	out, err := r.git("rev-list", "-n", strconv.Itoa(n), "HEAD")
	if err != nil {
		return err
	}
	newestFirst := strings.Fields(strings.TrimSpace(out))
	if len(newestFirst) == 0 {
		return errors.New("trim: nenhum commit encontrado")
	}
	// Reconstrói do mais antigo para o mais novo.
	var newHead string
	for i := len(newestFirst) - 1; i >= 0; i-- {
		sha := newestFirst[i]
		tree, err := r.git("rev-parse", sha+"^{tree}")
		if err != nil {
			return err
		}
		tree = strings.TrimSpace(tree)
		msg, err := r.git("log", "-1", "--format=%B", sha)
		if err != nil {
			return err
		}
		args := []string{"commit-tree", tree}
		if newHead != "" {
			args = append(args, "-p", newHead)
		}
		created, err := r.gitInput([]byte(msg), args...)
		if err != nil {
			return err
		}
		newHead = strings.TrimSpace(created)
	}
	if newHead == "" {
		return errors.New("trim: nenhum commit para manter")
	}
	// Aponta a branch para a nova história. A working tree permanece
	// idêntica, pois a árvore do novo HEAD é igual à do HEAD anterior.
	if _, err := r.git("update-ref", "refs/heads/"+branch, newHead); err != nil {
		return err
	}
	return nil
}

// AbsDir devolve o caminho absoluto do repositório.
func (r *Repo) AbsDir() (string, error) {
	if filepath.IsAbs(r.Dir) {
		return r.Dir, nil
	}
	abs, err := filepath.Abs(r.Dir)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// Available verifica se o binário `git` está disponível no PATH.
func Available() error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("binário `git` não encontrado no PATH: %w", err)
	}
	return nil
}
