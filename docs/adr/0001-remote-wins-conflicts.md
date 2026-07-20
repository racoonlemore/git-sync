---
status: accepted
---

# Resolução de conflitos: o remoto sempre prevalece

git-sync sincroniza arquivos binários, que não podem ser mesclados linha a linha. Quando o `git pull` falha por conflito (arquivo alterado tanto local quanto remotamente), o programa descarta as mudanças locais conflitantes e assume o estado do remoto integralmente, via `fetch` + `reset --hard`. Nenhum rastro do estado local descartado é preservado — não há commit intermediário nem estratégia de merge que mantenha histórico recuperável.

Essa escolha foi deliberada: dado que arquivos binários tornam qualquer merge automático inviável, e o caso de uso prioriza simplicidade sobre preservação de dados em conflito, o remoto foi definido como fonte da verdade. A alternativa considerada — commitar localmente antes do merge com estratégia `-X theirs`, preservando o estado local no histórico do git — foi rejeitada em favor de um comportamento mais simples e sem rastro.
