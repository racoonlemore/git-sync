package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/racoonlemore/git-sync/internal/engine"
	"github.com/racoonlemore/git-sync/internal/gitops"
)

// Os testes do motor exercitam o ciclo completo contra repositórios git
// reais e temporários (remoto = repositório bare no filesystem, sem rede),
// verificando comportamento externo observável: estado do repo após o
// ciclo, conteúdo/contagem de commits e propagação ao remoto.

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func configUser(t *testing.T, dir, name string) {
	t.Helper()
	git(t, dir, "config", "user.name", name)
	git(t, dir, "config", "user.email", name+"@example.com")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRemoteWithClone cria um remoto bare com um commit inicial e devolve o
// caminho do bare, de um clone de trabalho e o nome da branch.
func newRemoteWithClone(t *testing.T, user string) (bare, clone, branch string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "remote.git")
	git(t, root, "init", "--bare", "-b", "main", bare)

	clone = filepath.Join(root, "clone-"+user)
	git(t, root, "clone", bare, clone)
	configUser(t, clone, user)

	writeFile(t, clone, "seed.txt", "inicial")
	git(t, clone, "add", "-A")
	git(t, clone, "commit", "-m", "seed")
	git(t, clone, "push", "-u", "origin", "main")

	branch = git(t, clone, "rev-parse", "--abbrev-ref", "HEAD")
	return bare, clone, branch
}

// cloneFrom cria um clone adicional do remoto (segunda "máquina").
func cloneFrom(t *testing.T, bare, user string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "other-"+user)
	git(t, filepath.Dir(dir), "clone", bare, dir)
	configUser(t, dir, user)
	return dir
}

func commitCount(t *testing.T, dir, ref string) int {
	t.Helper()
	n, err := strconv.Atoi(git(t, dir, "rev-list", "--count", ref))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func isResourceExhausted(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no space left on device") ||
		strings.Contains(s, "too many open files")
}

func fixedClock() func() time.Time {
	ts := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	return func() time.Time { return ts }
}

func newEngine(repo engine.Repo, opts ...engine.Option) *engine.Engine {
	base := []engine.Option{engine.WithClock(fixedClock()), engine.WithLogger(func(string, ...any) {})}
	return engine.New(repo, "Alice", append(base, opts...)...)
}

// TestSyncCycleCommitsAndPushes cobre o caminho feliz: mudança local vira
// commit com a mensagem padrão e é propagada ao remoto.
func TestSyncCycleCommitsAndPushes(t *testing.T) {
	bare, clone, _ := newRemoteWithClone(t, "alice")
	writeFile(t, clone, "novo.bin", "conteúdo binário")

	eng := newEngine(gitops.Open(clone))
	if err := eng.SyncCycle(); err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}

	// Commit criado com a mensagem no formato esperado.
	msg := git(t, clone, "log", "-1", "--format=%s")
	if want := "sync 20-07-2026 09:30 (Alice)"; msg != want {
		t.Errorf("mensagem de commit = %q, quer %q", msg, want)
	}

	// Propagado ao remoto: um clone novo enxerga o arquivo.
	verify := cloneFrom(t, bare, "verify")
	if _, err := os.Stat(filepath.Join(verify, "novo.bin")); err != nil {
		t.Errorf("arquivo não chegou ao remoto: %v", err)
	}
}

// TestSyncCycleNoChangesIsNoop garante que, sem mudanças locais, o ciclo
// não cria commit algum.
func TestSyncCycleNoChangesIsNoop(t *testing.T) {
	_, clone, _ := newRemoteWithClone(t, "alice")
	before := commitCount(t, clone, "HEAD")

	if err := newEngine(gitops.Open(clone)).SyncCycle(); err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	if after := commitCount(t, clone, "HEAD"); after != before {
		t.Errorf("contagem de commits mudou sem alterações: %d -> %d", before, after)
	}
}

