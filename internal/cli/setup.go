package cli

import (
	"fmt"
	"os"

	"github.com/racoonlemore/git-sync/internal/autostart"
)

// Setup implementa o comando `setup`:
//  1. roda as checagens do validate; aborta sem instalar nada se falharem;
//  2. se já instalado para o repositório, é no-op;
//  3. registra o autostart como serviço de usuário.
func Setup(repoPath string) error {
	abs, err := absDir(repoPath)
	if err != nil {
		return err
	}

	c, err := runChecks(abs)
	if err != nil {
		return err
	}
	if !c.Ready() {
		printChecks(c)
		return fmt.Errorf("setup abortado: %w", c.Err)
	}

	if c.Installed {
		fmt.Printf("git-sync já configurado para %s (unit %s); nada a fazer.\n", abs, c.UnitName)
		return nil
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("não foi possível determinar o caminho do binário: %w", err)
	}

	inst, err := autostart.New()
	if err != nil {
		return err
	}

	cfg := autostart.Config{Name: c.UnitName, RepoPath: abs, BinPath: binPath}
	if err := inst.Install(cfg); err != nil {
		return fmt.Errorf("falha ao registrar autostart (%s): %w", inst.Kind(), err)
	}

	fmt.Printf("git-sync configurado para %s.\n", abs)
	fmt.Printf("  autostart: %s (unit %s)\n", inst.Kind(), c.UnitName)
	fmt.Printf("  usuário git: %s\n", c.UserName)
	return nil
}
