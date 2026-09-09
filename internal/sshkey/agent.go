package sshkey

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// StartAgent sobe um SSH agent in-process carregado com keyPEM, ouve num
// socket Unix e devolve o caminho do socket e uma função de parada.
// A chave nunca é escrita em disco — só fica em memória enquanto o processo
// está rodando.
func StartAgent(keyPEM []byte) (socketPath string, stop func(), err error) {
	rawKey, err := ssh.ParseRawPrivateKey(keyPEM)
	if err != nil {
		return "", nil, fmt.Errorf("sshkey: falha ao parsear chave privada: %w", err)
	}

	ring := agent.NewKeyring()
	if err := ring.Add(agent.AddedKey{PrivateKey: rawKey}); err != nil {
		return "", nil, fmt.Errorf("sshkey: falha ao carregar chave no agent: %w", err)
	}

	sockPath := agentSocketPath()
	_ = os.Remove(sockPath) // remove socket stale de execução anterior

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return "", nil, fmt.Errorf("sshkey: falha ao criar socket do agent em %s: %w", sockPath, err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener fechado pelo stop()
			}
			go func() {
				defer conn.Close()
				_ = agent.ServeAgent(ring, conn)
			}()
		}
	}()

	stopFn := func() {
		ln.Close()
		_ = os.Remove(sockPath)
	}
	return sockPath, stopFn, nil
}

func agentSocketPath() string {
	pid := strconv.Itoa(os.Getpid())
	switch runtime.GOOS {
	case "linux":
		return fmt.Sprintf("/run/user/%d/gitsync-agent-%s.sock", os.Getuid(), pid)
	case "darwin":
		return filepath.Join(os.TempDir(), "gitsync-agent-"+pid+".sock")
	default: // windows e outros
		return filepath.Join(os.TempDir(), "gitsync-agent-"+pid+".sock")
	}
}
