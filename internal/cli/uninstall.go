package cli

import (
	"fmt"

	"github.com/racoonlemore/git-sync/internal/autostart"
)

// Uninstall implementa o comando `uninstall`: remove a unit/tarefa de
// autostart do repositório atual (parando o processo em background). Escopo
// estritamente local — nunca mexe no remote nem no histórico de commits.
func Uninstall(repoPath string) error {
	abs, err := absDir(repoPath)
	if err != nil {
		return err
	}

	name := autostart.UnitName(abs)
	inst, err := autostart.New()
	if err != nil {
		return err
	}

	installed, err := inst.IsInstalled(name)
	if err != nil {
		return err
	}
	if !installed {
		fmt.Printf("git-sync não está instalado para %s (unit %s); nada a fazer.\n", abs, name)
		return nil
	}

	if err := inst.Uninstall(name); err != nil {
		return fmt.Errorf("falha ao remover autostart (%s): %w", inst.Kind(), err)
	}

	fmt.Printf("git-sync removido de %s (unit %s).\n", abs, name)
	fmt.Println("Remote e histórico de commits não foram alterados.")
	return nil
}
