# M3 — Modo plano + gate de aprovação

**Status**: ⏳ pendente
**Depende de**: M2
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Introduzir o **modo de planejamento** e o **gate de aprovação humana** por tool (SPEC §6, §5.2
item 3, §5.6.6): nada mutante executa sem aprovação explícita.

## 2. Escopo / entregáveis

- [ ] Política de aprovação por tool: `allow | ask | deny` (default `ask`; read-only `allow`;
      `write_file` `ask`; `shell` `ask`).
- [ ] Fonte de decisão injetável (função `Approver`) para testes e REPL interativo.
- [ ] `toolbox_mode: plan` (default): remove do registro do modelo todas as tools mutáveis —
      só read-only expostas; system prompt instrui "produza um plano, não execute mudanças".
- [ ] Fluxo do plano: operador vê o plano; `y` → re-treina turno com toolbox completo; `n` →
      descarta; feedback opcional ao modelo.
- [ ] `--run`: fluxo explícito de execução direta (requer cricket config de relaxamento).

## 3. Contratos

- `Approver` define para cada `ToolCall` pipeline: imprimir comando real → ler decisão
  (y/n/deny/allow-always) → retornar autorização; tempo de decisão.
- `Toolbox` do loop passa a filtrar por categoria mutável/read-only + política.

## 4. Requisitos de segurança deste milestone

- [ ] Em `plan`, tools mutáveis **nunca** aparecem no payload `tools` e nenhuma execução
      mutante ocorre (gate rígido).
- [ ] Sem `y` explícito, tool mutável **nunca** executa (interface de approver processo de
      tomada de decisão teste).
- [ ] Confirmar: o comando visto pelo operador é o **exato** vetor que será executado (sem
      race entre a UI e a execução).
- [ ] `approval.deny` não pode ser sobreescrito por nenhum prompt/modelo.

## 5. Testes obrigatórios

### Funcionais

- [ ] Approver `ask`: y executa, n nega, tempo configurável.
- [ ] `toolbox_mode=plan`: payload não contém tools mutáveis; read-only presentes.
- [ ] Fluxo `-run` sobrescreve plan apenas com config explícita.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [ ] Em plan, plano nunca provoca mutação (fake provider tenta `write_file` → tool não
      registrada → rejeitada).
- [ ] Sem `y` → tool mutável recusada e registrado na auditoria/loop (nada executado).
- [ ] `deny` no approver → recusa mesmo com `approval.default=allow` (ordem: política > UI).
- [ ] Mudar cache/estado entre aprovação e execução não altera o que o usuário aprovou
      (congelamento do vetor aprovado).

## 6. Critérios de aceite (DoD)

- [ ] **Todos os testes do projeto passam** (`go test -count=1 ./...`).
- [ ] `gofmt`/`go vet`/`go build` limpos.
- [ ] `docs/DECISOES.md` atualizado.
- [ ] Nota de sessão criada.
- [ ] Status deste arquivo e de `docs/SPEC.md` atualizados.
- [ ] Aceite funcional SPEC §12 M3: "`--plan` nunca executa mutações; plano só roda após `y`".