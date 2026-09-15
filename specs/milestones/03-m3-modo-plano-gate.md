# M3 — Modo plano + gate de aprovação

**Status**: ✅ concluído
**Depende de**: M2
**Sessão de referência**: `docs/session-2026-09-15-m3.md`

## 1. Objetivo

Introduzir o **modo de planejamento** e o **gate de aprovação humana** por tool (SPEC §6, §5.2
item 3, §5.6.6): nada mutante executa sem aprovação explícita.

## 2. Escopo / entregáveis

- [x] Política de aprovação por tool: `allow | ask | deny` (default `ask`; read-only `allow`;
      `write_file` `ask`; `shell` `ask`).
- [x] Fonte de decisão injetável (função `Approver`) para testes e REPL interativo.
- [x] `toolbox_mode: plan` (default): remove do registro do modelo todas as tools mutáveis —
      só read-only expostas; system prompt instrui "produza um plano, não execute mudanças".
- [x] Fluxo do plano: operador vê o plano; `y` → re-treina turno com toolbox completo; `n` →
      descarta; feedback opcional ao modelo.
- [x] `--run`: fluxo explícito de execução direta (requer config explícita de relaxamento).

## 3. Contratos

- `Approver` define para cada `ToolCall` pipeline: imprimir comando real → ler decisão
  (y/n/deny/allow-always) → retornar autorização; tempo de decisão.
- `Toolbox` do loop passa a filtrar por categoria mutável/read-only + política.

## 4. Requisitos de segurança deste milestone

- [x] Em `plan`, tools mutáveis **nunca** aparecem no payload `tools` e nenhuma execução
      mutante ocorre (gate rígido).
- [x] Sem `y` explícito, tool mutável **nunca** executa (interface de approver processo de
      tomada de decisão teste).
- [x] Confirmar: o comando visto pelo operador é o **exato** vetor que será executado (sem
      race entre a UI e a execução).
- [x] `approval.deny` não pode ser sobreescrito por nenhum prompt/modelo.

## 5. Testes obrigatórios

### Funcionais

- [x] Approver `ask`: y executa, n nega, tempo configurável.
- [x] `toolbox_mode=plan`: payload não contém tools mutáveis; read-only presentes.
- [x] Fluxo `-run` sobrescreve plan apenas com config explícita.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [x] Em plan, plano nunca provoca mutação (fake provider tenta `write_file` → tool não
      registrada → rejeitada).
- [x] Sem `y` → tool mutável recusada e registrado na auditoria/loop (nada executado).
- [x] `deny` no approver → recusa mesmo com `approval.default=allow` (ordem: política > UI).
- [x] Mudar cache/estado entre aprovação e execução não altera o que o usuário aprovou
      (congelamento do vetor aprovado).

## 6. Critérios de aceite (DoD)

- [x] **Todos os testes do projeto passam** (`go test -count=1 ./...`).
- [x] `gofmt`/`go vet`/`go build` limpos.
- [x] `docs/DECISOES.md` atualizado.
- [x] Nota de sessão criada.
- [x] Status deste arquivo e de `docs/SPEC.md` atualizados.
- [x] Aceite funcional SPEC §12 M3: "`--plan` nunca executa mutações; plano só roda após `y`".