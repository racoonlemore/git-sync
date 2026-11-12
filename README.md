# git-sync

CLI que sincroniza automaticamente um repositório git **já existente** com seu
remoto: observa a pasta e, com debounce, faz `pull` + `add` + `commit` + `push`;
faz `pull` periódico; mantém o histórico enxuto; e resolve conflitos com a
política "o remoto sempre vence". Ideal para pastas de arquivos (inclusive
binários) sincronizadas entre várias máquinas.

git-sync **nunca** roda `git init` nem clona: ele atua apenas sobre um
repositório com `.git` e remote já configurados.

## Instalação

Baixe o binário do seu sistema na página de [Releases](../../releases), ou
compile:

```sh
go build -o git-sync .
```

## Uso

```sh
git-sync validate    # checa se o repositório está pronto e se já está instalado
git-sync setup       # valida e registra o autostart no login do usuário
git-sync uninstall   # remove o autostart deste repositório (não toca no remote/histórico)
```

Rode dentro do repositório, ou aponte com `--repo <caminho>`.

Após o `setup`, um processo em segundo plano passa a rodar no seu login e
sincroniza o repositório automaticamente. Não há nada a configurar.

## Atualização

```sh
git-sync update check     # há versão nova? (resposta em cache por 12h; --force ignora o cache)
git-sync update apply     # baixa o release, confere o sha256 e troca o binário
```

O `apply` baixa o binário desta plataforma do último release do GitHub, recusa
se o sha256 não conferir com o publicado pelo GitHub e **preserva a chave SSH
embutida** (`add-key`), gravando-a no binário novo antes da troca. Os processos
em segundo plano seguem na versão antiga até reiniciarem (no Linux:
`systemctl --user restart 'git-sync-*'`). A pasta do binário precisa ser
gravável pelo seu usuário. Se o repositório de releases for privado, defina
`GITHUB_TOKEN`. Builds locais (versão `dev`) não se atualizam. Ver
[ADR 0004](docs/adr/0004-self-update-preserva-chave.md).

## Comportamento (fixo, não configurável)

- **Debounce de 1 minuto:** mudanças em rajada viram um único commit.
- **Pull a cada 1 hora**, além do pull a cada ciclo de sync.
- **Mensagem de commit:** `sync dd-mm-yyyy hh:mm (nome do git user.name)`.
- **Limite de 100 commits** na branch: ao atingir o limite, o histórico é reescrito
  mantendo só os 50 mais recentes e é feito um push forçado (`--force-with-lease`).
  A reescrita acontece uma vez a cada 50 syncs (ver [ADR 0005](docs/adr/0005-trim-com-folga.md)).
- **O remoto sempre vence:** em conflito de conteúdo (`pull`) ou rejeição de
  `push` por divergência, o git-sync faz `fetch` + `reset --hard` para o remoto
  e descarta o ciclo local — sem coordenação entre máquinas. Mudanças locais
  conflitantes são perdidas deliberadamente (ver [ADR 0001](docs/adr/0001-remote-wins-conflicts.md)
  e [ADR 0002](docs/adr/0002-trim-sem-coordenacao.md)).

## Autostart (serviço de usuário, nunca de sistema)

Registrado no contexto do seu login para ter acesso às credenciais git (chave
SSH, credential helper) sem configuração extra:

| SO      | Mecanismo                                              |
|---------|--------------------------------------------------------|
| Linux   | unit de `systemd --user`                               |
| macOS   | `LaunchAgent` (`~/Library/LaunchAgents`, `RunAtLoad`)  |
| Windows | tarefa do Task Scheduler, gatilho *at log on*, `LIMITED` |

Cada repositório tem sua própria unit/tarefa, nomeada de forma determinística
como `git-sync-<pasta>-<hash-do-caminho>`.

## Desenvolvimento

```sh
go test ./...      # testes (criam repositórios git reais e temporários)
go vet ./...
```

Ver [CONTEXT.md](CONTEXT.md) e [docs/specs/git-sync.md](docs/specs/git-sync.md)
para o domínio e as decisões de arquitetura.
