package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/racoonlemore/git-sync/internal/engine"
)

// TestEnsureLogFile verifica que o log local é criado e registrado no
// .gitignore, sem duplicar a linha e sem colar em conteúdo pré-existente.
func TestEnsureLogFile(t *testing.T) {
	countLine := func(dir string) int {
		data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
		n := 0
		for _, l := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(l) == engine.LogFileName {
				n++
			}
		}
		return n
	}

	t.Run("cria arquivo e gitignore do zero", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := ensureLogFile(dir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, engine.LogFileName)); err != nil {
			t.Errorf("log não criado: %v", err)
		}
		if got := countLine(dir); got != 1 {
			t.Errorf("linha no .gitignore = %d, quer 1", got)
		}
	})

	t.Run("idempotente: chamadas repetidas não duplicam", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < 3; i++ {
			if _, err := ensureLogFile(dir); err != nil {
				t.Fatal(err)
			}
		}
		if got := countLine(dir); got != 1 {
			t.Errorf("linha no .gitignore = %d, quer 1 (sem duplicar)", got)
		}
	})

	t.Run("anexa sem colar em .gitignore sem newline final", func(t *testing.T) {
		dir := t.TempDir()
		giPath := filepath.Join(dir, ".gitignore")
		if err := os.WriteFile(giPath, []byte("*.tmp"), 0o644); err != nil { // sem \n final
			t.Fatal(err)
		}
		if _, err := ensureLogFile(dir); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(giPath)
		if want := "*.tmp\n" + engine.LogFileName + "\n"; string(data) != want {
			t.Errorf(".gitignore = %q, quer %q", data, want)
		}
	})
}
