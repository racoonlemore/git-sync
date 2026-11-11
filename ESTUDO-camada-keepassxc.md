---
status: draft
date: 2026-11-10
---

# Estudo: git-sync como camada multi-SO sobre o KeePassXC

Estudo de viabilidade e arquitetura. **Nenhum código do projeto foi alterado.**
Os experimentos foram feitos em diretórios descartáveis fora do repositório.

Pedido original: (1) funcionar em qualquer SO; (2) criptografia com GPG;
(3) assinatura de commits; (4) o projeto deixa de ser só um sync e vira uma
camada em cima do KeePassXC do sistema.

## Legenda de evidência

| Marca | Significado |
|---|---|
| **[V]** | Verificado em fonte primária: documentação ou código-fonte do projeto dono (links no Apêndice B). |
| **[C]** | Verificado lendo o código deste repositório (`arquivo:linha`). |
| **[T]** | Verificado por experimento local. Ambiente: Linux, git 2.43.0, OpenSSH 9.6p1, Go 1.26.1, GnuPG 2.5.20. |
| **[S]** | Fonte secundária (resultado de busca, não a documentação do dono). Trate como indício. |
| **[I]** | Inferência minha. Não foi verificada. |
| **[?]** | Desconhecido. Precisa de teste ou leitura adicional antes de decidir. |

---

## 1. Resumo executivo

1. **A assinatura de commits é a parte mais barata e já funciona com o que existe.**
   Um agent SSH em memória, como o de `internal/sshkey/agent.go`, assina commits
   com `gpg.format=ssh`, sem a chave privada em disco, para ed25519 e RSA **[T]**.
   O diff é pequeno: flags `-c` no `commit` e `-S` no `commit-tree` do `Trim`.
2. **O `Trim` multiplica o custo de assinar.** Com a branch cheia, todo ciclo
   reescreve os 20 commits via `commit-tree`, e `commit-tree` **não assina sem
   `-S`**, mesmo com `commit.gpgsign=true` **[T]**. Cada ciclo exige 20 assinaturas.
   Isso descarta qualquer chave que peça confirmação a cada uso.
3. **O KeePassXC só documenta integração de agent para SSH; não há equivalente
   documentado para AWS ou GPG** **[V]**. O Secret Service
   dele só expõe atributos não protegidos (sem anexos e sem campos protegidos)
   **[V]**, só existe em Linux **[V]** e briga com o gnome-keyring **[V][T]**.
   Não serve como base multi-SO.
4. **Caminho multi-SO mais realista:** um leitor `.kdbx` nativo em Go
   (`gokeepasslib`, mantido ativamente **[V]**), com um modelo `unlock` no estilo
   `ssh-add`. O daemon guarda os segredos já abertos em memória e recebe o unlock
   por IPC (socket Unix ou named pipe). `keepassxc-cli` fica como backend
   secundário e para o `merge`.
5. **GPG não é necessário para assinar** (o SSH resolve). Para **cifrar**, o
   `.kdbx` já é cifrado. Para arquivos comuns, as opções reais são `age`
   (preferível) ou `ProtonMail/go-crypto` (se precisar interoperar com chaves
   OpenPGP). `golang.org/x/crypto/openpgp` está marcado como "não deve ser usado" **[V]**.
6. **Windows é o maior risco aberto.** Não verifiquei se o `ssh`/`ssh-keygen` do
   Windows aceita o socket AF_UNIX que o agent atual cria **[?]**. É o primeiro
   item da Fase 0.
7. **Ponto de atrito com decisões existentes:** a spec fixa "sem `config`", "um
   processo por repositório" e "remoto sempre vence". Esta evolução mexe nas três.
   Cada mudança precisa virar ADR (seção 6).

---

## 2. Estado atual (resumo do código)

- Binário único, `systemd --user` / LaunchAgent / Task Scheduler, um processo
  por repositório, sem registro central (`docs/specs/git-sync.md`, "Out of Scope") **[C]**.
- Chave SSH privada **gravada em texto dentro do próprio binário**
  (`sshkey.Patch`, placeholder de 4096 bytes, `internal/sshkey/key.go:16-18`) **[C]**.
  `add-key` recusa chaves com passphrase (`internal/cli/addkey.go:22-25`) **[C]**.
- Ao iniciar, `Run` sobe um agent SSH em memória e injeta `SSH_AUTH_SOCK` só nos
  subprocessos `git` via `Repo.Env` (`internal/cli/run.go:45-59`,
  `internal/gitops/gitops.go:28-30,48-50`) **[C]**.
- `agentSocketPath`: Linux `/run/user/<uid>/gitsync-agent-<pid>.sock`; macOS e
  Windows usam `os.TempDir()` (`internal/sshkey/agent.go:58-68`) **[C]**.
