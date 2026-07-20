package cli

import (
	"fmt"
	"strings"

	"github.com/racoonlemore/git-sync/internal/autostart"
	"github.com/racoonlemore/git-sync/internal/gitops"
)

// Checks é o resultado das checagens de prontidão de um repositório.
type Checks struct {
	RepoPath  string
	UnitName  string
	IsRepo    bool
	HasRemote bool
	UserName  string // vazio se não configurado
	Installed bool
	// Err é a primeira condição impeditiva encontrada (repo/remote/user).
	Err error
}

// Ready indica se o repositório atende aos pré-requisitos para o setup.
func (c Checks) Ready() bool { return c.Err == nil }

// runChecks executa as checagens do `validate` sobre repoPath (absoluto):
// (1) é repositório git; (2) tem remote; (3) tem user.name (local/global);
// (4) já está instalado (existência da unit/tarefa de autostart).
func runChecks(repoPath string) (Checks, error) {
	c := Checks{RepoPath: repoPath, UnitName: autostart.UnitName(repoPath)}

	if err := gitops.Available(); err != nil {
		c.Err = err
		return c, nil
	}

	repo := gitops.Open(repoPath)

	c.IsRepo = repo.IsRepo()
	if !c.IsRepo {
		c.Err = fmt.Errorf("%s não é um repositório git (git-sync nunca inicializa nem clona)", repoPath)
		return c, nil
	}

	hasRemote, err := repo.HasRemote()
	if err != nil {
		return c, err
	}
	c.HasRemote = hasRemote
	if !hasRemote {
		c.Err = fmt.Errorf("repositório sem remote configurado; git-sync depende de um remoto")
		return c, nil
	}

	name, err := repo.UserName()
	if err != nil {
		c.Err = err
		return c, nil
	}
	c.UserName = name

	inst, err := autostart.New()
	if err != nil {
		return c, err
	}
	installed, err := inst.IsInstalled(c.UnitName)
	if err != nil {
		return c, err
	}
	c.Installed = installed

	return c, nil
}

// Validate implementa o comando `validate`: reporta o estado das
// checagens e retorna erro (código de saída != 0) se o repositório não
// estiver pronto.
func Validate(repoPath string) error {
	abs, err := absDir(repoPath)
	if err != nil {
		return err
	}
	c, err := runChecks(abs)
	if err != nil {
		return err
	}
	printChecks(c)
	if !c.Ready() {
		return c.Err
	}
	return nil
}

func printChecks(c Checks) {
	line := func(ok bool, msg string) {
		mark := "✗"
		if ok {
			mark = "✓"
		}
		fmt.Printf("  %s %s\n", mark, msg)
	}
	fmt.Printf("git-sync validate — %s\n", c.RepoPath)
	line(c.IsRepo, "repositório git existente")
	line(c.HasRemote, "remote configurado")
	line(c.UserName != "", userNameMsg(c.UserName))
	line(c.Installed, installedMsg(c.Installed, c.UnitName))
	if c.Ready() {
		fmt.Println("Repositório pronto para o git-sync.")
	} else {
		fmt.Printf("Não pronto: %v\n", c.Err)
	}
}

func userNameMsg(name string) string {
	if name == "" {
		return "git config user.name (local ou global)"
	}
	return "git config user.name = " + name
}

func installedMsg(installed bool, unit string) string {
	if installed {
		return "git-sync instalado (unit " + unit + ")"
	}
	return "git-sync não instalado para este repositório (unit " + unit + ")"
}

// absDir normaliza e valida o caminho do repositório.
func absDir(repoPath string) (string, error) {
	repoPath = strings.TrimSpace(repoPath)
	r := gitops.Repo{Dir: repoPath}
	return r.AbsDir()
}
