# AGENTS.md — Guia para agentes/sessões de desenvolvimento

> Leia **este arquivo** antes de qualquer mudança. Ele orquestra o trabalho em sessões limpas.

## Projeto

**AncientAI** — harness de agente de IA para terminal em hardware **sem AVX** (Dell Inspiron
1545 / Core 2 Duo / 4 GB RAM / Linux server minimal). Cliente fino em **Go**: o modelo roda no
provedor remoto (OpenRouter / NVIDIA NIM / LiteLLM; contrato OpenAI-compatible). Binário único,
`CGO_ENABLED=0`, `GOAMD64=v1` (sem SSE pós-SSE2).

## Onde as coisas vivem

| Artefato | Caminho |
|---|---|
| Especificação técnica (arquitetura, ameaças, segurança) | `docs/SPEC.md` |
| Registro de decisões (ADR) | `docs/DECISOES.md` |
| Notas por sessão | `docs/session-<data>-<slug>.md` |
| Metodologia de trabalho (SDD) | `specs/methodology.md` — **obrigatória** |
| Regras não-negociáveis | `specs/constitution.md` |
| Specs por milestone | `specs/milestones/` |
| Templates | `specs/templates/` |

## Estado atual

- **M1 concluído** (skeleton: config, provider OpenAI-compat + SSE, histórico redigido, loop
  fail-closed, REPL). Ver `docs/session-2026-09-14-m1.md`.
- **M2 concluído** (tool registry, filesystem confinado, tool `shell` com `ask`, loop
  multi-turno com gate e teto, fallback JSON, scan de exfiltração). 105 testes, 0 falhas.
  Ver `docs/session-2026-09-15-m2.md`.
- **M3 concluído** (toolbox por modo plan/run, `Approver` interface com veto, fluxo do plano
  y/n/feedback no REPL, `--run` com allow explícito, `approval.timeout_seconds`).
  112 testes, 0 falhas. Ver `docs/session-2026-09-15-m3.md`.
- **M4 concluído** (sandbox completo: internal/audit JSONL append-only, internal/sandbox
  policy whitelist+hard-block, executor run_user/workdir/timeout, shell refatorado sobre
  sandbox, auditoria no loop com denied/timeout/error, args redigidos no audit).
  Ver `docs/session-2026-09-15-m4.md`.
- **M5 concluído** (docker tool com subcomando whitelist + vetor de args; apt update/install
  com remove/purge negados; toggles docker_tool/apt_tool exigem ask; sessão: `--resume`,
  `--cleanup` explícito, `session_context_bytes` + `Store.Reduce` por turnos; bug M4 latente
  corrigido — timeout default do shell era 30ns). Host: 157 pass / 14 skip, 0 falhas; suíte
  Linux real verde no WSL. Ver `docs/session-2026-09-15-m5.md`.
- **M6 concluído** (agente de codificação §10: `internal/tools/code` com `code_scaffold`
  (templates embed validados, nunca executa `.sh`), `edit_file` (diff + `ask`), `code_review`
  read-only com heurísticas locais e relatório estruturado; filesystem tools enraizadas no
  `code_root` strict-child validado; política `code_scaffold`/`edit_file`=ask; injeção indireta
  etiquetada untrusted). Host: 178 pass / 14 skip, 0 falhas; suíte Linux real verde no WSL
  (incl. `TestM6*`). Ver `docs/session-2026-09-15-m6.md`.
- **M6 pós-commit (validação no Inspiron)**: smoke M1–M6 real no Dell via SSH (session
  `docs/session-2026-09-16-inspiron-m6.md`): config fail-closed, scaffold real com gate,
  review read-only + strict-child, edit_file com aprovação, docker real, apt real (exit 100
  sem root, propagado), `--cleanup`/`--resume`/`--reset`/`--run` verificados. Defeito do
  preview `edit_file` corrigido (diff LCS stdlib, D-2026-09-16-29, teste de regressão).
  Ajustes de implantação: `run_user` exige harness no usuário dedicado `ancientai` ou
  privilégio (D-2026-09-16-30); apt não-root sem efeito, docker ok via grupo
  (D-2026-09-16-31).