- `Commit` é `git commit -m` (`gitops.go:159-162`) **[C]**. Não há assinatura.
- `SyncCycle` faz `Trim` sempre que `count > maxCommits`, e em seguida
  `push --force` (`internal/engine/engine.go:177-188`). `Trim` reconstrói cada
  commit com `commit-tree <tree> -p <parent>` e a mensagem em stdin
  (`gitops.go:279-324`) **[C]**.
- Política "remoto sempre vence" para conflito de pull e rejeição de push
  (ADR 0001 e 0002) **[C]**.
- Caso de uso real observado: um repositório de backup com bancos KeePass (cabeçalho
  `03d9a29a67fb4bb5`, a assinatura de arquivo KDBX), **sem extensão `.kdbx`** **[T]**.
  O KeePassXC **não está instalado** nesta máquina, e o gnome-keyring é o dono de
  `org.freedesktop.secrets` **[T]**.

---

## 3. Análise por tema

### 3.1 Ler segredos do KeePassXC a partir do Go

| Backend | Linux | macOS | Windows | Headless | Anexos / campos protegidos | Dependência externa |
|---|---|---|---|---|---|---|
| **A. `keepassxc-cli`** (subprocesso) | sim | sim | sim | só com senha vinda de algum lugar | sim | KeePassXC instalado |
| **B. Protocolo browser (native messaging)** | sim | sim | sim | não (precisa da GUI aberta e desbloqueada) | não documentado | KeePassXC GUI rodando |
| **C. Secret Service (D-Bus)** | sim | **não** [I] | **não** [I] | só com GUI aberta | **não** | KeePassXC GUI + abrir mão do gnome-keyring |
| **D. Leitor Go nativo** (`gokeepasslib`) | sim | sim | sim | sim (com unlock em memória) | sim | nenhuma |

**A. `keepassxc-cli`** **[V]**
- Abre com `-k/--key-file`, `--no-password` e `-y/--yubikey <slot[:serial]>`; `-q` silencia o prompt de senha.
- `show -a <attr>` pode repetir `-a`, com **um valor por linha, na ordem pedida**.
  Atributos protegidos saem em claro quando pedidos com `-a`.
- `attachment-export --stdout` escreve o anexo no stdout, sem arquivo temporário.
  Isso serve para um anexo com chave SSH.
- `merge <db1> <db2>` **substitui o db1 pelo resultado** e recomenda backup antes.
  Aceita `-s/--same-credentials`, `--dry-run`, `--key-file-from` e `--yubikey-from`.
- **[?]** O man page não documenta leitura da senha mestra por stdin. Falta testar
  `printf %s "$senha" | keepassxc-cli ...` (o binário não está instalado aqui).
  Sem isso, um processo sem TTY não consegue abrir o banco.
- Cada chamada paga o custo do KDF (Argon2) de novo **[I]**. Não serve para chamar a
  cada ciclo; serve para unlock único ou operações raras (`merge`).

**B. Protocolo browser** **[V]**
- Ações documentadas incluem `get-logins`, `get-totp`, `lock-database`,
  `database-locked`/`database-unlocked` e `associate`/`test-associate`
  (cada cliente se associa ao banco com um par de chaves).
- O exemplo documentado de `get-logins` devolve apenas `login`, `name`, `password`
  e `expired`. Anexos e atributos customizados **não aparecem** na documentação.
- O transporte é via `keepassxc-proxy` (native messaging). O documento do protocolo não
  descreve socket/pipe **[?]**.
- Vantagem: o git-sync não tocaria na senha mestra. Desvantagem: depende de GUI
  rodando e de aprovação do usuário a cada associação. Não resolve o SSH (anexo).

**C. Secret Service** **[V]**
- "Only one secret service provider can be enabled at a time"; é preciso desativar
  GNOME Keyring ou KWallet. **Nesta máquina o gnome-keyring é o dono do barramento** **[T]**.
- Só expõe Title, Username, Password, URL, Notes, TOTP e **atributos customizados
  não protegidos**. Anexos e campos protegidos ficam de fora.
- Com confirmação ativa, `secret-tool` pede aprovação **a cada chamada** (novo PID).
- A documentação fala em D-Bus. Que macOS e Windows ficam sem equivalente é inferência **[I]**.

**D. `gokeepasslib`** **[V]** (README, `header.go`, `credentials.go`, `binary.go`, `go.mod`)
- Release v3.7.1 em 2026-10-23; último commit no mesmo dia.
- KDBX 3.1, 4.0 e 4.1 (leitura sem opção; versão detectada).
- KDF: AES-KDF e Argon2 (via fork `tobischo/argon2`). Cifras ChaCha20 e Twofish presentes.
- Credenciais: senha, **keyfile** (`ParseKeyFile`) e o campo `Windows`.
- Anexos: `Binaries` / `GetContentBytes()`.
- **YubiKey challenge-response: sem ocorrências no código** **[I]**: não suporta.
- **Argon2id: nenhuma ocorrência do termo no código** **[?]**. Testar com `.kdbx` real
  gerado pelo KeePassXC com cada KDF antes de depender disso.
