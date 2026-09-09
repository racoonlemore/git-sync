package sshkey

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// fakeBinary constrói um blob que imita um binário com o placeholder embutido.
func fakeBinary(slotContent []byte) []byte {
	marker := keyMarker()
	var b []byte
	b = append(b, []byte("ELF_FAKE_HEADER")...)
	b = append(b, marker...)
	slot := make([]byte, MaxKeySize+1)
	copy(slot, slotContent)
	b = append(b, slot...)
	b = append(b, []byte("TAIL")...)
	return b
}

func TestFindOffset(t *testing.T) {
	blob := fakeBinary(nil)
	off, err := findOffset(blob)
	if err != nil {
		t.Fatalf("findOffset: %v", err)
	}
	want := bytes.Index(blob, keyMarker()) + markerLen
	if off != want {
		t.Fatalf("offset=%d, want %d", off, want)
	}
}

func TestFindOffsetMissing(t *testing.T) {
	_, err := findOffset([]byte("no marker here"))
	if err == nil {
		t.Fatal("esperava erro quando marker ausente")
	}
}

// TestPatchClearFile verifica que Patch escreve o PEM no arquivo em disco e
// Clear o zera — sem depender da variável em memória (que é fixa em testes).
func TestPatchClearFile(t *testing.T) {
	const fakePEM = "-----BEGIN OPENSSH PRIVATE KEY-----\nABCD\n-----END OPENSSH PRIVATE KEY-----\n"

	dir := t.TempDir()
	binPath := filepath.Join(dir, "fake-bin")
	if err := os.WriteFile(binPath, fakeBinary(nil), 0o755); err != nil {
		t.Fatal(err)
	}

	origExec := execPath
	execPath = func() (string, error) { return binPath, nil }
	defer func() { execPath = origExec }()

	// Patch deve escrever o PEM no arquivo.
	if err := Patch([]byte(fakePEM)); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	data, _ := os.ReadFile(binPath)
	off, _ := findOffset(data)
	payload := data[off : off+len(fakePEM)]
	if string(payload) != fakePEM {
		t.Fatalf("arquivo após Patch: got %q, want %q", payload, fakePEM)
	}

	// Clear deve zerar o slot.
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	data, _ = os.ReadFile(binPath)
	off, _ = findOffset(data)
	slot := data[off : off+MaxKeySize+1]
	if !bytes.Equal(slot, make([]byte, MaxKeySize+1)) {
		t.Fatal("slot não foi zerado após Clear")
	}
}

func TestReadEmpty(t *testing.T) {
	// embeddedKey está zerado (além do marker) no binário de teste → nil.
	got, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != nil {
		t.Fatalf("Read com slot vazio: esperava nil, got %q", got)
	}
}

func TestPatchTooBig(t *testing.T) {
	big := make([]byte, MaxKeySize+1)
	if err := Patch(big); err == nil {
		t.Fatal("esperava erro para chave maior que MaxKeySize")
	}
}
