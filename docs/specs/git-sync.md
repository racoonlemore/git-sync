---
status: ready-for-agent
---

# git-sync

## Problem Statement

Usuários mantêm pastas de arquivos (incluindo arquivos binários) sincronizadas manualmente via git entre várias máquinas. Isso exige lembrar de rodar `pull`, `add`, `commit` e `push` manualmente toda vez que algo muda, em cada máquina, e resolver conflitos manualmente quando duas máquinas mudaram o mesmo arquivo. É fácil esquecer de sincronizar, perder trabalho por não ter feito `pull` antes de editar, ou deixar o histórico do repositório crescer indefinidamente com arquivos binários pesados.

## Solution

Um binário de linha de comando (`git-sync`) que, uma vez configurado num repositório git já existente, roda em segundo plano naquela máquina, observa mudanças na pasta do repositório e sincroniza automaticamente com o remoto: faz `pull` periodicamente, e a cada mudança local (com debounce) faz `pull` + `add` + `commit` + `push`. Em caso de conflito de conteúdo, o remoto sempre prevalece — o objetivo é manter várias máquinas convergindo para o mesmo estado sem exigir intervenção manual, aceitando que mudanças locais conflitantes podem ser descartadas. O histórico de commits é mantido enxuto (últimos 20) para não deixar o repositório crescer sem limite.

## User Stories

1. Como usuário, eu quero rodar um comando `setup` dentro de um repositório git já existente, para que o git-sync passe a sincronizar aquele repositório automaticamente a partir de então.
2. Como usuário, eu quero que o `setup` falhe com um erro claro se a pasta atual não for um repositório git válido, para que eu não configure o git-sync no lugar errado.
3. Como usuário, eu quero que o `setup` falhe com um erro claro se o repositório não tiver um remote configurado, já que o git-sync depende de um remoto para sincronizar.
4. Como usuário, eu quero que o `setup` falhe com um erro claro se eu não tiver `git config user.name` configurado (nem local nem global), já que essa informação compõe a mensagem de commit.
5. Como usuário, eu quero que rodar `setup` de novo num repositório já configurado não faça nada (seja um no-op), para que eu não corra risco de duplicar processos ou units de autostart.
6. Como usuário, eu quero que o `setup` registre o git-sync para iniciar automaticamente no login da minha conta de usuário (não no boot do sistema), para que ele tenha acesso às minhas credenciais git (chave SSH, credential helper) sem configuração extra.
7. Como usuário Linux, eu quero que o autostart seja feito via uma unit de `systemd --user`, para que se integre nativamente ao meu ambiente de sessão.
8. Como usuário macOS, eu quero que o autostart seja feito via um `LaunchAgent` (não `LaunchDaemon`), para que rode no contexto da minha sessão de usuário.
9. Como usuário Windows, eu quero que o autostart seja feito via uma tarefa do Task Scheduler com gatilho "at log on" rodando com a minha conta, para que rode no contexto do meu usuário (não como SYSTEM).
10. Como usuário, eu quero que a unit/tarefa de autostart tenha um nome determinístico baseado no nome da pasta e um hash do caminho absoluto do repositório, para que múltiplos repositórios sincronizados na mesma máquina não colidam nem se confundam entre si.
11. Como usuário, eu quero rodar um comando `validate` a qualquer momento, para checar se o repositório atual está pronto para o git-sync funcionar (é um repo git válido, tem remote, tem `git config user.name`) e se o git-sync já está instalado (setup já rodado) para ele.
12. Como usuário, eu quero que o `setup` rode as mesmas checagens do `validate` antes de instalar qualquer coisa, para que eu nunca acabe com um serviço instalado sobre um repositório que não atende aos pré-requisitos.
13. Como usuário, eu quero que, uma vez configurado, o programa em segundo plano escute mudanças de arquivos na pasta do repositório continuamente, sem que eu precise interagir com ele.
14. Como usuário, eu quero que cada mudança de arquivo reinicie um temporizador de debounce de 1 minuto, para que várias mudanças em sequência rápida resultem em um único commit, não vários.
15. Como usuário, eu quero que, ao expirar o debounce, o programa tente um `git pull` antes de commitar minhas mudanças, para incorporar o que outras máquinas já sincronizaram.
16. Como usuário, eu quero que, se o `pull` encontrar conflito de conteúdo (arquivo alterado tanto localmente quanto no remoto), o remoto prevaleça — o programa descarta minhas mudanças locais conflitantes via `fetch` + `reset --hard` para o remoto, sem tentar mesclar, já que meus arquivos são binários e não são mesclamos automaticamente.
17. Como usuário, eu aceito que, no cenário de conflito, minhas mudanças locais divergentes são perdidas sem deixar rastro recuperável no histórico do git — essa é uma troca deliberada em favor de simplicidade (ver ADR 0001).
18. Como usuário, eu quero que, quando não há conflito, o programa prossiga automaticamente com `add`, `commit` e `push` das minhas mudanças locais, sem exigir confirmação minha.
19. Como usuário, eu quero que cada commit automático tenha a mensagem no formato `sync dd-mm-yyyy hh:mm (nome do usuário git)`, usando o `git config user.name` local do repositório (com fallback para o global), para saber quando e por qual máquina/usuário cada sincronização foi feita.
20. Como usuário, eu quero que o programa também faça `pull` a cada 1 hora, independentemente de mudanças locais, para que eu receba atualizações de outras máquinas mesmo se eu não estiver editando nada.
21. Como usuário, eu quero que o programa limite o histórico da branch atual a 20 commits, para que o repositório não cresça indefinidamente com o peso de arquivos binários versionados repetidamente.
22. Como usuário, eu quero que, ao ultrapassar o limite (criar o commit 21º), o programa reescreva o histórico local mantendo só os 20 commits mais recentes e faça `push --force`, para aplicar esse limite também no remoto.
23. Como usuário que sincroniza o mesmo repositório em várias máquinas simultaneamente, eu quero que essas máquinas não precisem se coordenar entre si para fazer o trim, para que a arquitetura continue simples e sem pontos únicos de falha.
24. Como usuário nesse cenário multi-máquina, eu aceito que, se o `push` (normal ou o `push --force` do trim) for rejeitado por divergência do remoto, a máquina que perdeu a corrida simplesmente descarta seu ciclo local (`fetch` + `reset --hard` para o remoto) e tenta de novo no próximo ciclo — a mesma política de "o remoto sempre vence" usada para conflitos de conteúdo (ver ADR 0002).
25. Como usuário, eu quero rodar um comando `uninstall` que desfaz tudo o que o `setup` fez naquela máquina (para o processo, remove a unit/tarefa de autostart), para poder parar de sincronizar um repositório quando eu quiser.
26. Como usuário, eu quero que o `uninstall` nunca altere o remote nem o histórico de commits do repositório — o escopo dele é só a instalação local naquela máquina.
27. Como usuário, eu NÃO quero ter que configurar nada (debounce, intervalo de pull, limite de commits) — esses valores são fixos (1 minuto, 1 hora, 20 commits) para que eu não precise pensar nisso.
28. Como mantenedor do projeto, eu quero que o binário seja compilado e publicado para Windows, macOS e Linux via workflow do GitHub Actions, para que os usuários possam baixar o binário certo para seu sistema sem precisar compilar nada.

