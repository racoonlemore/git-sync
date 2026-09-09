// Package sshkey gerencia a chave SSH privada embutida no próprio binário.
// O self-patch localiza um placeholder de 4096 bytes (com magic marker) no
// binário em disco e sobrescreve os bytes da chave PEM. Nenhuma chave toca
// arquivos de configuração externos — ela vive apenas no binário.
package sshkey

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// embeddedKey é o placeholder de 4096 bytes que add-key patcha em disco.
// Primeiros 10 bytes: magic marker. Restante: PEM null-terminado.
var embeddedKey = [4096]byte{
	0x00, 'G', 'S', 'Y', 'N', 'C', 'K', 'E', 'Y', 0x00,
}

const (
	markerLen  = 10
	// MaxKeySize é o tamanho máximo da chave PEM (sem null terminator).
	MaxKeySize = 4096 - markerLen - 1
)

// keyMarker constrói o magic marker em runtime para que a sequência exata
// não apareça como literal em .rodata e crie falso positivo na busca.
func keyMarker() []byte {
	b := make([]byte, markerLen)
	b[0] = 0x00
	b[1], b[2], b[3], b[4], b[5] = 'G', 'S', 'Y', 'N', 'C'
	b[6], b[7], b[8] = 'K', 'E', 'Y'
	b[9] = 0x00
	return b
}

func findOffset(data []byte) (int, error) {
	idx := bytes.Index(data, keyMarker())
	if idx < 0 {
		return 0, fmt.Errorf("sshkey: marcador não encontrado no binário")
	}
	return idx + markerLen, nil
}

// execPath é a função que localiza o binário em disco; substituível em testes.
var execPath = func() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(self)
}

// Read devolve o PEM da chave embutida, ou nil se nenhuma estiver configurada.
// Lê diretamente da variável em memória — isso força o linker a manter o
// placeholder no binário e reflete o estado patchado em disco ao iniciar.
func Read() ([]byte, error) {
	payload := embeddedKey[markerLen:]
	n := bytes.IndexByte(payload[:], 0x00)
	if n <= 0 {
		return nil, nil // sem chave configurada
	}
	out := make([]byte, n)
	copy(out, payload[:n])
	return out, nil
}

// Patch grava keyPEM no placeholder do binário (atomic via rename).
func Patch(keyPEM []byte) error {
	if len(keyPEM) > MaxKeySize {
		return fmt.Errorf("sshkey: chave muito grande (%d bytes, máximo %d)", len(keyPEM), MaxKeySize)
	}
	return patchBinary(func(slot []byte) {
		copy(slot, make([]byte, len(slot))) // zera slot
		copy(slot, keyPEM)
	})
}

// Clear zera o slot da chave embutida (remove-key).
func Clear() error {
	return patchBinary(func(slot []byte) {
		copy(slot, make([]byte, len(slot)))
	})
}

// patchBinary lê o binário, aplica fn sobre o slot da chave e reescreve
// atomicamente via rename para evitar ETXTBSY num binário em execução.
func patchBinary(fn func(slot []byte)) error {
	self, err := execPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	off, err := findOffset(data)
	if err != nil {
		return err
	}
	fn(data[off : off+MaxKeySize+1])
	return writeAtomic(self, data)
}

func writeAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".git-sync-patch-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(info.Mode()); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
