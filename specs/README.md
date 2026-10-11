# specs/ — Especificações de Desenvolvimento (SDD)

Este diretório é a fonte única da **metodologia de Spec-Driven Development (SDD)** do
AncientCode e contém as especificações de cada milestone.

## Conteúdo

| Arquivo | Propósito |
|---|---|
| `constitution.md` | Regras não-negociáveis do projeto (segurança, testes, documentação). |
| `methodology.md` | Metodologia de trabalho que **toda sessão** deve seguir. |
| `milestones/` | Uma especificação por milestone (M1..M14). |
| `templates/milestone.md` | Template para abrir uma nova spec de milestone. |
| `templates/session.md` | Template para notas de sessão em `docs/`. |

O desenho (arquitetura, modelo de ameaça, segurança operacional) vive em [docs/SPEC.md](../docs/SPEC.md).
Decisões registradas vivem em [docs/DECISOES.md](../docs/DECISOES.md). Notas por sessão vivem em
`docs/session-<data>-<slug>.md`.

## Estado por milestone

**M1–M14 mergeados em `main`** (M14 no PR #10, `89fdf19`). **M15 implementado e validado
no hardware**, aguardando merge na branch `m15-projeto-rascunho` (PR #11).

| | Milestone | Spec | Nota de sessão |
|---|---|---|---|
| M1 | Skeleton funcional | [`01-m1`](milestones/01-m1-skeleton-funcional.md) | [`m1`](../docs/session-2026-09-14-m1.md) |
| M2 | Tool loop | [`02-m2`](milestones/02-m2-tool-loop.md) | [`m2`](../docs/session-2026-09-15-m2.md) |
| M3 | Modo plano + gate | [`03-m3`](milestones/03-m3-modo-plano-gate.md) | [`m3`](../docs/session-2026-09-15-m3.md) |
| M4 | Sandbox completo | [`04-m4`](milestones/04-m4-sandbox-completo.md) | [`m4`](../docs/session-2026-09-15-m4.md) |
| M5 | Docker/apt + sessão | [`05-m5`](milestones/05-m5-docker-apt-sessao.md) | [`m5`](../docs/session-2026-09-15-m5.md) |
| M6 | Agente de codificação | [`06-m6`](milestones/06-m6-agente-codificacao.md) | [`m6`](../docs/session-2026-09-15-m6.md), [Inspiron m6](../docs/session-2026-09-16-inspiron-m6.md), [NVIDIA NIM](../docs/session-2026-09-16-inspiron-nvidia.md), [indicadores](../docs/session-2026-09-16-indicadores.md) |
| M7 | Skills | [`07-m7`](milestones/07-m7-skills.md) | [`m7`](../docs/session-2026-09-16-m7.md), [Inspiron](../docs/session-2026-09-16-inspiron-m7.md) |
| M8 | Interação prompt | [`08-m8`](milestones/08-m8-interacao-prompt.md) | [`m8`](../docs/session-2026-09-17-m8.md), [Inspiron](../docs/session-2026-09-17-inspiron-m8.md) |
| M9 | TUI | [`09-m9`](milestones/09-m9-tui.md) | [`m9`](../docs/session-2026-09-18-m9-tui.md), [Inspiron](../docs/session-2026-09-18-inspiron-m9.md) |
| M10 | Painéis TUI | [`10-m10`](milestones/10-m10-tui-paineis.md) | [`m10`](../docs/session-2026-09-30-m10-tui-paineis.md) |
| M11 | Gate informado | [`11-m11`](milestones/11-m11-gate-informado.md) | [`m11`](../docs/session-2026-10-01-m11-gate-informado.md) |
| M12 | Catálogo de modelos | [`12-m12`](milestones/12-m12-catalogo-modelos.md) | [`m12`](../docs/session-2026-10-01-m12-catalogo-modelos.md) |
| M13 | Painel de sessão | [`13-m13`](milestones/13-m13-painel-sessao.md) | [`m13`](../docs/session-2026-10-02-m13-painel-sessao.md) |
| M14 | `/connect` + credenciais | [`14-m14`](milestones/14-m14-connect-credenciais.md) | [`m14`](../docs/session-2026-10-02-m14-connect.md) |

`08-m8-agentes-paralelos.md` é um esboço não entregue (sem nota de sessão).

### Em aberto

- **`--check` dentro da TUI.** Hoje a TUI recusa a flag e aponta o REPL.
- **LiteLLM nunca foi medido.** NVIDIA e OpenRouter têm smoke real; LiteLLM ainda não tem
  instância rodando, então a estratégia de verificação foi escolhida por leitura de contrato,
  não por medição.
- **Driver raw do Windows** (`golang.org/x/term`), se a experiência de edição justificar —
  hoje o REPL roda em modo *cooked* fora do Linux e a TUI cobre o caso.
- **Backlog anotado**: `/compact`, `/new`, `/autocompact` (a unidade é a **sessão** — os três criam
  sessão nova, ver `milestones/13-m13-painel-sessao.md` §10); `/sessions` para não perder acesso
  à anterior; backlog Web (SSE + shell-HTTP + `Approver` por canal), cujo agendamento depende
  do usuário.

## Fluxo entre sessões

1. `AGENTS.md` (raiz) carrega convenções + metodologia resumida.
2. Cada sessão abre com: `constitution.md` → `methodology.md` → spec do milestone ativo.
3. Ao fechar o milestone: marcar specs, atualizar esta tabela, gerar nota de sessão, todos os
   testes verdes e commit (ver `methodology.md`).