## Implementation Decisions

- **Escopo de atuação:** git-sync nunca inicializa (`git init`) nem clona repositórios. Ele só atua sobre um repositório git já existente localmente e com remote configurado. Isso é validado antes de qualquer ação (`validate` e, internamente, `setup`).
- **Arquitetura multi-repositório (Opção B, resolvida no domain-modeling):** não há daemon único nem registro central de repositórios. Cada repositório configurado tem sua própria instância de processo em segundo plano e sua própria unit/tarefa de autostart, independentes entre si. "Repositórios registrados" (linguagem original do requisito) significa apenas "repositórios onde `setup` já foi rodado".
- **Autostart como serviço de usuário, sempre:** nunca como serviço de sistema/boot, para ter acesso direto às credenciais git do usuário sem configuração adicional. Implicação: o processo só inicia após o login do usuário, não antes.
  - Linux: unit de `systemd --user`.
  - macOS: `LaunchAgent` (não `LaunchDaemon`) em `~/Library/LaunchAgents`, com `RunAtLoad`.
  - Windows: tarefa no Task Scheduler com gatilho "at log on", rodando com a conta do usuário (não um Serviço do Windows como SYSTEM).
- **Identificação da unit/tarefa:** nome determinístico no formato `nome_da_pasta_(hash do caminho absoluto do repositório)`, igual nas três plataformas. Esse nome é também o critério que `validate` usa para determinar se o git-sync já está instalado para um repositório — não existe marcador separado dentro do repo.
- **Idempotência do `setup`:** se a unit/tarefa correspondente já existe, `setup` é um no-op.
- **Ordem de execução do `setup`:** roda as checagens de `validate` primeiro; aborta sem instalar nada se falharem.
- **Checagens do `validate`:** (1) pasta atual é repositório git existente (tem `.git`); (2) repositório tem remote configurado; (3) existe `git config user.name`, local ou global (fallback nessa ordem); (4) existência da unit/tarefa de autostart para decidir se já está instalado.
- **Fluxo do processo em segundo plano** (por repositório):
  1. Observa mudanças de arquivos na pasta do repositório.
  2. A cada mudança, reinicia um debounce de 1 minuto (fixo, não configurável).
  3. Ao expirar o debounce: tenta `git pull`.
     - Se o pull falhar por conflito de conteúdo (arquivo alterado local e remotamente), o remoto prevalece: `fetch` + `reset --hard` para o remoto; mudanças locais conflitantes são descartadas sem preservação no histórico (ver ADR 0001). O ciclo termina aqui, sem commit/push.
     - Se não houver conflito, segue para `add` + `commit` + `push`.
  4. Mensagem de commit: `sync dd-mm-yyyy hh:mm (nome do usuário git)`, onde o nome vem de `git config user.name` (local, com fallback para global). Ausência de ambos é um erro de pré-condição, já pego no `validate`/`setup` — não deve ocorrer em tempo de execução do daemon.
  5. Independente do debounce, faz `git pull` a cada 1 hora (fixo, não configurável).
  6. Mantém um limite fixo de N=20 commits na branch atual (não configurável). Ao criar o commit 21º, reescreve o histórico local (squash ou branch órfã) mantendo só os 20 mais recentes, e faz `push --force`.
  7. **Política unificada "o remoto sempre vence"** (ADR 0001, generalizada no ADR 0002): aplica-se tanto a falha de `pull` (conflito de conteúdo) quanto a falha/rejeição de `push` (incluindo o `push --force` do trim, quando outra máquina já sincronizou ou já fez trim primeiro). Em ambos os casos: `fetch` + `reset --hard` para o remoto, ciclo é abandonado, sem coordenação entre máquinas.
