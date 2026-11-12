# git-sync

git-sync é um aplicação CLI. O objetivo dessa aplicação é gerenciar um repositório git e realizar sincronização de forma automatica. Fazendo git push de forma automatica quando os arquivos mudam fazendo pull a cada X tempo.

# Requisitos Iniciais

- git-sync deve ser um binário único servido para windows, mac e linux.
- git-sync deve atuar APENAS sobre um repositório git já existente (tem `.git`) e com remote configurado; nunca inicializa (`git init`) nem clona um repositório. Rodar `setup` ou `validate` numa pasta que não atende isso deve retornar erro sinalizado ao usuário.
- git-sync roda um processo em background por repositório (não há registro central): cada `setup` cria sua própria unit de serviço (systemd no Linux) e sua própria instância do processo, escutando apenas o repositório em que foi rodado. "Repositórios registrados" significa apenas "os repositórios onde `setup` já foi executado", não uma lista compartilhada.

# Níveis de Implementação

## Binário

- esse reposítorio será hospedado no github, por isso, você deverá fazer um arquivo de workflow para que seja possível baixar binários para windows, linux e mac

## Comandos

### validate

- esse comando deverá validar o repositório está pronto o git-sync funcionar: verificar se a pasta atual é um repositório git existente (tem `.git`) e com remote configurado, sinalizando erro se não for; verificar se há `git config user.name` (local ou global), sinalizando erro se não houver nenhum; também verificar se o git-sync já está instalado para esse repositório — critério é a existência da unit de autostart de usuário (systemd/launchd/Task Scheduler) vinculada a esse repositório; não há marcador separado dentro do repo. A unit é identificada por um nome determinístico `nome_da_pasta_(hash do caminho absoluto)`, igual em todas as plataformas.

### setup

- esse comando deverá fazer o setup do git-sync que será basicamente:
  - rodar as checagens do `validate` primeiro; se falhar, aborta o setup sem instalar nada
  - verificar se git-sync ja foi setado para esse repositório; se já estiver, o comando é um no-op (não reinstala nem duplica nada)
  - deverá registrar o autostart como **serviço de usuário** (nunca de sistema/boot), pra ter acesso direto às credenciais git do usuário (chave SSH, `git config`, credential helper) sem configuração extra; isso implica iniciar apenas após o login do usuário, não antes:
    - Linux: unit de `systemd --user`
    - macOS: `LaunchAgent` (não `LaunchDaemon`) em `~/Library/LaunchAgents`, com `RunAtLoad`
    - Windows: tarefa no Task Scheduler com gatilho "at log on" rodando com a conta do usuário (não um Serviço do Windows rodando como SYSTEM)
  - voce deverá realizar o setup do programa em background (que escuta mudanças do repositório atual)
  - o programa em background fara:
    - ele deverá escutar as mudanças no folder; a cada mudança, reinicia um debounce de 1 minuto (para evitar que multiplas mudanças gerem multiplos commits)
    - quando o debounce expirar, ele deve tentar um `git pull`; se houver conflito (arquivo alterado tanto local quanto remotamente), o remoto prevalece: faz `fetch` + `reset --hard` para o remoto, descartando as mudanças locais conflitantes sem deixar rastro no histórico; caso não haja conflito, segue com `add`, `commit` e `push`
    - a mensagem de commit deverá ser "sync dd-mm-yyyy hh:mm (nome do usuário git)", onde o nome vem do `git config user.name` local do repositório, com fallback pro `user.name` global caso não haja local; se nenhum dos dois estiver configurado, é erro (sinalizado ao usuário, não segue sem o nome)
    - o programa deverá a cada 1h, fazer o pull do repositório
    - o programa deverá manter um limite de 100 commits na branch atual: ao atingir o 100º commit, deve reescrever o histórico local (squash/orphan) mantendo apenas os 50 commits mais recentes e fazer `push --force-with-lease` (ADR 0005; antes era N=20, reescrevendo a cada commit)
    - a mesma política de "o remoto sempre vence" vale também para o `push` (não só para o `pull`): se o `push` (inclusive o `push --force` do trim) for rejeitado por divergência do remoto — por exemplo, outra máquina já sincronizou ou já fez o trim primeiro — a máquina faz `fetch` + `reset --hard` para o remoto e pula esse ciclo, sem coordenação entre máquinas

### uninstall

