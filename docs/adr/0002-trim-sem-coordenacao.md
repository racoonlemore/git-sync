---
status: accepted
---

# Trim de histórico sem coordenação entre máquinas

Para manter o repositório pequeno (arquivos binários fazem o histórico crescer rápido), git-sync limita a branch a N=20 commits: ao criar o commit N+1, qualquer máquina reescreve o histórico local (squash/orphan) mantendo só os 20 mais recentes e faz `push --force`. Como várias máquinas podem estar sincronizando o mesmo repositório ao mesmo tempo, isso cria risco de disputa: duas máquinas podem tentar trimmar e forçar push simultaneamente, com resultados divergentes.

Decidimos não adicionar nenhuma coordenação entre nós (sem líder, sem lock distribuído) para resolver essa disputa. Em vez disso, generalizamos a política do [ADR-0001](./0001-remote-wins-conflicts.md) — "o remoto sempre vence" — para cobrir também rejeição de `push` (não só falha de `pull`): se o `push` (incluindo o `push --force` do trim) for rejeitado por divergência, a máquina simplesmente faz `fetch` + `reset --hard` para o remoto e descarta seu ciclo, sem tentar reconciliar. Isso resolve a disputa de forma auto-curativa, com o mesmo mecanismo que já existia para conflitos de conteúdo, sem exigir nenhuma infraestrutura de coordenação adicional.
