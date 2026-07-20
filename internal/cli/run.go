package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
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

	logger := log.New(os.Stderr, "git-sync ", log.LstdFlags)
	eng := engine.New(repo, userName, engine.WithLogger(logger.Printf))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = eng.Run(ctx, abs, engine.Debounce, engine.PullInterval)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
