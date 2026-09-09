package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/racoonlemore/git-sync/internal/sshkey"
	"golang.org/x/crypto/ssh"
)

// AddKey implementa o comando `add-key`: lê a chave privada em keyPath,
// valida (sem passphrase, tamanho) e patcha o próprio binário.
func AddKey(keyPath string) error {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("não foi possível ler a chave: %w", err)
	}

	_, err = ssh.ParseRawPrivateKey(data)
	if err != nil {
		var passphraseErr *ssh.PassphraseMissingError
		if errors.As(err, &passphraseErr) {
			return fmt.Errorf("chaves protegidas por passphrase não são suportadas")
		}
		return fmt.Errorf("chave inválida: %w", err)
	}

	if err := sshkey.Patch(data); err != nil {
		return err
	}

	fmt.Println("Chave SSH configurada.")
	fmt.Println("Reinicie o serviço git-sync para que a chave entre em vigor.")
	return nil
}