// TestConflictRemoteWins cobre ADR 0001: com conflito de conteúdo (arquivo
// alterado local e remotamente), o remoto prevalece via reset --hard e o
// ciclo termina sem commit local.
func TestConflictRemoteWins(t *testing.T) {
	bare, clone, _ := newRemoteWithClone(t, "alice")

	// Segunda máquina altera seed.txt e publica.
	other := cloneFrom(t, bare, "bob")
	writeFile(t, other, "seed.txt", "versão do bob")
	git(t, other, "add", "-A")
	git(t, other, "commit", "-m", "bob edita seed")
	git(t, other, "push", "origin", "main")
	remoteHead := git(t, other, "rev-parse", "HEAD")

	// Máquina local altera o MESMO arquivo (mudança não commitada).
	writeFile(t, clone, "seed.txt", "versão da alice")

	if err := newEngine(gitops.Open(clone)).SyncCycle(); err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}

	// Remoto venceu: HEAD local == HEAD do remoto, conteúdo do bob.
	if localHead := git(t, clone, "rev-parse", "HEAD"); localHead != remoteHead {
		t.Errorf("HEAD local %s != HEAD remoto %s (esperado reset --hard)", localHead, remoteHead)
	}
	got, _ := os.ReadFile(filepath.Join(clone, "seed.txt"))
	if string(got) != "versão do bob" {
		t.Errorf("conteúdo local = %q, esperado o do remoto", got)
	}
}

// TestPullFirstComArvoreSuja trava a regressão que quebrou em versões novas
// do git: o ciclo roda com a working tree suja (mudança local pendente) e o
// remoto avançou OUTRO arquivo. O pull-first tem que incorporar o remoto e
// preservar a mudança local (nada de "Cannot fast-forward your working
// tree"), propagando ambos ao remoto no fim.
func TestPullFirstComArvoreSuja(t *testing.T) {
	bare, clone, _ := newRemoteWithClone(t, "alice")

	// Ambos os arquivos existem no remoto.
	writeFile(t, clone, "racoon.bin", "r0")
	writeFile(t, clone, "lemore.bin", "l0")
	git(t, clone, "add", "-A")
	git(t, clone, "commit", "-m", "bases iniciais")
	git(t, clone, "push", "origin", "main")

	// Segunda máquina avança APENAS lemore.bin.
	other := cloneFrom(t, bare, "bob")
	writeFile(t, other, "lemore.bin", "l1-bob")
	git(t, other, "add", "-A")
	git(t, other, "commit", "-m", "bob edita lemore")
	git(t, other, "push", "origin", "main")

	// Máquina local suja APENAS racoon.bin (arquivo diferente), sem commitar.
	writeFile(t, clone, "racoon.bin", "r1-alice")

	if err := newEngine(gitops.Open(clone)).SyncCycle(); err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}

	// Local: mudança da alice preservada + avanço do bob incorporado.
	if got, _ := os.ReadFile(filepath.Join(clone, "racoon.bin")); string(got) != "r1-alice" {
		t.Errorf("racoon.bin local = %q, esperado r1-alice (preservado)", got)
	}
	if got, _ := os.ReadFile(filepath.Join(clone, "lemore.bin")); string(got) != "l1-bob" {
		t.Errorf("lemore.bin local = %q, esperado l1-bob (do remoto)", got)
	}
	// Remoto: recebeu a mudança da alice sobre o estado do bob.
	verify := cloneFrom(t, bare, "verify")
	if got, _ := os.ReadFile(filepath.Join(verify, "racoon.bin")); string(got) != "r1-alice" {
		t.Errorf("racoon.bin no remoto = %q, esperado r1-alice", got)
	}
	if got, _ := os.ReadFile(filepath.Join(verify, "lemore.bin")); string(got) != "l1-bob" {
		t.Errorf("lemore.bin no remoto = %q, esperado l1-bob", got)
	}
}

