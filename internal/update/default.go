package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/racoonlemore/git-sync/internal/sshkey"
)

// DefaultRepo é o repositório de onde saem os releases. GIT_SYNC_REPO
// sobrescreve (útil para forks e testes).
const DefaultRepo = "racoonlemore/git-sync"

// maxBinarySize limita o download para um asset inesperadamente grande não
// esgotar a memória.
const maxBinarySize = 256 << 20

// AssetName é o nome do binário desta plataforma no release, igual ao que o
// workflow de release publica: git-sync-<goos>-<goarch>[.exe].
func AssetName(goos, goarch string) string {
	name := "git-sync-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Default monta o Updater real: API do GitHub, cache em
// <UserCacheDir>/git-sync/update.json e troca do binário em execução.
// GITHUB_TOKEN (ou GH_TOKEN) autentica as chamadas, necessário se o
// repositório for privado.
func Default(current string) *Updater {
	repo := DefaultRepo
	if r := os.Getenv("GIT_SYNC_REPO"); r != "" {
		repo = r
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	gh := github{api: "https://api.github.com", repo: repo, token: token, client: newClient()}
	statePath := stateFile()

	return &Updater{
		Current:      current,
		AssetName:    AssetName(runtime.GOOS, runtime.GOARCH),
		Latest:       gh.latest,
		Now:          time.Now,
		ReadState:    func() (State, error) { return readState(statePath) },
		WriteState:   func(s State) error { return writeState(statePath, s) },
		Download:     gh.download,
		Prepare:      sshkey.Transplant,
		Swap:         swapExecutable,
		CleanupStale: cleanupStale,
	}
}

// ── GitHub ───────────────────────────────────────────────────────────────────

type github struct {
	api, repo, token string
	client           *http.Client
}

// newClient remove o Authorization quando um redirect troca de host (porta
// incluída), sem depender da regra do net/http, que ignora a porta.
func newClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("redirects demais")
			}
			if req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

func (g github) get(url, accept string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "git-sync-updater")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		// O token só vai para a API; newClient o remove em redirects para
		// outro host (o download do asset redireciona para o storage).
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	res, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("não foi possível acessar o GitHub: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		res.Body.Close()
		if res.StatusCode == http.StatusNotFound && g.token == "" {
			return nil, fmt.Errorf("GitHub respondeu 404 para %s; se o repositório for privado, defina GITHUB_TOKEN", g.repo)
		}
		return nil, fmt.Errorf("GitHub respondeu %d para %s", res.StatusCode, url)
	}
	return res, nil
}

func (g github) latest() (Release, error) {
	res, err := g.get(g.api+"/repos/"+g.repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name   string `json:"name"`
			URL    string `json:"url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("resposta inválida do GitHub: %w", err)
	}
	if body.TagName == "" {
		return Release{}, errors.New("resposta do GitHub sem tag_name")
	}
	r := Release{Version: strings.TrimPrefix(body.TagName, "v")}
	for _, a := range body.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Digest: a.Digest})
	}
	return r, nil
}

func (g github) download(a Asset) ([]byte, error) {
	res, err := g.get(a.URL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	bin, err := io.ReadAll(io.LimitReader(res.Body, maxBinarySize+1))
	if err != nil {
		return nil, fmt.Errorf("download de %s falhou: %w", a.Name, err)
	}
	if len(bin) > maxBinarySize {
		return nil, fmt.Errorf("download de %s passou de %d MiB; abortado", a.Name, maxBinarySize>>20)
	}
	return bin, nil
}

// ── cache do check ───────────────────────────────────────────────────────────

func stateFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "git-sync", "update.json")
}

func readState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if json.Unmarshal(data, &s) != nil {
		return State{}, nil // cache corrompido: trata como ausente
	}
	return s, nil
}

func writeState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ── troca do binário ─────────────────────────────────────────────────────────

func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func swapExecutable(bin []byte) error {
	target, err := executablePath()
	if err != nil {
		return err
	}
	return swap(target, bin, runtime.GOOS == "windows")
}

// swap grava bin ao lado de target e o renomeia por cima. No Windows um .exe
// em execução não pode ser sobrescrito, mas pode ser renomeado: ele vai para
// target.old primeiro, e o .old é apagado quando nenhum processo o usa mais.
func swap(target string, bin []byte, windows bool) error {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp := target + ".new"
	if err := os.WriteFile(tmp, bin, info.Mode().Perm()); err != nil {
		return fmt.Errorf("não foi possível gravar %s (a pasta do binário precisa ser gravável): %w", tmp, err)
	}
	if err := os.Chmod(tmp, info.Mode().Perm()); err != nil {
		os.Remove(tmp)
		return err
	}
	if windows {
		old := target + ".old"
		os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Rename(old, target) // devolve o binário antigo
			os.Remove(tmp)
			return err
		}
		os.Remove(old) // falha enquanto um daemon ainda roda o binário antigo
		return nil
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func cleanupStale() {
	if runtime.GOOS != "windows" {
		return
	}
	if exe, err := executablePath(); err == nil {
		os.Remove(exe + ".old")
	}
}