- esse comando deverá desfazer tudo, inclusive questões de inicialização (para o processo em background, remove a unit de autostart de usuário do repositório atual). Escopo é só local: não mexe no remote nem no histórico de commits do repositório.

Não há comando `config`: os três parâmetros do programa em background (debounce de 1 minuto, pull horário, limite de 100 commits, mantendo 50 no trim) são fixos e não configuráveis pelo usuário.

# Implementação (Go)

Módulo `github.com/racoonlemore/git-sync`. Layout:

- `main.go` — dispatch dos subcomandos (`validate`, `setup`, `uninstall`, `run`, `add-key`, `remove-key`, `update`, `version`) via `flag`; sem dependência de CLI framework.
- `internal/gitops` — wrappers finos sobre o binário `git` executado num diretório de repositório (`Repo`). Inclui `Trim`, que reescreve a branch para os N commits mais recentes via `commit-tree` (snapshot de árvores, sem reaplicar patches — seguro para binários), e detecção de rejeição de push por divergência.
- `internal/engine` — **seam 1**, o motor de sincronização. `SyncCycle` (pull→add→commit→push, trim ao exceder o limite), `PullCycle` (pull horário, adiado se há mudanças pendentes) e `Run` (laço com watcher `fsnotify` + debounce + ticker). A política "o remoto sempre vence" (ADR 0001/0002) vive aqui. Depende de `git` real; testado contra repositórios temporários com remoto bare local.
- `internal/autostart` — **seam 2**, registro como serviço de usuário. Interface `Installer` com implementações `SystemdInstaller` (Linux, `~/.config/systemd/user`), `LaunchdInstaller` (macOS, `~/Library/LaunchAgents`, `RunAtLoad`) e `SchtasksInstaller` (Windows, `schtasks /SC ONLOGON /RL LIMITED`). `New()` escolhe por `runtime.GOOS`. `UnitName` gera o nome determinístico `git-sync-<pasta>-<hash sha256 do caminho absoluto>`, idêntico nas três plataformas — geração de nome/conteúdo é pura e testada; a execução real de systemctl/launchctl/schtasks não é coberta por CI.
- `internal/update` — self-update a partir dos releases do GitHub (`update check` com cache de 12h em `<UserCacheDir>/git-sync/update.json`; `update apply` baixa o asset `git-sync-<goos>-<goarch>[.exe]`, confere o `digest` sha256 da API do GitHub, transplanta a chave SSH embutida com `sshkey.Transplant` e troca o binário; no Windows renomeia o exe em uso para `.old`). Ver ADR 0004.
- `internal/cli` — orquestra os comandos sobre os dois seams (checagens do `validate`, `setup`, `uninstall`, `run`).

Distribuição: `.github/workflows/release.yml` compila binários únicos para linux/darwin/windows (amd64/arm64) ao criar tag `vX.Y.Z`; `ci.yml` roda `vet` + `test -race` e cross-compila em cada push/PR.

Única dependência externa: `github.com/fsnotify/fsnotify` (observação de arquivos).

.

# Chaves SSH

Sem recompilação e sem tocar o disco, o design fica em duas partes:

---

1. Self-patch do binário (sem Go toolchain)

O binário carrega um placeholder fixo com magic marker:

// sshkey/key.go
var embeddedKey = [4096]byte{
0x00, 'G', 'S', 'Y', 'N', 'C', 'K', 'E', 'Y', 0x00, // magic marker
// resto zerado
}

O comando add-key não recompila — ele abre o próprio binário, localiza o magic marker, sobrescreve os bytes da chave, salva. Sem Go, sem source, só manipulação de bytes.

---

2. SSH agent in-process (chave nunca toca disco)

Na hora do push/pull, em vez de escrever a chave em arquivo temporário:

1. Sobe um mini SSH agent in-process usando golang.org/x/crypto/ssh/agent (já está na stdlib estendida)
2. Cria o socket em /run/user/$UID/gitsync-agent.sock — tmpfs, só RAM, nunca vai pro disco
3. Seta SSH_AUTH_SOCK no subprocesso git
4. Derruba o agent e o socket depois da operação

---

Fluxo completo

add-key ~/.ssh/id_ed25519 → patcha os bytes no próprio binário
git-sync run → lê bytes embedados → sobe agent in-process em tmp
→ git usa SSH_AUTH_SOCK → agent responde → socket

Quer que eu implemente?
