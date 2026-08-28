---
status: accepted
---

# Resolução de conflitos: o remoto sempre prevalece

git-sync sincroniza arquivos binários, que não podem ser mesclados linha a linha. Quando há conflito (o mesmo arquivo alterado tanto local quanto remotamente), o programa descarta as mudanças locais conflitantes e assume o estado do remoto integralmente, via `fetch` + `reset --hard`. Nenhum rastro do estado local descartado é preservado — não há commit intermediário nem estratégia de merge que mantenha histórico recuperável.

Essa escolha foi deliberada: dado que arquivos binários tornam qualquer merge automático inviável, e o caso de uso prioriza simplicidade sobre preservação de dados em conflito, o remoto foi definido como fonte da verdade. A alternativa considerada — commitar localmente antes do merge com estratégia `-X theirs`, preservando o estado local no histórico do git — foi rejeitada em favor de um comportamento mais simples e sem rastro.

O conflito é detectado em dois pontos, ambos resolvidos com a mesma política acima: (1) a falha do `git pull` ao integrar o remoto; e (2) a falha do `git stash pop` ao reaplicar as mudanças locais sobre o estado recém-integrado. O ponto (2) existe porque o ciclo guarda as mudanças locais (`stash`) em volta do pull para ser agnóstico à versão do git — ver [ADR-0003](./0003-stash-em-volta-do-pull.md), que detalha o mecanismo e como conflito e não-conflito são distinguidos. Quando as mudanças local e remota tocam arquivos **distintos** não há conflito: o `stash pop` reaplica limpo e ambos os lados são preservados; o descarte só ocorre no conflito real sobre o mesmo arquivo.

Esta política é generalizada para a rejeição de `push` no [ADR-0002](./0002-trim-sem-coordenacao.md).