- `go.mod` exige Go 1.26.0 e `x/crypto` v0.58.0. O projeto já está em Go 1.26.1 **[C]**, então é compatível.
- Risco: o git-sync passaria a **interpretar formato criptográfico dentro do próprio
  processo**. Mitigação: usar a lib só para leitura; deixar escrita e `merge` com o
  KeePassXC **[I]**.

**Integração com o SSH agent do KeePassXC** **[V]**: ele "não fornece um agent; funciona
como cliente de qualquer agent compatível com OpenSSH" e carrega as chaves no **agent
do sistema**. Isso **não alimenta** o agent in-process do git-sync. Num serviço
`systemd --user`, `SSH_AUTH_SOCK` pode nem estar no ambiente **[I]**. Por isso, ler o
anexo direto do `.kdbx` é mais robusto do que depender do agent do sistema.

### 3.2 Daemon headless sem prompt de senha: modelo `unlock`

O problema: o serviço sobe no login e **não tem TTY** para pedir a senha mestra.

Modelo proposto, no mesmo espírito do `ssh-add` **[I]**:
1. `git-sync unlock` (interativo) pede a senha mestra, abre o `.kdbx` e entrega ao
   daemon **só os segredos necessários** (chave SSH, credenciais), nunca a senha mestra.
2. O daemon guarda isso em memória, com TTL opcional, e `git-sync lock` descarta tudo.
3. Transporte do IPC: socket Unix em Linux/macOS; **named pipe** no Windows
   (`go-winio` trata de named pipes como transporte `net` **[V]**).
4. Sem unlock, o daemon **não sincroniza** e registra o motivo (o comportamento de
   hoje com chave embutida deixa de ser "sempre ligado").

Alternativas e seus custos **[I]**: keyfile em disco ao lado do banco (derrota o
propósito); YubiKey via `keepassxc-cli -y` (exige hardware e o backend A);
"Quick Unlock" do KeePassXC existe só na GUI.

**Tensão com a arquitetura atual:** a spec decidiu "um processo por repositório,
sem daemon central" (Opção B). Com N repositórios, `unlock` teria de alcançar N
processos ou passar a existir um processo único de cofre. Isso vira ADR.

**Achado do experimento [T]:** um caminho de socket com 123 caracteres falha com
`bind: invalid argument`. O limite de `sun_path` no Linux é 108. No macOS o limite
é menor (104) e o `os.TempDir()` de `agentSocketPath` é longo **[I]**. O código atual
ainda não protege contra isso. Em Linux, `/run/user/<uid>/` é curto o bastante.

### 3.3 Assinatura de commits

Formatos que o git aceita: `gpg.format` = `openpgp` (padrão), `x509` ou `ssh`;
programas padrão `gpg`, `gpgsm` e `ssh-keygen` **[V]**.

**SSH signing** **[V]**
- Introduzido no git 2.34. **Não funciona com `ssh-keygen` do OpenSSH 8.7** (quebrado);
  usar ≥ 8.8 **[V]** (RelNotes 2.34.0).
- `user.signingkey` aceita chave pública literal com prefixo `key::`, e a privada
  "needs to be available via ssh-agent" **[V]**.
- `ssh-keygen -Y sign -f` aceita "uma chave privada **ou uma pública cuja metade privada
  está no ssh-agent**" **[V]**.
- Verificação local usa `gpg.ssh.allowedSignersFile`; SSH não tem níveis de confiança
  e a revogação usa `gpg.ssh.revocationFile` **[V]**.
- GitHub: assinaturas SSH são aceitas ("the simplest to generate"); dá para usar a chave de
  autenticação existente como chave de assinatura. Já a chave GPG pode expirar ou ser
  revogada **[V]**. A verificação fica persistida por commit **[V]**.

**Experimento [T]** (Apêndice A): agent em memória com `x/crypto/ssh/agent` (v0.58.0) +
`git -c gpg.format=ssh -c user.signingkey=key::<pub> -c commit.gpgsign=true`, **chave
privada apagada do disco**:

| Caso | ed25519 | RSA-3072 |
|---|---|---|
| `git commit` assinado via agent | OK | OK |
| `verify-commit` com `allowed_signers` | "Good git signature" | "Good git signature" |
| `commit-tree -S` (como no Trim) | OK e verificado | OK e verificado |
| `commit-tree` **sem** `-S`, com `commit.gpgsign=true` | **sem assinatura** | **sem assinatura** |

**Impacto no `Trim`** **[C][T]**
- Após 20 commits, **todo ciclo** vira Trim: reescreve 20 commits e faz `push --force`.
  Logo, são **20 assinaturas por ciclo**, e as assinaturas antigas são descartadas
  (os objetos são recriados).
- `Trim` hoje passa só a árvore, o pai e a mensagem **[C]**. Autoria e data dos commits
  reescritos vêm da configuração/hora atuais da máquina que fez o trim **[I]**.
  Consequência: a assinatura atesta **"a última máquina que sincronizou"**, não o
  autor original. Com várias máquinas, o `allowed_signers` precisa listar todas as chaves.