// TestTrimAoExceLimite cobre a política de N commits: ao ultrapassar o
// limite, a história é reescrita mantendo só os N mais recentes e forçada
// ao remoto.
func TestTrimAoExceLimite(t *testing.T) {
	const max = 4 // dispara ao chegar em 4 commits e mantém 2
	bare, clone, _ := newRemoteWithClone(t, "alice")

	repo := gitops.Open(clone)
	eng := newEngine(repo, engine.WithMaxCommits(max))

	sync := func(i int) {
		t.Helper()
		writeFile(t, clone, "dados.bin", "rev-"+strconv.Itoa(i))
		if err := eng.SyncCycle(); err != nil {
			t.Fatalf("SyncCycle %d: %v", i, err)
		}
	}

	// seed + 2 ciclos = 3 commits: abaixo do limite, nada é reescrito.
	sync(0)
	sync(1)
	if n := commitCount(t, clone, "HEAD"); n != 3 {
		t.Fatalf("antes do limite: commits = %d, quer 3", n)
	}
	raiz := git(t, clone, "rev-list", "--max-parents=0", "HEAD")

	// Commit 4 = limite: fica só a metade mais recente.
	sync(2)
	if n := commitCount(t, clone, "HEAD"); n != max/2 {
		t.Errorf("após o trim: commits locais = %d, quer %d", n, max/2)
	}
	if n := commitCount(t, bare, "main"); n != max/2 {
		t.Errorf("após o trim: commits no remoto = %d, quer %d (push --force)", n, max/2)
	}
	if git(t, clone, "rev-list", "--max-parents=0", "HEAD") == raiz {
		t.Error("a raiz não mudou: história não foi reescrita")
	}

	// O próximo ciclo só acrescenta (sem nova reescrita até o limite).
	novaRaiz := git(t, clone, "rev-list", "--max-parents=0", "HEAD")
	sync(3)
	if n := commitCount(t, clone, "HEAD"); n != max/2+1 {
		t.Errorf("depois do trim: commits = %d, quer %d", n, max/2+1)
	}
	if git(t, clone, "rev-list", "--max-parents=0", "HEAD") != novaRaiz {
		t.Error("ciclo abaixo do limite reescreveu a história")
	}
	got, _ := os.ReadFile(filepath.Join(clone, "dados.bin"))
	if string(got) != "rev-3" {
		t.Errorf("conteúdo HEAD = %q, quer rev-3", got)
	}
}

// hookRepo decora um *gitops.Repo para simular uma segunda máquina que
// publica no remoto logo após o commit local — provocando rejeição de push
// por divergência dentro do mesmo ciclo.
type hookRepo struct {
	*gitops.Repo
	afterCommit func()
}

func (h *hookRepo) Commit(msg string) error {
	if err := h.Repo.Commit(msg); err != nil {
		return err
	}
	if h.afterCommit != nil {
		h.afterCommit()
	}
	return nil
}

// TestPushRejeitadoRemoteWins cobre ADR 0002: se o push é rejeitado por
// divergência (outra máquina sincronizou entre o pull e o push), a máquina
// descarta o ciclo local via reset --hard, sem coordenação.
func TestPushRejeitadoRemoteWins(t *testing.T) {
	bare, clone, _ := newRemoteWithClone(t, "alice")

	var remoteHead string
	hook := &hookRepo{
		Repo: gitops.Open(clone),
		afterCommit: func() {
			// Segunda máquina publica um commit divergente.
			other := cloneFrom(t, bare, "bob")
			writeFile(t, other, "bob.bin", "trabalho do bob")
			git(t, other, "add", "-A")
			git(t, other, "commit", "-m", "bob sincroniza primeiro")
			git(t, other, "push", "origin", "main")
			remoteHead = git(t, other, "rev-parse", "HEAD")
		},
	}

	writeFile(t, clone, "alice.bin", "trabalho da alice")
	if err := newEngine(hook).SyncCycle(); err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}

	// Ciclo abandonado: máquina local convergiu para o estado do remoto.
	if localHead := git(t, clone, "rev-parse", "HEAD"); localHead != remoteHead {
		t.Errorf("HEAD local %s != HEAD remoto %s (esperado reset ao remoto)", localHead, remoteHead)
	}
	// O trabalho local divergente foi descartado.
	if _, err := os.Stat(filepath.Join(clone, "alice.bin")); !os.IsNotExist(err) {
		t.Errorf("alice.bin deveria ter sido descartado, err=%v", err)
	}
	// O estado do remoto (bob) está presente.
	if _, err := os.Stat(filepath.Join(clone, "bob.bin")); err != nil {
		t.Errorf("bob.bin (estado do remoto) ausente: %v", err)
	}
}

