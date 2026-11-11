// Command git-sync é uma CLI que sincroniza automaticamente um repositório
// git já existente com seu remoto: observa mudanças na pasta e, com
// debounce, faz pull/add/commit/push; faz pull periódico; mantém o
// histórico enxuto; e resolve conflitos/divergências com "o remoto sempre
// vence". Ver docs/specs/git-sync.md e docs/adr para o domínio completo.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/racoonlemore/git-sync/internal/cli"
)

// version é injetada em tempo de build via -ldflags "-X main.version=...".
var version = "dev"

const usage = `git-sync — sincronização automática de um repositório git com seu remoto.

Uso:
  git-sync <comando> [--repo <caminho>]

Comandos:
  validate    Verifica se o repositório está pronto (repo git, remote,
              user.name) e se o git-sync já está instalado para ele.
  setup       Roda as checagens do validate e, se passarem, registra o
              git-sync para iniciar no login do usuário (serviço de usuário).
  uninstall   Remove o autostart do repositório atual e para o processo.
              Não altera o remote nem o histórico de commits.
  run         Executa o processo de sincronização em background (usado pela
              unit/tarefa de autostart; normalmente não invocado à mão).
  add-key     Embute uma chave SSH privada no binário (opcional; sem ela o
              git usa o ambiente do usuário normalmente).
              Uso: git-sync add-key <caminho-da-chave-privada>
  remove-key  Remove a chave SSH embutida do binário.
  update      Atualiza o git-sync a partir dos releases do GitHub.
              update check [--force]  informa se há versão nova (cache de 12h)
              update apply            baixa, confere o sha256 e troca o binário
                                      (preserva a chave SSH embutida)
              Repositório privado: defina GITHUB_TOKEN.
  version     Mostra a versão do git-sync.

Flags:
  --repo <caminho>   Repositório alvo (padrão: diretório atual).

Parâmetros do daemon são fixos: debounce de 3 minutos, pull a cada 1 hora,
limite de 20 commits.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	command := os.Args[1]
	switch command {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	case "-v", "--version", "version":
		fmt.Printf("git-sync %s\n", version)
		return
	case "update":
		if err := runUpdate(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fs := flag.NewFlagSet(command, flag.ExitOnError)
	repo := fs.String("repo", ".", "caminho do repositório git alvo")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	_ = fs.Parse(os.Args[2:])

	var err error
	switch command {
	case "validate":
		err = cli.Validate(*repo)
	case "setup":
		err = cli.Setup(*repo)
	case "uninstall":
		err = cli.Uninstall(*repo)
	case "run":
		err = cli.Run(*repo)
	case "add-key":
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "uso: git-sync add-key <caminho-da-chave-privada>")
			os.Exit(2)
		}
		err = cli.AddKey(fs.Arg(0))
	case "remove-key":
		err = cli.RemoveKey()
	default:
		fmt.Fprintf(os.Stderr, "comando desconhecido %q\n\n", command)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
}

// runUpdate despacha `update [check|apply] [--force]`. Sem subcomando, faz
// o check.
func runUpdate(args []string) error {
	sub := "check"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("update "+sub, flag.ExitOnError)
	force := fs.Bool("force", false, "consulta o GitHub mesmo com o cache válido")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	_ = fs.Parse(args)

	switch sub {
	case "check":
		return cli.UpdateCheck(version, *force)
	case "apply":
		return cli.UpdateApply(version)
	}
	return fmt.Errorf("subcomando desconhecido %q (use check ou apply)", sub)
}
