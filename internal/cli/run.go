package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/racoonlemore/git-sync/internal/engine"
	"github.com/racoonlemore/git-sync/internal/gitops"
)

// Run implementa o comando interno `run`: o processo em background que
// observa o repositório e sincroniza. É o comando que a unit/tarefa de
// autostart invoca; não se espera que o usuário rode manualmente.
func Run(repoPath string) error {
	abs, err := absDir(repoPath)
	if err != nil {
		return err
	}

	if err := gitops.Available(); err != nil {
		return err
	}

	repo := gitops.Open(abs)
	if !repo.IsRepo() {
		return fmt.Errorf("%s não é um repositório git", abs)
	}

	// user.name é pré-condição validada no setup; revalidamos para falhar
	// cedo com mensagem clara caso a config tenha sido removida.
	userName, err := repo.UserName()
	if err != nil {
		return err
	}

	// Log de execução local: além do stderr (journal do serviço), grava num
	// arquivo na raiz do repo para o usuário debugar facilmente. O arquivo é
	// ignorado pelo git e pelo watcher (ver ensureLogFile / isIgnored).
	logPath, err := ensureLogFile(abs)
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("não foi possível abrir o log %s: %w", logPath, err)
	}
	defer logFile.Close()

	logger := log.New(io.MultiWriter(os.Stderr, logFile), "git-sync ", log.LstdFlags)
	eng := engine.New(repo, userName, engine.WithLogger(logger.Printf))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = eng.Run(ctx, abs, engine.Debounce, engine.PullInterval)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// ensureLogFile garante que o arquivo de log local exista na raiz do repo e
// esteja listado no .gitignore, para que ele nunca seja commitado nem
// propagado ao remoto. Devolve o caminho do log. É idempotente.
func ensureLogFile(repoDir string) (string, error) {
	logPath := filepath.Join(repoDir, engine.LogFileName)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("não foi possível criar o log %s: %w", logPath, err)
	}
	f.Close()
	if err := ensureGitignoreLine(repoDir, engine.LogFileName); err != nil {
		return "", err
	}
	return logPath, nil
}

// ensureGitignoreLine adiciona line ao .gitignore do repositório se ainda
// não estiver lá (criando o arquivo se necessário). Idempotente.
func ensureGitignoreLine(repoDir, line string) error {
	giPath := filepath.Join(repoDir, ".gitignore")
	data, err := os.ReadFile(giPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == line {
			return nil // já ignorado
		}
	}
	f, err := os.OpenFile(giPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n" // não colar na última linha sem newline
	}
	_, err = f.WriteString(prefix + line + "\n")
	return err
}
