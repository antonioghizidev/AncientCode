# specs/ — Especificações de Desenvolvimento (SDD)

Este diretório é a fonte única da **metodologia de Spec-Driven Development (SDD)** do
AncientAI e contém as especificações de cada milestone.

## Conteúdo

| Arquivo | Propósito |
|---|---|
| `constitution.md` | Regras não-negociáveis do projeto (segurança, testes, documentação). |
| `methodology.md` | Metodologia de trabalho que **toda sessão** deve seguir. |
| `milestones/` | Uma especificação por milestone (M1..M8). |
| `templates/milestone.md` | Template para abrir uma nova spec de milestone. |
| `templates/session.md` | Template para notas de sessão em `docs/`. |

O desenho (arquitetura, modelo de ameaça, segurança operacional) vive em [docs/SPEC.md](../docs/SPEC.md).
Decisões registradas vivem em [docs/DECISOES.md](../docs/DECISOES.md). Notas por sessão vivem em
`docs/session-<data>-<slug>.md`.

## Estado atual

- **M1 — Skeleton funcional**: ✅ concluído (implementação e testes; aceite final pendente de
  smoke test no notebook alvo). Ver `milestones/01-m1-skeleton-funcional.md`.
- **M2 — Tool loop**: ✅ concluído em 2026-09-15. Ver `milestones/02-m2-tool-loop.md`.
- **M3 — Modo plano + gate**: ✅ concluído em 2026-09-15. Ver `milestones/03-m3-modo-plano-gate.md`.
- **M4 — Sandbox completo**: ✅ concluído em 2026-09-15. Ver `milestones/04-m4-sandbox-completo.md`.
- **M5 — Docker/apt tools + sessão persistente**: ✅ concluído em 2026-09-15. Ver `milestones/05-m5-docker-apt-sessao.md`.
- **M6 — Agente de codificação**: ✅ concluído em 2026-09-15. Ver `milestones/06-m6-agente-codificacao.md`.
- **Próximo**: validação/tuning real no Inspiron (smoke M1–M6 via SSH); **depois** avaliar
  M7 — Skills (`milestones/07-m7-skills.md`).

## Fluxo entre sessões

1. `AGENTS.md` (raiz) carrega contexto + metodologia resumida.
2. Cada sessão abre com: `constitution.md` → `methodology.md` → spec do milestone ativo.
3. Ao fechar o milestone: marcar specs, atualizar rotas, gerar nota de sessão, todos os testes
   verdes e commit (ver `methodology.md`).