// Package update implementa o self-update do git-sync a partir dos releases
// do GitHub: `update check` consulta o último release (com cache de 12h) e
// `update apply` baixa o binário da plataforma, confere o sha256 publicado
// pelo GitHub, transplanta a chave SSH embutida e troca o binário em disco.
//
// O orquestrador (Updater) recebe o mundo externo por funções injetadas,
// para ser testado sem rede nem disco; Default() monta a versão real.
package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CheckInterval é por quanto tempo a resposta de um check vale sem consultar
// o GitHub de novo (salvo com --force).
const CheckInterval = 12 * time.Hour

// Release é o último release publicado.
type Release struct {
	Version string // sem o "v" da tag
	Assets  []Asset
}

// Asset é um arquivo anexado ao release.
type Asset struct {
	Name   string
	URL    string // URL da API do asset (funciona em repositório privado)
	Digest string // "sha256:<hex>", como a API do GitHub publica
}

// State é o cache do último check, persistido entre execuções.
type State struct {
	LastCheck time.Time `json:"lastCheck"`
	Target    string    `json:"target,omitempty"` // versão pendente; vazio = em dia
}

// ErrDevBuild impede que um binário compilado localmente (versão "dev") se
// sobrescreva com um release.
var ErrDevBuild = errors.New("este binário é um build de desenvolvimento (sem versão); o update só roda em binários de release")

// Updater orquestra check e apply.
type Updater struct {
	Current   string // versão em execução (main.version)
	AssetName string // nome do asset desta plataforma

	Latest       func() (Release, error)
	Now          func() time.Time
	ReadState    func() (State, error)
	WriteState   func(State) error
	Download     func(a Asset) ([]byte, error)
	Prepare      func(newBin []byte) ([]byte, error) // ex.: transplantar a chave SSH
	Swap         func(newBin []byte) error
	CleanupStale func()
}

// CheckResult diz se há versão nova.
type CheckResult struct {
	Pending bool
	Current string
	Target  string
}

// Check consulta o último release, usando o cache se ele tiver menos de
// CheckInterval e force for falso.
func (u *Updater) Check(force bool) (CheckResult, error) {
	state, err := u.ReadState()
	if err != nil {
		return CheckResult{}, err
	}
	if !force && !state.LastCheck.IsZero() && u.Now().Sub(state.LastCheck) < CheckInterval {
		return u.result(state.Target), nil
	}
	r, err := u.Latest()
	if err != nil {
		return CheckResult{}, err
	}
	target := ""
	if u.newer(r.Version) {
		if _, err := selectAsset(r, u.AssetName); err != nil {
			return CheckResult{}, err
		}
		target = r.Version
	}
	if err := u.WriteState(State{LastCheck: u.Now(), Target: target}); err != nil {
		return CheckResult{}, err
	}
	return u.result(target), nil
}

func (u *Updater) result(target string) CheckResult {
	// Um cache antigo pode apontar para uma versão que este binário já
	// alcançou (ex.: atualizado à mão); nesse caso não há nada pendente.
	if target == "" || !u.newer(target) {
		return CheckResult{Current: u.Current}
	}
	return CheckResult{Pending: true, Current: u.Current, Target: target}
}

// newer diz se v é mais nova que a versão em execução. Um build "dev" não
// tem versão comparável e trata qualquer release como mais novo.
func (u *Updater) newer(v string) bool {
	cur, ok := parseSemver(u.Current)
	if !ok {
		return true
	}
	next, ok := parseSemver(v)
	return ok && compare(next, cur) > 0
}

// ApplyResult é o desfecho de Apply.
type ApplyResult int

const (
	UpToDate ApplyResult = iota
	Updated
)

// Apply baixa e instala o último release, se for mais novo. Sempre consulta
// o GitHub (rodar `update apply` já é a decisão de atualizar).
func (u *Updater) Apply() (ApplyResult, string, error) {
	if _, ok := parseSemver(u.Current); !ok {
		return 0, "", ErrDevBuild
	}
	if u.CleanupStale != nil {
		u.CleanupStale()
	}
	r, err := u.Latest()
	if err != nil {
		return 0, "", err
	}
	if !u.newer(r.Version) {
		return UpToDate, "", u.WriteState(State{LastCheck: u.Now()})
	}
	a, err := selectAsset(r, u.AssetName)
	if err != nil {
		return 0, "", err
	}
	bin, err := u.Download(a)
	if err != nil {
		return 0, "", err
	}
	if err := verifyDigest(a, bin); err != nil {
		return 0, "", err
	}
	if u.Prepare != nil {
		if bin, err = u.Prepare(bin); err != nil {
			return 0, "", err
		}
	}
	if err := u.Swap(bin); err != nil {
		return 0, "", err
	}
	// O binário já foi trocado; uma falha ao gravar o cache não desfaz isso.
	_ = u.WriteState(State{LastCheck: u.Now()})
	return Updated, r.Version, nil
}

func selectAsset(r Release, name string) (Asset, error) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("o release %s não tem o binário %s para esta plataforma", r.Version, name)
}

// verifyDigest confere o sha256 que o GitHub calcula e publica para cada
// asset. Sem digest, recusa: o binário seria instalado sem nenhuma checagem.
func verifyDigest(a Asset, bin []byte) error {
	want, ok := strings.CutPrefix(a.Digest, "sha256:")
	if !ok {
		return fmt.Errorf("o asset %s não tem digest sha256 publicado; update recusado", a.Name)
	}
	sum := sha256.Sum256(bin)
	wantBytes, err := hex.DecodeString(want)
	if err != nil || !bytes.Equal(sum[:], wantBytes) {
		return fmt.Errorf("sha256 do download não confere com o publicado para %s; update recusado", a.Name)
	}
	return nil
}
