package cli

import (
	"fmt"

	"github.com/racoonlemore/git-sync/internal/sshkey"
)

// RemoveKey implementa o comando `remove-key`: zera o slot da chave SSH
// embutida no binário, voltando ao comportamento padrão do git.
func RemoveKey() error {
	if err := sshkey.Clear(); err != nil {
		return err
	}
	fmt.Println("Chave SSH removida.")
	fmt.Println("Reinicie o serviço git-sync para que a remoção entre em vigor.")
	return nil
}