- Uma chave que exija confirmação por uso (opção de agent) geraria 20 prompts por
  ciclo. A chave de assinatura tem de estar num agent **sem confirmação**.

**Como injetar a config sem tocar na do usuário** **[T][I]**
- `-c` na linha de comando (usado no experimento) ou `GIT_CONFIG_COUNT` /
  `GIT_CONFIG_KEY_n` / `GIT_CONFIG_VALUE_n` no ambiente. Testei as variáveis
  localmente com git 2.43 **[T]**; a versão mínima do git que as suporta não foi
  conferida aqui **[?]**. O `Repo.Env` já existe para isso **[C]**.

**GPG (OpenPGP)**
- O git chama `gpg --status-fd=2 -bsau <key>` e procura `[GNUPG:] SIG_CREATED ` na
  saída **[V]** (`gpg-interface.c:1019-1035`). Portanto o próprio git-sync poderia ser
  um **shim de `gpg.program`**, mas precisaria gerar assinatura OpenPGP desacoplada
  com `go-crypto`. É bem mais trabalho do que SSH **[I]**.
- Usar o `gpg` real num serviço sem TTY exige `gpg-agent` e `pinentry` **[I]**.
- O KeePassXC avisa que o agent SSH do GnuPG **não é compatível** com a integração
  dele (não remove chaves) **[V]**. Isso vale só para o modo agent SSH do GPG.
- **Recomendação:** SSH como padrão. GPG só se houver requisito concreto.

**Pontos em aberto da assinatura**
- **[?]** O GitHub marca "Verified" só se o e-mail do committer estiver verificado na
  conta dona da chave? Não confirmei na documentação. O `commit` do git-sync usa o
  `user.email` da máquina.
- **Fail-open ou fail-closed?** Se o agent/cofre estiver indisponível, o ciclo
  falha ou commita sem assinatura? Decisão de produto (ADR).
- **Verificar na entrada:** antes de aplicar "remoto vence", checar assinaturas dos
  commits recebidos contra o `allowed_signers`. Barra push de quem não tem chave,
  mas **não barra replay** de um commit antigo legítimo **[I]**.

### 3.4 Criptografia (GPG e alternativas)

Primeiro, o que **já** é cifrado: o `.kdbx` usa a cifra do próprio formato (AES,
ChaCha20 ou Twofish, com KDF Argon2/AES) **[V]** (constantes em `header.go` do
`gokeepasslib`). Cifrar de novo por cima agrega pouco. O que fica em claro no GitHub
são **os outros arquivos** do repositório (a spec fala em "pastas de arquivos").

| Opção | O que é | Pontos verificados | Ressalvas |
|---|---|---|---|
| **`age`** | formato e lib Go, chaves pequenas, sem opções | destinatários `age1…` **ou chaves SSH públicas**; senha; plugin YubiKey; README cita suporte pós-quântico **[V]** | não interopera com chaves GPG |
| **`ProtonMail/go-crypto`** | OpenPGP em Go, "backwards compatible" com `x/crypto/openpgp` | repositório ativo (último push 2026-09-29) **[V]** | superfície OpenPGP é grande |
| **`golang.org/x/crypto/openpgp`** | OpenPGP legado | pkg.go.dev: *"unsafe by design… not maintained, and should not be used"*; recomenda `go-crypto`. Versão `v0.1.0-deprecated` **[V]** | **descartar** |
| **`git-crypt`** | filtro do git, AES-256-CTR com IV sintético | **não cifra nomes de arquivo, mensagens de commit nem metadados**; não revoga acesso; README diz que não é a melhor ferramenta para cifrar "most or all files" **[V]** | determinístico; vaza igualdade de arquivos |
| **`git-remote-gcrypt`** | remote helper que cifra o repositório inteiro com GnuPG | README admite que "every git push effectively has a `--force`" (bug de longa data), com flag para exigir force explícito **[V]** | esconde os commits do GitHub: **perde a verificação de assinatura do GitHub** **[I]** e muda o modelo do ADR 0002 |

Efeito do Trim: blobs cifrados não deltificam bem **[I]**; o limite de 20 commits já
contém o crescimento.

**Recomendação [I]:** (a) não cifrar `.kdbx`; (b) se for preciso cifrar outros
arquivos, `age` com o **destinatário sendo a mesma chave SSH** do cofre; (c) usar
`go-crypto` só se existir chave OpenPGP preexistente a respeitar. Antes de decidir,
é preciso saber o que "criptografia com GPG" significa para o usuário (seção 7).

### 3.5 SSH agent e sockets no Windows e macOS

- O agent do OpenSSH for Windows escuta no **named pipe** `\\.\pipe\openssh-ssh-agent`;
  vários clientes usam esse nome como padrão **[S]**.
