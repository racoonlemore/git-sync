package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const asset = "git-sync-linux-amd64"

// fakeWorld é o mundo externo do Updater: GitHub, relógio, cache e disco.
type fakeWorld struct {
	release     Release
	latestErr   error
	downloadErr error
	payload     []byte
	state       State
	now         time.Time
	fetches     int
	swapped     []byte
	prepared    bool
	swept       bool
}

func digestOf(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

func newWorld(version string) *fakeWorld {
	payload := []byte("binário novo")
	return &fakeWorld{
		payload: payload,
		now:     time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC),
		release: Release{Version: version, Assets: []Asset{
			{Name: "git-sync-darwin-arm64", URL: "u-darwin", Digest: digestOf([]byte("outro"))},
			{Name: asset, URL: "u-linux", Digest: digestOf(payload)},
		}},
	}
}

func (w *fakeWorld) updater(current string) *Updater {
	return &Updater{
		Current:   current,
		AssetName: asset,
		Latest: func() (Release, error) {
			w.fetches++
			return w.release, w.latestErr
		},
		Now:        func() time.Time { return w.now },
		ReadState:  func() (State, error) { return w.state, nil },
		WriteState: func(s State) error { w.state = s; return nil },
		Download: func(a Asset) ([]byte, error) {
			if a.URL != "u-linux" {
				return nil, errors.New("asset errado: " + a.URL)
			}
			return w.payload, w.downloadErr
		},
		Prepare: func(b []byte) ([]byte, error) {
			w.prepared = true
			return append([]byte("chave+"), b...), nil
		},
		Swap:         func(b []byte) error { w.swapped = b; return nil },
		CleanupStale: func() { w.swept = true },
	}
}

func TestCheck(t *testing.T) {
	w := newWorld("0.1.0")
	r, err := w.updater("0.0.3").Check(false)
	if err != nil || !r.Pending || r.Current != "0.0.3" || r.Target != "0.1.0" {
		t.Fatalf("Check = %+v, %v", r, err)
	}

	// Dentro de 12h, responde do cache sem consultar o GitHub.
	w.now = w.now.Add(11 * time.Hour)
	if r, _ := w.updater("0.0.3").Check(false); !r.Pending || w.fetches != 1 {
		t.Fatalf("esperava resposta do cache; pending=%v fetches=%d", r.Pending, w.fetches)
	}
	// --force ignora o cache.
	if _, err := w.updater("0.0.3").Check(true); err != nil || w.fetches != 2 {
		t.Fatalf("force não consultou: fetches=%d err=%v", w.fetches, err)
	}
	// Cache vencido consulta de novo.
	w.now = w.now.Add(13 * time.Hour)
	if _, _ = w.updater("0.0.3").Check(false); w.fetches != 3 {
		t.Fatalf("cache vencido não consultou: fetches=%d", w.fetches)
	}
	// Cache apontando para versão já alcançada não é pendência.
	if r, _ := w.updater("0.1.0").Check(false); r.Pending {
		t.Fatal("binário já na versão alvo não deveria ter pendência")
	}
}

func TestCheckEmDia(t *testing.T) {
	for _, current := range []string{"0.1.0", "v0.1.0", "0.2.0"} {
		w := newWorld("0.1.0")
		if r, err := w.updater(current).Check(false); err != nil || r.Pending {
			t.Errorf("current=%s: %+v, %v", current, r, err)
		}
		if w.state.LastCheck.IsZero() || w.state.Target != "" {
			t.Errorf("current=%s: cache não gravado corretamente: %+v", current, w.state)
		}
	}
}

func TestCheckSemAssetDaPlataforma(t *testing.T) {
	w := newWorld("0.1.0")
	w.release.Assets = w.release.Assets[:1]
	if _, err := w.updater("0.0.3").Check(false); err == nil {
		t.Fatal("esperava erro sem asset desta plataforma")
	}
}

func TestApply(t *testing.T) {
	w := newWorld("0.1.0")
	w.state = State{LastCheck: w.now, Target: "0.1.0"}
	res, target, err := w.updater("0.0.3").Apply()
	if err != nil || res != Updated || target != "0.1.0" {
		t.Fatalf("Apply = %v, %q, %v", res, target, err)
	}
	if !w.swept || !w.prepared || string(w.swapped) != "chave+binário novo" {
		t.Fatalf("swept=%v prepared=%v swapped=%q", w.swept, w.prepared, w.swapped)
	}
	if w.state.Target != "" {
		t.Fatalf("pendência não foi limpa: %+v", w.state)
	}
}