- **M6 pós-commit (NVIDIA NIM, usuário dedicado)**: validação real no Inspiron como
  `ancientai` com provider NVIDIA (`nvidia/nemotron-3.5-lightning-30b-a3b`). Provider NIM
  vivo (o slug `:free` 404 nos directs NIM; o `-30b-a3b` responde); cold start passou de
  2 min → timeout provider 5 min (D-2026-09-16-32); write/shell **com efeito** executados e
  auditados (`ask→allow→executed`, owner `ancientai`, guarda de chaining M2 negou `;` e o
  modelo decompôs). Defeito de re-root de caminho absoluto em `write_file` corrigido —
  tools de arquivo recusam absoluto, fail-closed (D-2026-09-16-33). Pareamento:
  nemotron/NIM → `toolbox_mode: run` com ask (D-2026-09-16-34); `plan` para modelos que
  planejam (validado em M6/OpenRouter). Ver
  `docs/session-2026-09-16-inspiron-nvidia.md`.
- **Pós-commit (indicadores de progresso no REPL)**: gancho `Loop.SetStatus` + render TTY-only
  no REPL (`isCharDevice`): fases `modelo: aguardando o primeiro token...` →
  `modelo: recebendo tokens...` (SSE onEvent) → `gate humano: aguardando sua aprovacao...`,
  com limpeza `""` (D-2026-09-16-35). Teto de turno alinhado ao provider (5 min). Tests:
  `TestStatusHookStreamsPhases`/`TestStatusHookReportsHumanGate`. Binário Linux `0.6.0-m6-ind`
  (`GOAMD64=v1`) em `/home/ancientai/ancientai-0.6.0-m6-ind` no Inspiron (o `m6` estava em
  uso por sessão aberta). Ver `docs/session-2026-09-16-indicadores.md`.
- **M7 concluído (Skills, §11.1)** — branch `m7-skills` (D-2026-09-16-38..41): pacote
  `internal/skills` (frontmatter YAML estrito KnownFields, `name`==dir, limites 256 KiB/64 KiB,
  nome `^[a-z0-9]+(?:-[a-z0-9]+)*$`, scripts strict-child, `--verify-skill` com stamp
  `.ancientai-verified` + `origin_hash`); tool `skill_run` via `sandbox.ExecVector` (vetor puro,
  sem shell; recusa `run_user`≠atual/root; ask; argv ≤64/16 KiB); loop: `SetSkills`+`SetSkill`
  (allowed_tools validado no registry, fail-closed), `enabledTools` **intersect** com a união
  (skill restringe, nunca concede), `policyFor(skill_run)=ask`, injeção no system prompt com
  marcadores untrusted; config `skills_dir` (default `~/.config/ancientai/skills`) +
  `tool_timeouts.skill_run` (60s). Binário `0.7.0-m7-ind` (`dist/ancientai`, GOAMD64=v1).
  Host serial 16/16 e Linux WSL `-race` 16/16; 5 testes de segurança do milestone obrigatórios.
  Ver `docs/session-2026-09-16-m7.md`.
- **M7 pós-commit (validação no Inspiron)**: smoke real como `ancientai` com NVIDIA NIM
  (`docs/session-2026-09-16-inspiron-m7.md`): skill `smoke` com `origin` + `scripts/status.sh`;
  load sem stamp recusado; `--verify-skill` grava `.ancientai-verified`; tamper no SKILL.md →
  carga recusada até re-verify; `skill_run` em sessão NIM real com ask→allow→executed (93 ms,
  efeito gravado, audit `status=executed policy=ask`); `--run` recusa skill scriptada. Binário
  `0.7.0-m7-ind` em `/home/ancientai/ancientai-0.7.0-m7-ind`.
