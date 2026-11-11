package cli

import (
	"fmt"
	"runtime"

	"github.com/racoonlemore/git-sync/internal/update"
)

// UpdateCheck implementa `update check`: informa se há release mais novo.
// A resposta fica em cache por 12h; force consulta o GitHub de novo.
func UpdateCheck(version string, force bool) error {
	r, err := update.Default(version).Check(force)
	if err != nil {
		return fmt.Errorf("falha ao verificar atualização: %w", err)
	}
	if !r.Pending {
		fmt.Printf("git-sync %s está na versão mais recente.\n", version)
		return nil
	}
	fmt.Printf("Atualização disponível: %s → %s\n", r.Current, r.Target)
	fmt.Println("Rode `git-sync update apply` para instalar.")
	return nil
}

// UpdateApply implementa `update apply`: baixa o último release, confere o
// sha256, preserva a chave SSH embutida e troca o binário em disco.
func UpdateApply(version string) error {
	res, target, err := update.Default(version).Apply()
	if err != nil {
		return fmt.Errorf("falha ao atualizar: %w", err)
	}
	if res == update.UpToDate {
		fmt.Printf("git-sync %s está na versão mais recente.\n", version)
		return nil
	}
	fmt.Printf("git-sync atualizado: %s → %s\n", version, target)
	fmt.Println("Os processos em background continuam na versão antiga até reiniciarem:")
	fmt.Println("  " + restartHint())
	return nil
}

func restartHint() string {
	switch runtime.GOOS {
	case "linux":
		return "systemctl --user restart 'git-sync-*'"
	case "darwin":
		return "faça logout/login, ou launchctl kickstart -k gui/$(id -u)/<unit> para cada repositório"
	case "windows":
		return "faça logout/login, ou reinicie as tarefas git-sync-* no Agendador de Tarefas"
	}
	return "reinicie os serviços git-sync"
}
