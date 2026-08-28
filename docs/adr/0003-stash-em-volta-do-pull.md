---
status: accepted
---

# Stash em volta do pull para ser agnóstico à versão do git

O ciclo de sincronização é *pull-first* (ver spec, história 15): ao expirar o debounce, o git-sync incorpora o que o remoto tem **antes** de commitar as mudanças locais. Na prática isso significava chamar `git pull` com a working tree suja — havia edições locais ainda não commitadas nos arquivos binários enquanto o pull rodava.

O problema é que o comportamento do `git pull` sobre uma working tree suja **não é estável entre versões do git**. Em versões mais antigas (ex.: 2.43), o pull atualiza apenas os arquivos que o remoto mudou e deixa intactos os arquivos sujos que ele não precisa tocar — então duas máquinas editando arquivos **diferentes** (ex.: duas bases KeePassXC) convergiam sem atrito. Em versões mais novas, o `git pull` passou a tentar fast-forwardar a working tree inteira e **recusa com `fatal: Cannot fast-forward your working tree` (exit 128) se qualquer arquivo estiver sujo** — mesmo que o remoto tenha mexido em outro arquivo. Resultado: atualizar o git quebrou um fluxo que antes funcionava, de forma silenciosa e dependente de ambiente, num pool de máquinas Windows e Linux com versões diferentes.

Decidimos **guardar as mudanças locais com `git stash` antes do pull e reaplicá-las com `git stash pop` depois**, mantendo o pull-first da spec. Assim o `git pull` sempre roda sobre uma working tree limpa e se comporta de forma idêntica em qualquer versão do git. A reaplicação tem dois desfechos, ambos alinhados com o [ADR-0001](./0001-remote-wins-conflicts.md):

- **Arquivos distintos (sem conflito):** o `stash pop` reaplica limpo, a edição local é preservada e segue para commit/push. Nenhum trabalho é perdido.
- **Mesmo arquivo alterado local e remotamente (conflito):** o `stash pop` falha e deixa caminhos não-mesclados; o git-sync detecta isso via `git ls-files --unmerged` (que independe do idioma das mensagens do git), descarta o stash (`git stash drop`) e aplica "o remoto vence" via `fetch` + `reset --hard`. Nenhum commit intermediário é criado e nada do estado local descartado vai ao remoto — exatamente a política do ADR-0001.

O stash é um mecanismo interno e transitório; a política observável (pull-first, remoto como fonte única da verdade, remoto vence em conflito) permanece a mesma. A alternativa considerada — commitar as mudanças locais antes de integrar (*commit-first*) — foi rejeitada porque produz merge commits e históricos divergentes, enfraquecendo a premissa de que o remoto é a única fonte da verdade, além de deixar um commit intermediário no reflog em caso de conflito (contrariando o ADR-0001). A alternativa de simplesmente descartar as mudanças locais antes do pull também foi rejeitada, pois perderia trabalho não-conflitante (arquivos distintos) sem necessidade.

Numa falha de `stash pop` que **não** seja conflito (erro de infraestrutura, sem caminhos não-mesclados), o ciclo é abortado e registrado, e a edição local permanece guardada no stash (recuperável via `git stash list`) em vez de ser silenciosamente descartada.
