package autostart

import (
	"path/filepath"
	"strings"
	"testing"
)

// Os testes do seam 2 verificam a geração determinística de nomes e do
// conteúdo/comandos das units — sem invocar systemctl/launchctl/schtasks.

func TestUnitNameDeterministicoEIsolado(t *testing.T) {
	a := UnitName("/home/user/docs")
	b := UnitName("/home/user/docs")
	if a != b {
		t.Errorf("UnitName não determinístico: %q != %q", a, b)
	}

	// Caminhos diferentes não colidem (repositórios distintos na mesma máquina).
	if c := UnitName("/home/user/fotos"); c == a {
		t.Errorf("caminhos diferentes geraram o mesmo nome: %q", c)
	}

	// Inclui o nome da pasta e um sufixo de hash.
	if !strings.HasPrefix(a, "git-sync-docs-") {
		t.Errorf("nome %q não segue o formato git-sync-<pasta>-<hash>", a)
	}
}

func TestUnitNameSanitizaCaracteres(t *testing.T) {
	name := UnitName("/tmp/my repo (v2)")
	if strings.ContainsAny(name, " ()") {
		t.Errorf("nome contém caracteres inseguros: %q", name)
	}
}

func TestSystemdUnitContent(t *testing.T) {
	s := &SystemdInstaller{homeDir: "/home/alice"}
	cfg := Config{Name: "git-sync-docs-abc123", RepoPath: "/home/alice/docs", BinPath: "/usr/local/bin/git-sync"}
	content := s.unitContent(cfg)

	mustContain(t, content, "[Service]")
	mustContain(t, content, "ExecStart=/usr/local/bin/git-sync run --repo /home/alice/docs")
	mustContain(t, content, "WorkingDirectory=/home/alice/docs")
	// Serviço de usuário: WantedBy=default.target (não multi-user/boot).
	mustContain(t, content, "WantedBy=default.target")
	if strings.Contains(content, "multi-user.target") {
		t.Error("unit não deve depender de multi-user.target (é serviço de usuário)")
	}

	path, err := s.unitPath(cfg.Name)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.FromSlash("/home/alice/.config/systemd/user/git-sync-docs-abc123.service")
	if path != want {
		t.Errorf("unitPath = %q, quer %q", path, want)
	}
}

func TestLaunchdPlistContent(t *testing.T) {
	l := &LaunchdInstaller{homeDir: "/Users/alice"}
	cfg := Config{Name: "git-sync-docs-abc123", RepoPath: "/Users/alice/docs", BinPath: "/usr/local/bin/git-sync"}
	content := l.plistContent(cfg)

	mustContain(t, content, "<key>Label</key>")
	mustContain(t, content, "<string>git-sync-docs-abc123</string>")
	mustContain(t, content, "<key>RunAtLoad</key>")
	mustContain(t, content, "<string>/usr/local/bin/git-sync</string>")
	mustContain(t, content, "<string>--repo</string>")
	mustContain(t, content, "<string>/Users/alice/docs</string>")

	path, err := l.plistPath(cfg.Name)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.FromSlash("/Users/alice/Library/LaunchAgents/git-sync-docs-abc123.plist")
	if path != want {
		t.Errorf("plistPath = %q, quer %q", path, want)
	}
}

func TestSchtasksCreateArgs(t *testing.T) {
	w := &SchtasksInstaller{}
	cfg := Config{Name: "git-sync-docs-abc123", RepoPath: `C:\Users\alice\docs`, BinPath: `C:\bin\git-sync.exe`}
	args := strings.Join(w.createArgs(cfg), " ")

	// Gatilho no login, com a conta do usuário (LIMITED, não SYSTEM).
	mustContain(t, args, "/SC ONLOGON")
	mustContain(t, args, "/RL LIMITED")
	mustContain(t, args, "/TN git-sync-docs-abc123")
	mustContain(t, args, `run --repo`)
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("esperava encontrar %q em:\n%s", needle, haystack)
	}
}
