package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// isolatedGit aponta as configs global/system do git para arquivos vazios
// controlados pelo teste, para que UserName não dependa da máquina.
func isolatedGit(t *testing.T) (globalPath string) {
	t.Helper()
	globalPath = filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(globalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalPath)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	return globalPath
}

func TestUserNameFallbackLocalDepoisGlobal(t *testing.T) {
	global := isolatedGit(t)
	dir := t.TempDir()
	run(t, dir, "init", "-b", "main")
	repo := Open(dir)

	// Sem nenhum user.name configurado: erro.
	if _, err := repo.UserName(); err == nil {
		t.Fatal("esperava erro quando user.name não está configurado")
	}

	// Só global configurado: usa o global.
	run(t, dir, "config", "--file", global, "user.name", "Usuário Global")
	if name, err := repo.UserName(); err != nil || name != "Usuário Global" {
		t.Fatalf("UserName com só global = %q, %v; quer 'Usuário Global'", name, err)
	}

	// Local configurado: precede o global.
	run(t, dir, "config", "user.name", "Usuário Local")
	if name, err := repo.UserName(); err != nil || name != "Usuário Local" {
		t.Fatalf("UserName com local = %q, %v; quer 'Usuário Local'", name, err)
	}
}

func TestIsRepoEHasRemote(t *testing.T) {
	isolatedGit(t)
	dir := t.TempDir()

	// Diretório sem git.
	if Open(dir).IsRepo() {
		t.Error("diretório vazio não deveria ser repositório")
	}

	run(t, dir, "init", "-b", "main")
	repo := Open(dir)
	if !repo.IsRepo() {
		t.Error("esperava IsRepo=true após git init")
	}

	if has, err := repo.HasRemote(); err != nil || has {
		t.Errorf("HasRemote sem remote = %v, %v; quer false", has, err)
	}
	run(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	if has, err := repo.HasRemote(); err != nil || !has {
		t.Errorf("HasRemote com remote = %v, %v; quer true", has, err)
	}
}

// TestTrimMantemUltimosNEPreservaHead verifica diretamente o trim: mantém
// os N commits mais recentes e preserva exatamente a árvore do HEAD.
func TestTrimMantemUltimosNEPreservaHead(t *testing.T) {
	isolatedGit(t)
	dir := t.TempDir()
	run(t, dir, "init", "-b", "main")
	run(t, dir, "config", "user.name", "Tester")
	run(t, dir, "config", "user.email", "t@example.com")

	for i := 0; i < 6; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, dir, "add", "-A")
		run(t, dir, "commit", "-m", "c")
	}

	repo := Open(dir)
	headTreeBefore := revParse(t, dir, "HEAD^{tree}")

	if err := repo.Trim("main", 3); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	if n, err := repo.CommitCount(); err != nil || n != 3 {
		t.Fatalf("CommitCount após trim = %d, %v; quer 3", n, err)
	}
	if headTreeAfter := revParse(t, dir, "HEAD^{tree}"); headTreeAfter != headTreeBefore {
		t.Errorf("árvore do HEAD mudou no trim: %s -> %s", headTreeBefore, headTreeAfter)
	}
	// A nova raiz não tem pais.
	if parents := revList(t, dir, "--max-parents=0", "HEAD"); len(parents) != 1 {
		t.Errorf("esperava exatamente uma raiz após trim, achei %d", len(parents))
	}
}

func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", rev)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse %s: %v\n%s", rev, err, out)
	}
	return strings.TrimSpace(string(out))
}

func revList(t *testing.T, dir string, args ...string) []string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"rev-list"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-list: %v\n%s", err, out)
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}
