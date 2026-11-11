package update

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGitHub exercita o cliente HTTP real contra um GitHub falso: lê o
// release, baixa o asset pela URL da API (que redireciona para o storage)
// e confere que o token não vaza para o host do redirect.
func TestGitHub(t *testing.T) {
	payload := []byte("binário do release")
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("token vazou para o host do redirect")
		}
		w.Write(payload)
	}))
	defer storage.Close()

	var api *httptest.Server
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer segredo" {
			http.Error(w, "sem token", http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/repos/dono/git-sync/releases/latest":
			w.Write([]byte(`{"tag_name":"v0.2.0","assets":[{"name":"` + asset + `","url":"` + api.URL + `/asset/1","digest":"` + digestOf(payload) + `"}]}`))
		case "/asset/1":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Error("download sem Accept: application/octet-stream")
			}
			http.Redirect(w, r, storage.URL+"/blob", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	gh := github{api: api.URL, repo: "dono/git-sync", token: "segredo", client: newClient()}
	r, err := gh.latest()
	if err != nil || r.Version != "0.2.0" || len(r.Assets) != 1 {
		t.Fatalf("latest = %+v, %v", r, err)
	}
	bin, err := gh.download(r.Assets[0])
	if err != nil || string(bin) != string(payload) {
		t.Fatalf("download = %q, %v", bin, err)
	}
	if err := verifyDigest(r.Assets[0], bin); err != nil {
		t.Fatal(err)
	}

	anon := github{api: api.URL, repo: "dono/git-sync", client: newClient()}
	if _, err := anon.latest(); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("sem token: err=%v, quer dica de GITHUB_TOKEN", err)
	}
}
