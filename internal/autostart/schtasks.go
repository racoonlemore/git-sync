package autostart

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

// SchtasksInstaller registra o git-sync como tarefa do Task Scheduler do
// Windows com gatilho "at log on", rodando com a conta do usuário (não
// como SYSTEM).
type SchtasksInstaller struct {
	runner func(args ...string) (stdout string, exitCode int, err error)
}

func (w *SchtasksInstaller) Kind() string { return "tarefa do Task Scheduler (at log on)" }

// createArgs monta os argumentos do `schtasks /Create` (função pura).
// /SC ONLOGON = gatilho no login; /RL LIMITED = executa com privilégios
// do usuário (não elevado/SYSTEM); /F sobrescreve se já existir.
func (w *SchtasksInstaller) createArgs(cfg Config) []string {
	tr := fmt.Sprintf(`"%s" run --repo "%s"`, cfg.BinPath, cfg.RepoPath)
	return []string{
		"/Create",
		"/TN", cfg.Name,
		"/TR", tr,
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F",
	}
}

func (w *SchtasksInstaller) deleteArgs(name string) []string {
	return []string{"/Delete", "/TN", name, "/F"}
}

func (w *SchtasksInstaller) queryArgs(name string) []string {
	return []string{"/Query", "/TN", name}
}

func (w *SchtasksInstaller) Install(cfg Config) error {
	_, _, err := w.run(w.createArgs(cfg)...)
	return err
}

func (w *SchtasksInstaller) Uninstall(name string) error {
	_, code, err := w.run(w.deleteArgs(name)...)
	if err != nil && code != 0 {
		// Tarefa inexistente não é falha de uninstall.
		return nil
	}
	return err
}

func (w *SchtasksInstaller) IsInstalled(name string) (bool, error) {
	_, code, err := w.run(w.queryArgs(name)...)
	if err == nil {
		return true, nil
	}
	// schtasks devolve código != 0 quando a tarefa não existe.
	if code != 0 {
		return false, nil
	}
	return false, err
}

func (w *SchtasksInstaller) run(args ...string) (string, int, error) {
	if w.runner != nil {
		return w.runner(args...)
	}
	cmd := exec.Command("schtasks", args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	if err != nil {
		return out.String(), code, fmt.Errorf("schtasks %v: %v: %s", args, err, errBuf.String())
	}
	return out.String(), code, nil
}
