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
- **M8 concluído (interação prompt, branch `m8-interacao-prompt`)**: domínio stdlib
  (D-2026-09-17-42..47): observer de eventos transport-agnóstico (`SetObserver`, `Phase`
  append-only, `Event` JSON estável `phase/tool/detail/dur/at`, `detail` redigido no emit,
  `Phase.Label()` = vocabulário D-35); `provider.ModelController` (`Model/SetModel/BaseURL/
  SetBaseURL`, mutex, fail-closed — blank recusado, `SetBaseURL` só com env já setada e http só
  loopback); `config.ProviderDefault`/`ProviderNames`; histórico de prompts redigido **no Add**
  (cap 20, `PromptList`/`PromptAt`, `!N` re-envia redigido); `ContextUsage` reaproveitando o
  reducer (budget 0 → off; pct cap 100); `SwitchModel`/`SwitchProvider` por sessão sem persistir
  no YAML. Entrega REPL stdlib: parser puro `commands.go` (`/exit /quit /reset /help /historico
  !N /retry /continue /model /provider /context`), editor de linha puro + raw-mode **linux+TTY**
  (`raw_linux.go` termios com **ISIG mantido** — Ctrl+C = SIGINT cancela turno em voo /
  `os.Exit(130)` no prompt; `raw_other.go` cooked), gate humano via `confirmRaw(ctx)`, observer
  → `Phase.Label()` no REPL, versão `0.8.0-m8`. Host: 16/16 (38 novos testes) 0 falhas; WSL
  `-race` 16/16; `gofmt` LF-clean. Ver `docs/session-2026-09-17-m8.md`. **Smoke Inspiron
  concluído** (raw/pty real: echo do editor com cursor exato, status `aguardando`→`recebendo
  tokens`→resposta NIM, histórico ↑↑ + `!N` com inválido fail-closed `erro: sem prompt !9`,
  Ctrl+C cancel em voo → `turno cancelado (Ctrl+C)` e loop vivo após, `/context`/`/model`/
  `/provider` em raw, `/exit` limpo). Defeito do cursor no emit pty corrigido + teste de
  regressão (D-2026-09-17-48, `cf57fa9`). Ver `docs/session-2026-09-17-inspiron-m8.md`.
- **M9 concluído (TUI, branch `m9-tui`)** (D-2026-09-17-49): Bubble Tea **v1.3.10 pinado,
  delivery-only** em `cmd/ancientai` (flag `--tui`, TTY-only: stdin+stdout char device; recusa
  pipe/`--run`; versão `0.9.0-m9`). `tui_panels.go` stdlib-puro (ctxBar 5 células, buildFooter
  2 linhas `>80% → !`, activityFromEvent re-reda, readAuditTail, buildView, canUseTUI);
  `tui_gate.go` = `loop.Approver` com diálogo único serializado + veto `d` + fail-closed em ctx
  cancelado; `tui.go` = Model/Update/View (Enter submit, `r`/`c` retry/clear, ↑/↓, y/n/d, C-c
  cancela/sai, `/...` via parser compartilhado, View re-redige sessão); `runTUI` wiring observer
  → eventos + bridge do gate. REPL default e formatos de pipe/`--run` inalterados; núcleo
  `internal/*` continua stdlib (enforcement `TestLoopMustNotImportCmdOrUI`). Host 16/16, WSL
  `-race` 16/16, vet/build `GOAMD64=v1` limpos. Ver `docs/session-2026-09-18-m9-tui.md`.
- **M9 pós-commit (validação no Inspiron)**: smoke real como `ancientai` com NVIDIA NIM via pty
  Python (`tui_drv.py`) numa sessão SSH: respondendo as queries do terminal Bubble Tea (OSC11 +
  DSR) destrava o modelo de input no pty cru (defeito do cenário de teste descoberto — não do
  produto); Enter = `\r`. Validações: **gate ask `y`** → audit `write_file status=executed
  policy=ask decision=allow`, arquivo `smoke-m9.txt` criado com conteúdo exato, painel de
  atividade mostrou `modelo: aguardando... → recebendo tokens → gate: write_file → tool ok
  0.00s` e footer `erro`→`ok`; **veto `d`** → audit `status=denied decision=denied`, arquivo
  NÃO criado, diálogo `[aprovar] write_file | y aprova, n nega, d nega sempre` renderizado;
  **C-c em voo** → `erro: provider: SSE read: context canceled`, 0 registros de tool no audit,
  arquivo NÃO criado, loop vivo após; **`/provider`** → `base_url: https://integrate.api.
  nvidia.c…` no painel; **`/model`** → modelo NIM ativo; **`/exit`** → saída limpa (restaura
  alt-screen). Guarda de pipe: `echo ola | ancientai --tui` → `erro: --tui exige stdin E stdout
  TTY (pipes/arquivos recusados)` RC=2. Artefatos de teste removidos. Ver
  `docs/session-2026-09-18-inspiron-m9.md`.
- **M10 em andamento (branch `m10-tui-paineis`)** (D-2026-09-30-50): o M9 foi aceito no design
  mas delivery quedó aquém do esperado — faltavam bordas desenhadas, scrollbar e navegação entre
  painéis. `tui_panels.go` ganhou a projeção lazydocker real (**sessão ~60% à esquerda /
  atividade ~40% à direita**, moldura dupla box-drawing com borda vertical compartilhada `┬`/`┴`,
  títulos no topo): `renderPanels`, `panelWidths`/`panelInnerWidths` (60/40 com soma exata),
  `viewportFor` (offset clampado), `scrollThumb`+`scrollbarCol` (trilha `░` + thumb `█`
  proporcional; coluna em branco quando não rola), `panelTitle` (marcador `▸` + ciano no foco),
  `promptLine` (cursor in-band reverse video). `tui.go` ganhou `height`/`focus`/`sessionOff`/
  `activityOff`: `Tab`/`Shift+Tab`/`←→` ciclam foco, `↑↓`/PgUp/PgDn/Home/End rolam o painel
  focado, digitar devolve o foco ao prompt, `followSessionBottom` mantém o rodapé. **Sem
  dependência nova** (rejeitado `lipgloss`: borda Unicode + ANSI stdlib bastam). Corrigido no
  caminho o cursor do M9, que acrescentava espaços ao fim e quebrava edição no meio da linha.
  Gate/observer/parser/redação intactos; `tuiGate` segue o único `Approver`. Versão `0.10.0-m10`.
  Host serial 16/16 0 falhas, `gofmt`/`vet` limpos, `GOAMD64=v1` build ok. Ver
  `docs/session-2026-09-30-m10-tui-paineis.md`.
- **Próximo**: fechar M10 — `go test -race` no WSL e smoke no Inspiron (pty real, NIM:
  bordas/scrollbar, `Tab`/setas, gate `y`/`d` auditado, `/comandos`); depois commit
  `feat(M10)`. **Backlog Web** (SSE + shell-HTTP + `Approver` por canal + auth/TLS/CSRF) —
  reusa o contrato M8/M9, sem tocar em `internal/loop`; **agendamento depende do usuário**.

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