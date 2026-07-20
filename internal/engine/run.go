package engine

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Run executa o daemon de sincronização: observa mudanças em watchDir,
// aplica debounce e dispara SyncCycle, além de rodar PullCycle
// periodicamente. Bloqueia até o contexto ser cancelado.
//
// debounce e pullInterval são parametrizados para facilitar testes; em
// produção use Debounce e PullInterval.
func (e *Engine) Run(ctx context.Context, watchDir string, debounce, pullInterval time.Duration) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := addRecursive(watcher, watchDir); err != nil {
		return err
	}

	// Timer de debounce começa parado; só arma quando chega um evento.
	debounceTimer := time.NewTimer(debounce)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	debouncePending := false

	pullTicker := time.NewTicker(pullInterval)
	defer pullTicker.Stop()

	e.logf("git-sync iniciado; observando %s (debounce %s, pull %s)", watchDir, debounce, pullInterval)

	for {
		select {
		case <-ctx.Done():
			e.logf("encerrando git-sync")
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if isIgnored(watchDir, event.Name) {
				continue
			}
			// Passa a observar diretórios recém-criados.
			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					_ = addRecursive(watcher, event.Name)
				}
			}
			// (Re)arma o debounce a cada mudança relevante.
			if debouncePending && !debounceTimer.Stop() {
				select {
				case <-debounceTimer.C:
				default:
				}
			}
			debounceTimer.Reset(debounce)
			debouncePending = true

		case <-debounceTimer.C:
			debouncePending = false
			if err := e.SyncCycle(); err != nil {
				e.logf("ciclo de sync falhou: %v", err)
			}

		case <-pullTicker.C:
			if err := e.PullCycle(); err != nil {
				e.logf("pull periódico falhou: %v", err)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			e.logf("erro do watcher: %v", err)
		}
	}
}

// addRecursive registra dir e todos os subdiretórios no watcher,
// ignorando o diretório .git.
func addRecursive(watcher *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // ignora entradas ilegíveis em vez de abortar
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

// isIgnored decide se um caminho de evento deve ser ignorado. Mudanças
// dentro de .git não representam edições do usuário.
func isIgnored(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".git" || strings.HasPrefix(rel, ".git"+string(os.PathSeparator)) {
		return true
	}
	return false
}
