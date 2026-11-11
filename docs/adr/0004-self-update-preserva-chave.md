---
status: accepted
---

# Self-update preserva a chave embutida e exige o sha256 do GitHub

`git-sync update apply` troca o próprio binário pelo último release do GitHub, na mesma estrutura do `taskthing` (`update check` com cache de 12h, `update apply` com download-e-rename). Duas particularidades do git-sync mudaram o desenho.

**A chave SSH vive dentro do binário.** O `add-key` grava a chave privada num slot de 4096 bytes do próprio executável (ver CONTEXT.md, "Chaves SSH"). Trocar o binário por um download limpo apagaria a chave em silêncio, e o próximo `push` do daemon falharia por autenticação, longe de quem rodou o update. Por isso o `apply` lê a chave do binário em execução e a grava no slot do binário novo **antes** da troca (`sshkey.Transplant`). Se o binário novo não tiver o slot, o update é recusado, em vez de instalar uma versão sem a chave.

**O binário roda sem supervisão e tem a chave.** Um binário adulterado receberia a chave no primeiro `run`. O `apply` só instala se o sha256 do download conferir com o `digest` que a API do GitHub publica para cada asset; asset sem `digest` é recusado. Isso protege contra download corrompido ou trocado no caminho, não contra um release malicioso publicado na própria conta (para isso seria preciso assinar os releases, fora de escopo por ora).

Outras decisões:

- **Sem update automático no daemon.** O processo em background não se atualiza sozinho: o update é sempre um comando explícito, e os daemons continuam na versão antiga até reiniciarem (o comando diz como). Atualizar sem ninguém olhando um processo que faz `push --force` foi considerado arriscado demais para o ganho.
- **Build `dev` não se atualiza.** Um binário compilado localmente não tem versão comparável e não é sobrescrito por um release.
- **Windows:** um `.exe` em execução não pode ser sobrescrito, mas pode ser renomeado. O atual vai para `.old`, o novo assume o nome, e o `.old` é apagado quando possível (no próximo `apply`, se um daemon ainda o estiver usando).
- **Repositório privado:** `GITHUB_TOKEN`/`GH_TOKEN` autentica as chamadas; o token é removido do header em qualquer redirect para outro host (o download do asset redireciona para o storage do GitHub).