- **Próximo**: M7 fechado e push de `m7-skills` feito → backlog TUI **avaliado e convertido em specs**
  M8 (`specs/milestones/08-m8-interacao-prompt.md`, domínio + REPL, stdlib) e M9
  (`specs/milestones/09-m9-tui.md`, TUI Bubble Tea como frontend delivery sobre o mesmo core).
  Iniciar **M8** (o gate do M7 — validação Inspiron — foi cumprido), exceto se o usuário decidir
  priorizar outra coisa.
- **Backlog documentado (pós-M7) — TUI + recursos de interação prompt/UI**: `--tui` Bubble Tea
  no estilo lazydocker (painel sessão + painel atividade + prompt + footer com modelo/
  provider/ctx%) e paridade com o prompt para: histórico de prompts (↑/↓, ≥10), retry/
  continue pós-falha, troca de modelo/provedor por sessão (`/model`, `/provider`), interromper
  (C-c) + instruir + continuar, e medidor de contexto (`/context`, barra no footer). Decisões:
  D-2026-09-16-36 (Bubble Tea só na camada de render/input; núcleo segue stdlib) e
  D-2026-09-16-37 (paridade prompt↔UI; troca de provider por sessão, sem persistir no YAML;
  interrupção não executa tool após cancelamento; `Loop.ContextUsage()` com % vs
  `session_context_bytes`). Contrato completo: `docs/design-tui-prompt-features.md`.
  **Dividido em M8** (domínio + comandos REPL, stdlib, fronteira UI↔core transport-agnóstica) e
  **M9** (TUI Bubble Tea delivery sobre o mesmo core). Web futura (acesso no navegador de outras
  máquinas) = milestone posterior que reusa o contrato (Event JSON + comandos puros + `Approver`
  por canal), sem tocar em `internal/loop`.
- **M8** (agora próximo candidato): iniciar quando o usuário decidir; manter a ordem **M8 → M9**,
  pois a TUI consome o contrato de eventos/comandos que o M8 estabiliza.

## Como trabalhar (resumo da metodologia)

1. Ler esta base + `specs/constitution.md` + `specs/methodology.md` + spec do milestone ativo.
2. Decompor escopo em tasks; registrar decisões em `docs/DECISOES.md`.
3. **TDD**: teste (falha) → implementação → verde; **testes de segurança para tudo que for
   relevante à segurança do milestone** (devem falhar se a proteção for removida).
4. Verificar qualidade antes de fechar:
   - `gofmt -l .` (vazio), `go vet ./...`, `go build ./...`
   - `go test -count=1 ./...` — **todos devem passar**
   - quando houver toolchain C (Linux/CI): `go test -race -count=1 ./...`
5. Documentar: DECISOES + nota de sessão (template `specs/templates/session.md`) + marcar
   checkboxes **somente o que foi feito**.
6. Commit padronizado: `feat(M<N>): <resumo imperativo em minúsculas>`; revisar `git diff`;
   nunca segredos/binários/`.obsidian/`.

## Regras de segurança inegociáveis (resumo)

- **Deny-by-default**: nada executa/altera sem aprovação explícita; config insegura falha na
  validação.
- **Confiança zero** na saída do modelo E de ferramentas; tool mutável só com gate humano.
- **Segredos jamais**: env/secret manager; redigir com `internal/redact` antes de log/persistir/
  enviar ao modelo; nunca citar valores de chave em erros.
- **Fail-closed**: parser estrito, tool desconhecida recusada, loop com teto de iterações.
- **Sem downgrade silencioso**: não desligar verificação TLS, não enfraquecer para passar teste.

## Convenções de código (Go)

- Stdlib + `gopkg.in/yaml.v3` (config) — não adicionar dependência sem justificativa +
  decisão registrada.
- `os/exec` sempre com vetor de args; proibido montar shell a partir de texto do modelo (só a
  tool `shell` deliberada, com `ask`).
- Todo I/O de rede e execução com `context`/deadline; leituras sempre limitadas.
- Path traversal: `filepath.Clean` + verificação de prefixo em toda escrita/leitura.

## Comandos úteis

```sh
go test -count=1 ./...                      # suíte completa (gate)
go vet ./...                                # estático
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags="-s -w" -o dist/ancientai ./cmd/ancientai
```