- Há indício de comportamentos diferentes entre clientes quanto a `SSH_AUTH_SOCK`: a
  documentação do JGit diz que ela **não deve** estar definida com o Win32-OpenSSH,
  enquanto a libssh2 a lê primeiro **[S]**. **[?]** O que o `ssh.exe` e o `ssh-keygen`
  que o git usa no Windows fazem com um caminho AF_UNIX em `SSH_AUTH_SOCK` não está
  verificado.
- AF_UNIX existe no Windows desde a build 17063 (dez/2017), para sockets de fluxo **[V]**
  (blog de Command Line da Microsoft).
- O KeePassXC no Windows suporta **Pageant** (padrão) e **OpenSSH for Windows**
  **[V]**. Ou seja, no Windows o "agent do KeePassXC" é um agent externo, de natureza
  diferente do in-process do git-sync.
- Consequência: `agentSocketPath` no Windows (`os.TempDir()` + `.sock`) **[C]** é uma
  aposta sem teste. Opções, em ordem de preferência **[I]**: (1) testar o AF_UNIX
  atual no Git for Windows e no OpenSSH nativo; (2) servir o agent em named pipe com
  `go-winio` e apontar `SSH_AUTH_SOCK` para ele, se o cliente aceitar; (3) último
  recurso, arquivo temporário com ACL restrita, que contraria a regra "chave nunca
  toca o disco" do design atual.
- A CI só cross-compila para Windows e macOS (`ci.yml`) **[C]**; a spec admite que a
  execução real de `systemctl`/`launchctl`/`schtasks` não é coberta. **Nada disso foi
  exercitado em Windows ou macOS.**

### 3.6 Conflito: ADR 0001 ("remoto vence") × `.kdbx`

Para um banco de senhas, "remoto vence" **descarta silenciosamente** uma entrada nova
criada em outra máquina dentro da janela de sync (ADR 0001, "sem rastro") **[C]**.
O próprio KeePassXC tem merge: `keepassxc-cli merge <db1> <db2>` **[V]**
(substitui o db1; usar `--dry-run` e backup; `-s` quando as credenciais são iguais).

Desenho possível **[I]**:
1. Detectar arquivo KDBX pela **assinatura do cabeçalho**, não pela extensão (os bancos
   observados não têm `.kdbx`) **[T]**.
2. No conflito, extrair a versão remota com `git show <remote>/<branch>:<arquivo>` para
   um diretório temporário com permissão `0700`, rodar o merge com as credenciais do
   cofre aberto e só então commitar.
3. Se o cofre estiver bloqueado: aplicar o ADR 0001 (remoto vence) **ou** preservar a
   cópia local numa cópia `*.conflict-<ts>`. O ADR 0001 rejeitou "preservar rastro"
   para arquivos binários em geral; para KDBX o custo de perder uma senha é bem maior.
   Isso pede um ADR que **substitua parcialmente** o 0001.

### 3.7 Modelo de ameaças

**Ativos:** conteúdo do `.kdbx`; chave SSH de push/assinatura; credenciais guardadas
(por exemplo, AWS); segredos abertos na memória do daemon.

**Adversários:** quem lê o repositório remoto; outros colaboradores do remoto (o
histórico observado tem **commits de mais de um autor** **[T]**: confirmar quem tem
acesso); malware rodando como o mesmo usuário; disco/notebook perdido.

**Fragilidades já existentes [C]:**
- A chave privada fica **em claro dentro de `~/.local/bin/git-sync`** (ou onde o binário
  estiver). Qualquer processo do usuário a lê, e qualquer backup ou cópia do binário a
  vaza. É o ponto mais fraco do desenho atual e um bom motivo para a camada do
  KeePassXC: a chave passa a existir só dentro do `.kdbx` (cifrado) e na memória do
  daemon.
- Chaves com passphrase não são suportadas (`addkey.go:22-25`).
- Qualquer processo do mesmo usuário pode usar o agent enquanto o daemon roda (igual
  ao `ssh-agent`). Em Linux, `/run/user/<uid>` é `0700` **[I]**.

**Novas superfícies com a camada de cofre [I]:**
- Socket/pipe de IPC do `unlock`: restringir ao usuário; conferir credenciais do peer.
- Segredos em memória: o GC do Go não garante apagar; swap e core dumps podem vazar.
  Desligar core dump do serviço; TTL curto para o unlock.
- Logs: o log local fica na raiz do repositório (ignorado pelo git) **[C]**; garantir
  que nunca receba segredo, nem em mensagens de erro do `keepassxc-cli`.
- Integridade: quem escreve no remoto pode fazer **rollback** para uma versão antiga do
  banco; "remoto vence" aceitaria. Assinatura mais `allowed_signers` barra autores
  desconhecidos, mas não replay de commit legítimo antigo **[I]**.
- Confiança na lib de parsing KDBX: dependência nova em código sensível (3.1.D).

---

## 4. Arquitetura proposta (esboço)

Princípio: **mínimo para cada fase entregar valor sozinha**; sem abstração antes de
existir um segundo uso real.

