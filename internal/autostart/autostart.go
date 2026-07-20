// Package autostart abstrai o registro do git-sync como serviço de
// usuário (nunca de sistema/boot), atrás de uma interface única por SO
// (seam 2).
//
// O objetivo do serviço de usuário é ter acesso direto às credenciais git
// do usuário (chave SSH, credential helper, git config) sem configuração
// extra — implicando que o processo só inicia após o login.
//
// A geração de nomes e do conteúdo das units é pura e testável em qualquer
// plataforma; apenas Install/Uninstall/IsInstalled tocam o sistema real
// (systemctl/launchctl/schtasks), o que fica fora da cobertura
// automatizada (verificação manual por plataforma).
package autostart

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Config descreve o que registrar no autostart.
type Config struct {
	// Name é o nome determinístico da unit/tarefa (ver UnitName).
	Name string
	// RepoPath é o caminho absoluto do repositório sincronizado.
	RepoPath string
	// BinPath é o caminho absoluto do binário git-sync.
	BinPath string
}

// Installer registra/desregistra o git-sync como serviço de usuário.
// Cada plataforma tem sua implementação; use New para obter a correta.
type Installer interface {
	// Install registra o autostart de usuário (idempotência é do chamador).
	Install(cfg Config) error
	// Uninstall remove a unit/tarefa de autostart pelo nome.
	Uninstall(name string) error
	// IsInstalled indica se já existe autostart registrado com esse nome.
	IsInstalled(name string) (bool, error)
	// Kind descreve o mecanismo (para mensagens ao usuário).
	Kind() string
}

// New devolve o Installer adequado ao sistema operacional atual.
func New() (Installer, error) {
	switch runtime.GOOS {
	case "linux":
		return &SystemdInstaller{}, nil
	case "darwin":
		return &LaunchdInstaller{}, nil
	case "windows":
		return &SchtasksInstaller{}, nil
	default:
		return nil, fmt.Errorf("autostart não suportado no SO %q", runtime.GOOS)
	}
}

// unsafeChars casa qualquer caractere não seguro para nomes de
// unit/tarefa/arquivo nas três plataformas.
var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// UnitName gera o nome determinístico da unit/tarefa no formato
// `git-sync-<nome_da_pasta>-<hash do caminho absoluto>`, idêntico em todas
// as plataformas. Esse nome é também o critério que valida se o git-sync
// já está instalado para um repositório (não há marcador dentro do repo).
func UnitName(repoPath string) string {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	abs = filepath.Clean(abs)

	base := unsafeChars.ReplaceAllString(filepath.Base(abs), "-")
	base = strings.Trim(base, "-._")
	if base == "" {
		base = "repo"
	}

	sum := sha256.Sum256([]byte(abs))
	hash := hex.EncodeToString(sum[:])[:12]

	return fmt.Sprintf("git-sync-%s-%s", base, hash)
}
