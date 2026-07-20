package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LaunchdInstaller registra o git-sync como LaunchAgent do usuário no
// macOS (nunca LaunchDaemon), em ~/Library/LaunchAgents, com RunAtLoad.
type LaunchdInstaller struct {
	homeDir string
	runner  func(name string, args ...string) error
}

func (l *LaunchdInstaller) Kind() string { return "LaunchAgent (launchd)" }

func (l *LaunchdInstaller) plistFileName(name string) string {
	return name + ".plist"
}

func (l *LaunchdInstaller) plistPath(name string) (string, error) {
	home, err := l.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", l.plistFileName(name)), nil
}

// plistContent gera o conteúdo do plist do LaunchAgent (função pura).
func (l *LaunchdInstaller) plistContent(cfg Config) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	fmt.Fprintf(&b, "    <key>Label</key>\n    <string>%s</string>\n", cfg.Name)
	b.WriteString("    <key>ProgramArguments</key>\n    <array>\n")
	for _, arg := range []string{cfg.BinPath, "run", "--repo", cfg.RepoPath} {
		fmt.Fprintf(&b, "        <string>%s</string>\n", arg)
	}
	b.WriteString("    </array>\n")
	fmt.Fprintf(&b, "    <key>WorkingDirectory</key>\n    <string>%s</string>\n", cfg.RepoPath)
	b.WriteString("    <key>RunAtLoad</key>\n    <true/>\n")
	b.WriteString("    <key>KeepAlive</key>\n    <true/>\n")
	b.WriteString("</dict>\n")
	b.WriteString("</plist>\n")
	return b.String()
}

func (l *LaunchdInstaller) Install(cfg Config) error {
	path, err := l.plistPath(cfg.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(l.plistContent(cfg)), 0o644); err != nil {
		return err
	}
	// bootstrap na sessão gui do usuário atual.
	return l.run("launchctl", "load", "-w", path)
}

func (l *LaunchdInstaller) Uninstall(name string) error {
	path, err := l.plistPath(name)
	if err != nil {
		return err
	}
	_ = l.run("launchctl", "unload", "-w", path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (l *LaunchdInstaller) IsInstalled(name string) (bool, error) {
	path, err := l.plistPath(name)
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

func (l *LaunchdInstaller) home() (string, error) {
	if l.homeDir != "" {
		return l.homeDir, nil
	}
	return os.UserHomeDir()
}

func (l *LaunchdInstaller) run(name string, args ...string) error {
	if l.runner != nil {
		return l.runner(name, args...)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