- **`uninstall`:** remove a unit/tarefa de autostart de usuário do repositório atual e para o processo. Escopo estritamente local — nunca altera o remote nem o histórico de commits do repositório.
- **Sem comando `config`:** os três parâmetros do daemon (debounce de 1 minuto, intervalo de pull de 1 hora, limite de 20 commits) são fixos no binário, não configuráveis pelo usuário.
- **Distribuição:** binário único compilado para Windows, macOS e Linux, publicado via workflow do GitHub Actions no repositório hospedado no GitHub.
- **Seams de teste** (ver Testing Decisions): (1) motor de sincronização (lógica de domínio: debounce, pull/commit/push, resolução de conflito, trim), testável contra repositórios git reais e temporários; (2) instalador de autostart, abstraído atrás de uma interface própria por SO, para não depender de systemd/launchd/Task Scheduler reais em CI.

## Testing Decisions

- Testes devem verificar comportamento externo observável (estado do repositório git após um ciclo de sync, conteúdo do commit gerado, se um `push --force` foi acionado, se a unit de autostart tem o nome/conteúdo esperado) — não detalhes internos de implementação (como a lib de filesystem-watch é chamada internamente, por exemplo).
- **Seam 1 — motor de sincronização:** cobertura principal aqui. Testes criam repositórios git reais e temporários (locais, sem rede — remoto pode ser outro repositório git local no filesystem de teste) e exercitam o ciclo completo: mudança de arquivo → debounce → pull/commit/push; cenário de conflito de conteúdo (remoto vence, `reset --hard`, sem commit); cenário de trim ao ultrapassar 20 commits (histórico reescrito, `push --force`); cenário de push rejeitado por divergência (ex.: simulando uma segunda "máquina" que já sincronizou) resultando em `reset --hard` e abandono do ciclo, sem coordenação. Não há prior art no repo (projeto novo) — este é o padrão a estabelecer para o restante do projeto.
- **Seam 2 — instalador de autostart:** testes por SO verificam que, dado um caminho de repositório, a interface gera o nome de unit/tarefa correto (`nome_da_pasta_(hash)`) e o conteúdo/comando esperado para registrar como serviço de usuário — sem de fato invocar `systemctl`/`launchctl`/`schtasks`. Execução real desses comandos fica fora da cobertura automatizada (verificação manual por plataforma).
- Comandos de CLI (`validate`, `setup`, `uninstall`) são testados através dos dois seams acima (ex: `setup` chamando o seam de autostart com os parâmetros certos após passar nas checagens do seam de validação), não como um seam próprio.

## Out of Scope

- Comando `config` ou qualquer configuração de usuário para debounce, intervalo de pull ou limite de commits — esses valores são fixos.
- Registro central de múltiplos repositórios ou daemon único multi-repo.
- Autostart como serviço de sistema/boot (rodando antes do login do usuário).
- Qualquer resolução de conflito que não seja "remoto sempre vence" (ex: merge manual, prompts interativos de resolução, estratégias configuráveis).
- Preservação/recuperação do estado local descartado em conflitos ou em trims perdidos (é perda de dados deliberada, sem rastro).
- Coordenação entre máquinas (locks distribuídos, eleição de líder) para evitar corridas de trim/push — resolvida via "remoto sempre vence" em vez disso.
- `git init` ou clone de repositórios pelo git-sync.
- Suporte a múltiplas branches simultâneas por repositório (o fluxo descrito assume a branch atual/única sendo sincronizada).

## Further Notes

- Duas decisões de arquitetura foram registradas como ADRs durante a sessão de domain-modeling e devem ser respeitadas pela implementação:
  - [ADR 0001 — Resolução de conflitos: o remoto sempre prevalece](../adr/0001-remote-wins-conflicts.md)
  - [ADR 0002 — Trim de histórico sem coordenação entre máquinas](../adr/0002-trim-sem-coordenacao.md)
- O glossário/domínio do projeto está em [CONTEXT.md](../../CONTEXT.md) na raiz do repositório; deve ser consultado e mantido atualizado conforme a implementação avança e novos termos/decisões surgirem.
- Este projeto não tem tracker de issues configurado no momento (sem `gh`, sem repositório git inicializado) — esta spec foi salva localmente em vez de publicada. Considerar rodar `/setup-matt-pocock-skills` antes de abrir trabalho de implementação, para poder aplicar a label `ready-for-agent` num tracker real.