- `internal/sshkey`: mantém o agent in-process; a **origem da chave** deixa de ser o
  binário patchado e passa a ser o cofre. `Patch`/`Clear` ficam como legado até o fim
  da migração.
- `internal/signing` (novo, pequeno): produz os flags/env do git para assinatura SSH e
  deriva a chave pública do PEM. Usado por `gitops.Commit` e `gitops.Trim`.
- `internal/vault` (novo): lê o `.kdbx` (D) e, opcionalmente, chama `keepassxc-cli` (A)
  para `merge`. Uma interface só se houver dois backends reais.
- IPC do `unlock`: socket Unix / named pipe, atrás de uma interface por SO, na mesma
  linha do seam `autostart.Installer` **[C]**.
- Comandos novos (propostos): `unlock`, `lock`, `status`; `add-key` e `remove-key`
  viram legado.

---

## 5. Plano em fases (lazy-first)

Cada fase vale por si e pode ser a última.

**Fase 0, spikes de 1 a 2 dias, sem merge:**
1. Windows: o `ssh-keygen -Y sign` e o transporte `git`/`ssh` aceitam o agent AF_UNIX atual? Testar Git for Windows e OpenSSH nativo.
2. `gokeepasslib` lendo `.kdbx` reais do KeePassXC com **Argon2d, Argon2id e AES-KDF**, com keyfile, e extraindo um **anexo** de chave SSH.
3. macOS: comprimento de `agentSocketPath` com um `$TMPDIR` real.
4. `keepassxc-cli` com senha via stdin e sem TTY.
5. GitHub: condições para "Verified" com assinatura SSH e `user.email`.

**Fase 1, assinatura SSH com a chave já embutida, sem KeePassXC:**
`-c gpg.format=ssh -c user.signingkey=key::<pub> -c commit.gpgsign=true` em `Commit`;
`-S` em cada `commit-tree` do `Trim`; derivar a pública do PEM. Teste: criar repo
temporário, ciclo de sync, `verify-commit`. O usuário cadastra a pública como
*signing key* no GitHub.

**Fase 2, cofre de leitura:** `unlock` com leitor Go; a chave SSH vem do anexo do
`.kdbx`; `sshkey.Patch` fica obsoleto. Depende do ADR sobre processo único × um por
repositório.

**Fase 3, merge de `.kdbx`:** substitui parcialmente o ADR 0001 para arquivos KDBX.

**Fase 4, provedor de credenciais:** comando que entrega credenciais do cofre a
outras ferramentas (por exemplo, `credential_process` da AWS). **[?]** O formato do
`credential_process` não foi verificado nesta pesquisa.

**Fase 5, opcional:** GPG (shim ou `gpg` externo) e/ou `age` para arquivos comuns.

---

## 6. Decisões que viram ADR

| # | Decisão | Por quê |
|---|---|---|
| A | **Escopo e nome do projeto**: deixa de ser só sync | contradiz "sem `config`" e "binário sem configuração" (`CONTEXT.md`) |
| B | **Backend de segredos**: leitor Go nativo × `keepassxc-cli` | dependência externa × risco de parsing |
| C | **Modelo de unlock e IPC**; relação com "um processo por repositório" | a spec proíbe daemon central |
| D | **Assinatura SSH como padrão**; fail-open × fail-closed | custo e semântica do Trim |
| E | **Conflito de KDBX** (substitui parcialmente o ADR 0001) | perda silenciosa de senhas |
| F | **Criptografia de arquivos comuns**: `age` × `go-crypto` × nada | depende do requisito real |
| G | **Fim da chave embutida no binário** (`sshkey.Patch`) | maior fragilidade atual |

---

## 7. Perguntas em aberto

1. **"Criptografia com GPG"** é cifrar os arquivos do repositório antes do push,
   proteger a chave em repouso, ou só assinar? Muda a resposta da seção 3.4.
2. **Quem tem acesso ao repositório remoto?** O histórico tem commits de mais de um
   autor. Se for compartilhado, entram as ameaças de colaborador.
3. **Windows e macOS são alvos reais agora?** Hoje só há cross-compile; ninguém
   exercitou autostart nem agent nesses SOs.
4. **Aceita `git-sync unlock` a cada login?** Ou prefere keyfile em disco / YubiKey?
5. **KeePassXC instalado é obrigatório?** Ou basta o formato `.kdbx` (leitor nativo)?
6. **Chave de assinatura por máquina ou compartilhada?** Afeta o `allowed_signers`.
7. **Se o cofre estiver bloqueado, o sync para (fail-closed) ou segue sem assinar?**
8. **O projeto continua se chamando `git-sync`?**

---

## 8. Discussão de 2026-11-11: features desejadas e ferramentas existentes

### 8.1 Features que o usuário quer

