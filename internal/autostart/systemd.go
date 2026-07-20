package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SystemdInstaller registra o git-sync como unit de `systemd --user`.
type SystemdInstaller struct {
	// homeDir sobrescreve o HOME (usado em testes). Vazio => os.UserHomeDir.
	homeDir string
	// runner executa comandos externos; injetável em testes. Vazio => exec real.
	runner func(name string, args ...string) error
}

func (s *SystemdInstaller) Kind() string { return "unit de systemd --user" }

// unitFileName é o nome do arquivo da unit (.service).
func (s *SystemdInstaller) unitFileName(name string) string {
	return name + ".service"
}

// unitPath devolve o caminho do arquivo da unit em ~/.config/systemd/user.
func (s *SystemdInstaller) unitPath(name string) (string, error) {
	home, err := s.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", s.unitFileName(name)), nil
}

// unitContent gera o conteúdo do arquivo .service (função pura, testável).
func (s *SystemdInstaller) unitContent(cfg Config) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=git-sync para %s\n", cfg.RepoPath)
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n\n")

	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", cfg.RepoPath)
	fmt.Fprintf(&b, "ExecStart=%s run --repo %s\n", cfg.BinPath, cfg.RepoPath)
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=10\n\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

func (s *SystemdInstaller) Install(cfg Config) error {
	path, err := s.unitPath(cfg.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(s.unitContent(cfg)), 0o644); err != nil {
		return err
	}
	if err := s.run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	return s.run("systemctl", "--user", "enable", "--now", s.unitFileName(cfg.Name))
}

func (s *SystemdInstaller) Uninstall(name string) error {
	// Ignora erro de disable (a unit pode não estar ativa).
	_ = s.run("systemctl", "--user", "disable", "--now", s.unitFileName(name))
	path, err := s.unitPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.run("systemctl", "--user", "daemon-reload")
}

// IsInstalled usa a existência do arquivo da unit como critério.
func (s *SystemdInstaller) IsInstalled(name string) (bool, error) {
	path, err := s.unitPath(name)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *SystemdInstaller) home() (string, error) {
	if s.homeDir != "" {
		return s.homeDir, nil
	}
	return os.UserHomeDir()
}

func (s *SystemdInstaller) run(name string, args ...string) error {
	if s.runner != nil {
		return s.runner(name, args...)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
