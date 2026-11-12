---
status: accepted
---

# Trim com folga: limite de 100 commits, mantém 50

Substitui o limite de N=20 do [ADR-0002](./0002-trim-sem-coordenacao.md) e da spec (histórias 21, 22 e 27). A política sem coordenação do ADR-0002 continua valendo.

Com o limite antigo (manter 20, trim ao criar o 21º), a branch ficava sempre cheia depois de atingir o limite, e **todo** sync reescrevia a história inteira. Cada reescrita cria uma raiz nova, então o pull de todas as outras máquinas falhava com "refusing to merge unrelated histories" a cada ciclo. Esse regime permanente expôs um bug de perda de dados (corrigido na v0.0.5, ver ADR-0001) e tem outros custos: autor e data dos commits reescritos passam a ser os da máquina que fez o trim, e uma futura assinatura de commits exigiria refazer todas as assinaturas a cada ciclo.

Ao mesmo tempo, o ganho era pequeno. No caso de uso real (dois bancos KeePass somando ~53 KB, ~10 syncs por mês), o repositório sem trim nenhum cresceria uns 6 MB por ano: um `.kdbx` é cifrado e não deltifica, então cada versão custa o arquivo inteiro, mas o arquivo é pequeno.

Decidimos manter o limite fixo, mas com folga: **ao chegar a 100 commits, o trim descarta os 50 mais antigos e mantém os 50 mais recentes**. A reescrita passa a acontecer uma vez a cada 50 syncs (no ritmo acima, a cada ~5 meses), o histórico guarda mais versões e o repositório continua limitado (no máximo 99 versões).

O valor segue não configurável, como os outros parâmetros do daemon (CONTEXT.md, "sem `config`").

Alternativas consideradas:
- **Limite por tamanho** (trim quando o `.git` passa de um orçamento): mede melhor o que se quer limitar, mas exige medir o repositório a cada ciclo e um orçamento que serve mal tanto para repositórios pequenos quanto para grandes. A contagem com folga resolve o problema prático com uma mudança mínima.
- **Snapshot único** ao passar do limite: mais simples, mas joga fora todas as versões antigas de uma vez.
- **Não reescrever nunca:** o repositório cresceria sem limite, o que contraria o objetivo original do trim.