1. **Cifrar e decifrar com chaves GPG.**
2. **Commits assinados.**
3. **Credenciais e tokens (AWS e qualquer outro) guardados no `.kdbx` e usados de
   forma "nativa" pelas aplicações**: o usuário digita a senha do KeePass e tudo
   funciona, sem segredo gravado em arquivo. É a feature que mais interessa.

As features 1 e 2 seguem o desenho das seções 3.3 e 3.4. A feature 3 foi a que
mais se discutiu.

### 8.2 Desenho geral: agent + adaptadores **[I]**

Um agent guarda o `.kdbx` aberto em memória (TTL, lock ao bloquear a tela ou
suspender, `lock` manual). Cada aplicação recebe o segredo por um adaptador fino
que fala o protocolo que ela já entende. Se o agent estiver bloqueado, o
adaptador abre um prompt de senha (gráfico ou no terminal). O controle de acesso
por processo usa as credenciais do peer no socket (`SO_PEERCRED`) e é só parcial
contra malware rodando como o mesmo usuário. Cada entrada declara seu uso por
tag ou atributo (por exemplo `aws_access_key_id`, `aws_mfa_serial`).

Mecanismos considerados para a feature 3, do mais ao menos robusto:

| # | Mecanismo | Observação |
|---|---|---|
| 1 | Helpers de credencial das ferramentas (AWS `credential_process`, git `credential.helper`, Docker `credHelpers`, kubectl exec plugin, Terraform, Cargo) | específico por ferramenta; formatos ainda não conferidos **[?]** |
| 2 | Endpoint HTTP local que imita a AWS (`AWS_CONTAINER_CREDENTIALS_FULL_URI`) | cobre qualquer SDK da AWS; o `aws-vault --server` faz isso **[S]** |
| 3 | `exec` com injeção de variáveis de ambiente | universal |
| 4 | **`.env` com referências a entradas do cofre**, resolvidas no `run` | **escolhido pelo usuário como o mais interessante**: não prende o binário a protocolos de cada ferramenta |
| 5 | Arquivo virtual (FIFO/FUSE) | último recurso; não funciona no Windows |
| 6 | Ser o keyring do SO (Secret Service) | só Linux; conflita com o KWallet/gnome-keyring; descartado como base |

Ganhos específicos da AWS **[I]**: guardar só a chave de longo prazo e entregar
credenciais temporárias via STS; gerar o código MFA a partir do TOTP da entrada
(cômodo, mas junta senha e segundo fator no mesmo cofre).

Para GPG (feature 1), a opção mais pragmática levantada: `GNUPGHOME` efêmero em
tmpfs (`/run/user/<uid>/…`), criado no unlock com a chave vinda de um anexo do
`.kdbx` e destruído no lock, para `gpg`, `pass`, `sops` e git funcionarem sem
mudança **[I]**. No Windows e no macOS não há tmpfs garantido **[?]**.

Limites do modelo `.env` **[I]**: variável de ambiente é legível pelo mesmo usuário
(`/proc/<pid>/environ`) e herdada pelos filhos; apps que só leem arquivo
(`kubeconfig`, SSO da AWS, `.npmrc`) precisam que o `run` gere arquivos
temporários em tmpfs e aponte a variável para eles.

### 8.3 O que já existe (pesquisa de 2026-11-11)

O modelo "`.env` com referências + `run`" já existe para KeePass. Fontes lidas
nos READMEs e documentações dos projetos **[V]**, salvo indicação:

