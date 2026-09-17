# Sessão 2026-09-16 — inspiron m7 (smoke)

> Validação real do M7 (Skills) no Inspiron como usuário dedicado `ancientai`,
> provider NVIDIA NIM. Metodologia: `specs/methodology.md`.

## Objetivo

Smoke do milestone M7 no hardware-alvo: skill com script → `--verify-skill` → `skill_run` com
ask→allow→executed (efeito real + auditoria), tamper fail-closed, e `--run` recusando skill
scriptada.

## O que foi feito

- Binário `0.7.0-m7-ind` (dist/ancientai, GOAMD64=v1) instalado em
  `/home/ancientai/ancientai-0.7.0-m7-ind`; `chmod +x` após scp (o scp entrega permissão 664).
- Skill `smoke` instalada em `~/.config/ancientai/skills/smoke` com `origin` (simulando
  skill baixada) + `scripts/status.sh` que grava um marcador em `/home/ancientai/skill-smoke-marker.txt`.
- Validação em 4 blocos:

1. **Load antes da verificação → recusado (fail-closed)**: `--skill smoke` sem stamp imprime
   "nao foi verificada ou foi alterada apos a verificacao" e não entra no loop.
2. **`--verify-skill`**: escreve `.ancientai-verified` (0600) com `skill_sha256` +
   `script_sha256` de cada script; idempotente; **tamper no SKILL.md** (append de linha) → nova
   carga recusada; re-verify restaura.
3. **`--run` + skill scriptada → recusado na inicialização** (sem chamar a NIM).
4. **Sessão NIM real (nemotron, toolbox run)**: o modelo chamou
   `skill_run {"skill":"smoke","script":"status.sh","argv":[]}`; o gate humano mostrou o resumo,
   `y` → **executed em 93 ms**; marcador escrito; audit:
   `{"tool":"skill_run",...,"status":"executed","policy":"ask","decision":"allow","duration":"93.584865ms"}`.

## Decisões

- Nenhuma decisão nova nesta sessão (valida as D-2026-09-16-38..41).

## Testes

- N/A (smoke real em hardware; suíte host/Linux já verde — ver `docs/session-2026-09-16-m7.md`).

## Critérios de aceite

| Critério | Status |
|---|---|
| Skill baixada sem verify recusada no primeiro uso | ✅ |
| `--verify-skill` + re-verify após tamper | ✅ |
| skill_run ask → allow → executed com efeito e auditoria | ✅ |
| Script limitado a scripts/ da skill (vetor, sem shell) | ✅ (via gate, audit, 93 ms) |
| `--run` recusa skill scriptada | ✅ |

## Próximos passos

1. Push do branch `m7-skills` para o GitHub (nada na `main`).
2. M7 fechado → avaliar backlog TUI (D-36/D-37) com decisões D-36/D-37.

## Observações / riscos

- Smoke anterior (um "FAIL: 1") foi falso negativo do próprio script de teste (`nao` vs `não` na
  comparação da mensagem); comportamento confirmado correto.
- Sessões de teste removidas via `rm` (só `default.jsonl` e `smoke-m7.jsonl` restam).
- O binário `m6` antigo não foi tocado (sessão aberta do usuário).