// TestPullCycleAdiaComMudancasPendentes garante que o pull horário não
// descarta mudanças locais ainda pendentes.
func TestPullCycleAdiaComMudancasPendentes(t *testing.T) {
	_, clone, _ := newRemoteWithClone(t, "alice")
	writeFile(t, clone, "pendente.bin", "editando")

	if err := newEngine(gitops.Open(clone)).PullCycle(); err != nil {
		t.Fatalf("PullCycle: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(clone, "pendente.bin"))
	if string(got) != "editando" {
		t.Errorf("mudança pendente foi perdida no pull horário: %q", got)
	}
}

// TestRunSincronizaAoDetectarMudanca exercita o laço Run completo: uma
// mudança de arquivo, após o debounce, deve virar commit e ser propagada
// ao remoto — sem intervenção manual.
func TestRunSincronizaAoDetectarMudanca(t *testing.T) {
	bare, clone, _ := newRemoteWithClone(t, "alice")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbg := engine.WithLogger(func(f string, a ...any) { t.Logf("[engine] "+f, a...) })
	done := make(chan error, 1)
	go func() {
		done <- newEngine(gitops.Open(clone), dbg).Run(ctx, clone, 150*time.Millisecond, time.Hour)
	}()

	// Dá um instante para o watcher registrar os diretórios, então edita.
	time.Sleep(100 * time.Millisecond)
	writeFile(t, clone, "novo.bin", "sincroniza isto")

	// Aguarda o remoto receber o arquivo (debounce + push).
	verify := cloneFrom(t, bare, "verify")
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			// Ambientes com inotify esgotado (ex.: sandboxes de CI locais
			// com muitos watchers ativos) não conseguem criar o watcher;
			// pular em vez de falhar, já que não é defeito do código.
			if isResourceExhausted(err) {
				t.Skipf("watcher indisponível neste ambiente: %v", err)
			}
			t.Fatalf("Run encerrou prematuramente: %v", err)
		default:
		}
		git(t, verify, "pull", "--quiet")
		if _, err := os.Stat(filepath.Join(verify, "novo.bin")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("mudança não foi sincronizada ao remoto dentro do prazo")
		}
		time.Sleep(150 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Run não encerrou após cancelamento do contexto")
	}
}

// TestTrimDeOutraMaquinaNaoDescartaMudancaSemConflito reproduz o regime
// permanente de um repositório com a branch cheia: toda sincronização faz
// trim e reescreve a história, então o pull da outra máquina encontra
// "unrelated histories". Uma edição local em OUTRO arquivo não conflita e
// não pode ser descartada (ADR 0001 só descarta o que conflita).
func TestTrimDeOutraMaquinaNaoDescartaMudancaSemConflito(t *testing.T) {
	const limite = 4
	bare, alice, branch := newRemoteWithClone(t, "alice")
	bob := cloneFrom(t, bare, "bob")
	engA := newEngine(gitops.Open(alice), engine.WithMaxCommits(limite))
	engB := newEngine(gitops.Open(bob), engine.WithMaxCommits(limite))

	// Enche a branch até um commit antes do limite, e Bob acompanha.
	for i := 0; i < limite-2; i++ {
		writeFile(t, alice, "a.txt", "versão "+strconv.Itoa(i))
		if err := engA.SyncCycle(); err != nil {
			t.Fatal(err)
		}
	}
	if err := engB.PullCycle(); err != nil {
		t.Fatal(err)
	}

	// Alice sincroniza de novo: chega ao limite, trim + push --force.
	writeFile(t, alice, "a.txt", "versão da alice pós-trim")
	if err := engA.SyncCycle(); err != nil {
		t.Fatal(err)
	}

	// Bob editou outro arquivo nesse meio tempo e sincroniza.
	writeFile(t, bob, "b.txt", "edição do bob")
	if err := engB.SyncCycle(); err != nil {
		t.Fatalf("SyncCycle do bob: %v", err)
	}

	got := git(t, bob, "show", "origin/"+branch+":a.txt")
	if got != "versão da alice pós-trim" {
		t.Errorf("a.txt no remoto = %q, quer a versão da alice", got)
	}
	data, err := os.ReadFile(filepath.Join(bob, "b.txt"))
	if err != nil || string(data) != "edição do bob" {
		t.Fatalf("edição do bob sumiu da working tree: %q, %v", data, err)
	}
	git(t, bob, "fetch", "origin")
	if got := git(t, bob, "show", "origin/"+branch+":b.txt"); got != "edição do bob" {
		t.Errorf("b.txt no remoto = %q, quer a edição do bob", got)
	}
}

// TestTrimDeOutraMaquinaComConflitoRemotoVence: depois do trim da outra
// máquina, uma edição local no MESMO arquivo é conflito real e o remoto vence.
func TestTrimDeOutraMaquinaComConflitoRemotoVence(t *testing.T) {
	const limite = 4
	bare, alice, branch := newRemoteWithClone(t, "alice")
	bob := cloneFrom(t, bare, "bob")
	engA := newEngine(gitops.Open(alice), engine.WithMaxCommits(limite))
	engB := newEngine(gitops.Open(bob), engine.WithMaxCommits(limite))

	for i := 0; i < limite-1; i++ { // a última volta chega ao limite e faz trim
		writeFile(t, alice, "a.txt", "versão "+strconv.Itoa(i))
		if err := engA.SyncCycle(); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := engB.PullCycle(); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeFile(t, bob, "a.txt", "versão do bob")
	if err := engB.SyncCycle(); err != nil {
		t.Fatalf("SyncCycle do bob: %v", err)
	}
	if got := git(t, bob, "show", "HEAD:a.txt"); got != "versão 2" {
		t.Errorf("a.txt = %q, quer a versão do remoto", got)
	}
	if st := git(t, bob, "status", "--porcelain"); st != "" {
		t.Errorf("working tree suja depois do remoto-vence: %q", st)
	}
	if n := git(t, bob, "stash", "list"); n != "" {
		t.Errorf("stash sobrou: %q", n)
	}
	if got := git(t, bob, "rev-parse", "HEAD"); got != git(t, bob, "rev-parse", "origin/"+branch) {
		t.Error("HEAD do bob não é o remoto")
	}
}

// TestPullSemRedePreservaMudancas: com o remoto inacessível, o ciclo falha
// mas a edição local continua na working tree para o próximo ciclo.
func TestPullSemRedePreservaMudancas(t *testing.T) {
	_, clone, _ := newRemoteWithClone(t, "alice")
	git(t, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "nao-existe.git"))
	writeFile(t, clone, "novo.txt", "trabalho local")

	if err := newEngine(gitops.Open(clone)).SyncCycle(); err == nil {
		t.Fatal("esperava erro com o remoto inacessível")
	}
	data, err := os.ReadFile(filepath.Join(clone, "novo.txt"))
	if err != nil || string(data) != "trabalho local" {
		t.Fatalf("mudança local perdida: %q, %v", data, err)
	}
	if n := git(t, clone, "stash", "list"); n != "" {
		t.Errorf("stash sobrou: %q", n)
	}
}

// TestTrimNaoSobrescrevePushConcorrente cobre ADR 0002 no push do trim: se
// outra máquina publicou entre o pull e o push --force, o push tem de ser
// rejeitado (force-with-lease) e o remoto vence, em vez de o trim apagar
// o commit da outra máquina.
func TestTrimNaoSobrescrevePushConcorrente(t *testing.T) {
	const limite = 4
	bare, clone, _ := newRemoteWithClone(t, "alice")
	repo := gitops.Open(clone)
	for i := 0; i < limite-2; i++ { // um commit antes do limite
		writeFile(t, clone, "alice.bin", "rev-"+strconv.Itoa(i))
		if err := newEngine(repo, engine.WithMaxCommits(limite)).SyncCycle(); err != nil {
			t.Fatal(err)
		}
	}

	var bobHead string
	hook := &hookRepo{Repo: repo, afterCommit: func() {
		other := cloneFrom(t, bare, "bob")
		writeFile(t, other, "bob.bin", "trabalho do bob")
		git(t, other, "add", "-A")
		git(t, other, "commit", "-m", "bob sincroniza no meio")
		git(t, other, "push", "origin", "main")
		bobHead = git(t, other, "rev-parse", "HEAD")
	}}

	writeFile(t, clone, "alice.bin", "rev-final") // chega ao limite: trim
	if err := newEngine(hook, engine.WithMaxCommits(limite)).SyncCycle(); err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}

	if got := git(t, bare, "rev-parse", "main"); got != bobHead {
		t.Fatalf("remoto = %s, quer o commit do bob %s (o trim sobrescreveu o push concorrente)", got, bobHead)
	}
	if got := git(t, clone, "rev-parse", "HEAD"); got != bobHead {
		t.Errorf("alice não convergiu para o remoto: HEAD %s", got)
	}
}