func TestApplyEmDia(t *testing.T) {
	w := newWorld("0.0.3")
	if res, _, err := w.updater("0.0.3").Apply(); err != nil || res != UpToDate || w.swapped != nil {
		t.Fatalf("res=%v err=%v swapped=%q", res, err, w.swapped)
	}
}

func TestApplyRecusa(t *testing.T) {
	cases := map[string]func(*fakeWorld) string{
		"build dev":        func(w *fakeWorld) string { return "dev" },
		"download falha":   func(w *fakeWorld) string { w.downloadErr = errors.New("boom"); return "0.0.3" },
		"github falha":     func(w *fakeWorld) string { w.latestErr = errors.New("offline"); return "0.0.3" },
		"sha256 diferente": func(w *fakeWorld) string { w.payload = []byte("adulterado"); return "0.0.3" },
		"sem digest": func(w *fakeWorld) string {
			w.release.Assets[1].Digest = ""
			return "0.0.3"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld("0.1.0")
			current := setup(w)
			if _, _, err := w.updater(current).Apply(); err == nil {
				t.Fatal("esperava erro")
			}
			if w.swapped != nil {
				t.Fatal("binário foi trocado apesar do erro")
			}
		})
	}
	if _, _, err := newWorld("0.1.0").updater("dev").Apply(); !errors.Is(err, ErrDevBuild) {
		t.Fatalf("dev: err=%v, quer ErrDevBuild", err)
	}
}

func TestSemver(t *testing.T) {
	order := []string{"0.0.3", "0.1.0-rc.1", "0.1.0-rc.2", "0.1.0-rc.10", "0.1.0", "v0.1.1", "1.0.0"}
	for i := 1; i < len(order); i++ {
		a, _ := parseSemver(order[i-1])
		b, ok := parseSemver(order[i])
		if !ok || compare(a, b) >= 0 || compare(b, a) <= 0 {
			t.Errorf("%s deveria vir antes de %s", order[i-1], order[i])
		}
	}
	for _, bad := range []string{"dev", "", "1.2", "1.2.x", "v"} {
		if _, ok := parseSemver(bad); ok {
			t.Errorf("parseSemver(%q) deveria falhar", bad)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("windows", "amd64"); got != "git-sync-windows-amd64.exe" {
		t.Errorf("windows: %s", got)
	}
	if got := AssetName("darwin", "arm64"); got != "git-sync-darwin-arm64" {
		t.Errorf("darwin: %s", got)
	}
}

func TestSwap(t *testing.T) {
	for _, windows := range []bool{false, true} {
		dir := t.TempDir()
		target := filepath.Join(dir, "git-sync")
		if err := os.WriteFile(target, []byte("velho"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := swap(target, []byte("novo"), windows); err != nil {
			t.Fatalf("windows=%v: %v", windows, err)
		}
		data, _ := os.ReadFile(target)
		info, _ := os.Stat(target)
		if string(data) != "novo" || info.Mode().Perm() != 0o755 {
			t.Errorf("windows=%v: conteúdo=%q modo=%v", windows, data, info.Mode())
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Errorf("windows=%v: sobrou lixo na pasta: %v", windows, entries)
		}
	}
}

func TestState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "update.json")
	if s, err := readState(path); err != nil || !s.LastCheck.IsZero() {
		t.Fatalf("cache ausente: %+v %v", s, err)
	}
	want := State{LastCheck: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC), Target: "0.1.0"}
	if err := writeState(path, want); err != nil {
		t.Fatal(err)
	}
	if got, err := readState(path); err != nil || !got.LastCheck.Equal(want.LastCheck) || got.Target != want.Target {
		t.Fatalf("got %+v %v", got, err)
	}
	os.WriteFile(path, []byte("{lixo"), 0o644)
	if s, err := readState(path); err != nil || !s.LastCheck.IsZero() {
		t.Fatalf("cache corrompido deveria ser ignorado: %+v %v", s, err)
	}
}