| Projeto | Linguagem | Referência | Unlock | Notas |
|---|---|---|---|---|
| [desktop-secrets](https://github.com/it-atelier-gn/desktop-secrets) | Go | `.env.tpl` com `keepass(...)`; `tplenv run` | prompt + **daemon com TTL**; aprovação por uso opcional | o mais próximo da ideia; multi-SO; MIT; 1 estrela |
| [fnox](https://fnox.jdx.dev/providers/keepass.html) | Rust | `fnox.toml`; `fnox exec` | env, campo de config ou prompt no terminal; hook de shell não pede senha | o mais maduro; lê `.kdbx` direto; cita `daemon_cache` **[?]** |
| [Varlock](https://varlock.dev/plugins/keepass/) | JS/TS | `.env.schema` com `kp("Grupo/Entrada#Campo")` | senha vinda de env | sintaxe mais rica; arquivo (WASM) ou `keepassxc-cli` |
| [yarrasys/kdbx](https://github.com/yarrasys/kdbx) | Go | `.keepassxc.json`; `kdbx run` | só keyfile, sem daemon | cofre por projeto e ambiente; MIT |
| [keeenv](https://github.com/scross01/keeenv) | Python | `.keeenv` com `${"Entrada".Password}` | prompt a cada execução | simples |
| [keepassxc-run](https://pypi.org/project/keepassxc-run/) | Python | `keepassxc://…` | o KeePassXC GUI faz o unlock (via `git-credential-keepassxc`) **[S]** | clone do `op run` |

A referência de UX de todos é o [`op run` do 1Password](https://developer.1password.com/docs/cli/secrets-environment-variables):
`.env` com `op://vault/item/campo`, `op run --env-file`, mascaramento do stdout
e variáveis dentro da referência para trocar de ambiente.

**Lacuna:** nenhum junta unlock único estilo agent, cofre sincronizado entre
máquinas (o papel do git-sync) e SSH/assinatura/GPG saindo do mesmo `.kdbx`.

### 8.4 Próximos passos sugeridos

1. Spike no `agent-dev`: `.kdbx` de teste com uma entrada AWS falsa; testar
   desktop-secrets e fnox com o fluxo real. Decidir entre **adotar** um deles para
   a feature 3 (e focar em sync + SSH/assinatura + GPG) ou **reimplementar**
   (o desktop-secrets é Go e MIT, bom para estudar daemon e resolvedor).
2. Decidir se a camada é um projeto separado, com o git-sync como cliente, ou se
   fica dentro do git-sync (ADR A da seção 6).

### 8.5 Feito na mesma sessão: self-update

Implementado `git-sync update check` / `update apply`, no molde do `taskthing`,
com duas diferenças: preserva a chave SSH embutida (`sshkey.Transplant`) e exige
o sha256 publicado pelo GitHub. Ver `docs/adr/0004-self-update-preserva-chave.md`.

---

## Apêndice A: experimento de assinatura (reproduzível)

Fora do repositório (diretório descartável). Resumo do procedimento:

1. Programa Go mínimo, equivalente a `sshkey.StartAgent`: `agent.NewKeyring()` +
   `ring.Add(...)` + `net.Listen("unix", ...)` + `agent.ServeAgent`. `golang.org/x/crypto` v0.58.0.
2. `ssh-keygen -t ed25519` (e `-t rsa -b 3072`) sem passphrase; subir o agent com a
   chave; **`shred -u` na chave privada**.
3. Repositório temporário; `SSH_AUTH_SOCK` apontando para o agent; flags
   `-c gpg.format=ssh -c user.signingkey=key::<pub> -c commit.gpgsign=true`.
4. `git commit`, `git commit-tree <tree> -S`, `git commit-tree <tree>` (sem `-S`),
   e `git -c gpg.format=ssh -c gpg.ssh.allowedSignersFile=… verify-commit`.

Resultados: ver a tabela da seção 3.3.

**Armadilha encontrada:** a primeira tentativa falhou porque o caminho do socket
(123 caracteres) excedeu o limite de 108 de `sun_path`. O erro apareceu como
`bind: invalid argument` no agent e como `Couldn't get agent socket?` no git. Isso
**não era** um problema de assinatura.

---

## Apêndice B: fontes

**Primárias [V]**
- KeePassXC, man page do CLI: `docs/man/keepassxc-cli.1.adoc` (github.com/keepassxreboot/keepassxc)
- KeePassXC, `docs/topics/SecretService.adoc`, `SSHAgent.adoc`, `BrowserIntegration.adoc`
- Protocolo do browser: `keepassxc-protocol.md` (github.com/keepassxreboot/keepassxc-browser)
- `gokeepasslib` (github.com/tobischo/gokeepasslib): `README.md`, `header.go`, `credentials.go`, `binary.go`, `go.mod`, releases e commits
- git: `Documentation/config/gpg.adoc`, `Documentation/config/user.adoc`, `Documentation/git-commit-tree.adoc`, `Documentation/RelNotes/2.34.0.adoc`, `gpg-interface.c` (github.com/git/git)
- OpenSSH: `ssh-keygen.1` (github.com/openssh/openssh-portable)
- GitHub Docs: *About commit signature verification*; *Telling Git about your signing key* (github.com/github/docs)
- pkg.go.dev: `golang.org/x/crypto/openpgp`
- `ProtonMail/go-crypto`, `FiloSottile/age`, `AGWA/git-crypt`, `spwhitton/git-remote-gcrypt`, `microsoft/go-winio`
- Microsoft Command Line blog: *AF_UNIX comes to Windows* (devblogs.microsoft.com/commandline/af_unix-comes-to-windows/)

**Secundárias [S]**: resultados de busca sobre o named pipe `\\.\pipe\openssh-ssh-agent`
(JGit `org.eclipse.jgit.ssh.apache.agent`, libssh2 `agent.c`, `asyncssh`). Não são a
documentação do dono; confirmar na Fase 0.

**Locais [C]/[T]**: este repositório (`internal/sshkey`, `internal/gitops`,
`internal/engine`, `internal/cli`, `docs/`, `CONTEXT.md`) e os experimentos da seção 3.3.

## Não verificado nesta pesquisa

Windows (qualquer item); macOS (qualquer item); `keepassxc-cli` em execução (não está
instalado); senha por stdin; Argon2id no `gokeepasslib`; versão mínima do git para
`GIT_CONFIG_COUNT`; condições do "Verified" no GitHub; formato do `credential_process`
da AWS; transporte do `keepassxc-proxy`.
