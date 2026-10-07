# AGENTS.md — Guia para agentes/sessões de desenvolvimento

> Leia **este arquivo** antes de qualquer mudança. Ele orquestra o trabalho em sessões limpas.

## Projeto

**AncientCode** — harness de agente de IA para terminal em hardware **sem AVX** (Dell Inspiron
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
  Ajustes de implantação: `run_user` exige harness no usuário dedicado `ancientcode` ou
  privilégio (D-2026-09-16-30); apt não-root sem efeito, docker ok via grupo
  (D-2026-09-16-31).
- **M6 pós-commit (NVIDIA NIM, usuário dedicado)**: validação real no Inspiron como
  `ancientcode` com provider NVIDIA (`nvidia/nemotron-3.5-lightning-30b-a3b`). Provider NIM
  vivo (o slug `:free` 404 nos directs NIM; o `-30b-a3b` responde); cold start passou de
  2 min → timeout provider 5 min (D-2026-09-16-32); write/shell **com efeito** executados e
  auditados (`ask→allow→executed`, owner `ancientcode`, guarda de chaining M2 negou `;` e o
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
  (`GOAMD64=v1`) em `/home/ancientcode/ancientcode-0.6.0-m6-ind` no Inspiron (o `m6` estava em
  uso por sessão aberta). Ver `docs/session-2026-09-16-indicadores.md`.
- **M7 concluído (Skills, §11.1)** — branch `m7-skills` (D-2026-09-16-38..41): pacote
  `internal/skills` (frontmatter YAML estrito KnownFields, `name`==dir, limites 256 KiB/64 KiB,
  nome `^[a-z0-9]+(?:-[a-z0-9]+)*$`, scripts strict-child, `--verify-skill` com stamp
  `.ancientcode-verified` + `origin_hash`); tool `skill_run` via `sandbox.ExecVector` (vetor puro,
  sem shell; recusa `run_user`≠atual/root; ask; argv ≤64/16 KiB); loop: `SetSkills`+`SetSkill`
  (allowed_tools validado no registry, fail-closed), `enabledTools` **intersect** com a união
  (skill restringe, nunca concede), `policyFor(skill_run)=ask`, injeção no system prompt com
  marcadores untrusted; config `skills_dir` (default `~/.config/ancientcode/skills`) +
  `tool_timeouts.skill_run` (60s). Binário `0.7.0-m7-ind` (`dist/ancientcode`, GOAMD64=v1).
  Host serial 16/16 e Linux WSL `-race` 16/16; 5 testes de segurança do milestone obrigatórios.
  Ver `docs/session-2026-09-16-m7.md`.
- **M7 pós-commit (validação no Inspiron)**: smoke real como `ancientcode` com NVIDIA NIM
  (`docs/session-2026-09-16-inspiron-m7.md`): skill `smoke` com `origin` + `scripts/status.sh`;
  load sem stamp recusado; `--verify-skill` grava `.ancientcode-verified`; tamper no SKILL.md →
  carga recusada até re-verify; `skill_run` em sessão NIM real com ask→allow→executed (93 ms,
  efeito gravado, audit `status=executed policy=ask`); `--run` recusa skill scriptada. Binário
  `0.7.0-m7-ind` em `/home/ancientcode/ancientcode-0.7.0-m7-ind`.
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
  delivery-only** em `cmd/ancientcode` (flag `--tui`, TTY-only: stdin+stdout char device; recusa
  pipe/`--run`; versão `0.9.0-m9`). `tui_panels.go` stdlib-puro (ctxBar 5 células, buildFooter
  2 linhas `>80% → !`, activityFromEvent re-reda, readAuditTail, buildView, canUseTUI);
  `tui_gate.go` = `loop.Approver` com diálogo único serializado + veto `d` + fail-closed em ctx
  cancelado; `tui.go` = Model/Update/View (Enter submit, `r`/`c` retry/clear, ↑/↓, y/n/d, C-c
  cancela/sai, `/...` via parser compartilhado, View re-redige sessão); `runTUI` wiring observer
  → eventos + bridge do gate. REPL default e formatos de pipe/`--run` inalterados; núcleo
  `internal/*` continua stdlib (enforcement `TestLoopMustNotImportCmdOrUI`). Host 16/16, WSL
  `-race` 16/16, vet/build `GOAMD64=v1` limpos. Ver `docs/session-2026-09-18-m9-tui.md`.
- **M9 pós-commit (validação no Inspiron)**: smoke real como `ancientcode` com NVIDIA NIM via pty
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
  alt-screen). Guarda de pipe: `echo ola | ancientcode --tui` → `erro: --tui exige stdin E stdout
  TTY (pipes/arquivos recusados)` RC=2. Artefatos de teste removidos. Ver
  `docs/session-2026-09-18-inspiron-m9.md`.
- **M10 concluído (branch `m10-tui-paineis`)** (D-2026-09-30-50): o M9 foi aceito no design
  mas delivery ficou aquém do esperado — faltavam bordas desenhadas, scrollbar e navegação entre
  painéis. `tui_panels.go` ganhou a projeção lazydocker real (**sessão ~60% à esquerda /
  atividade ~40% à direita**, moldura dupla box-drawing com borda vertical compartilhada `┬`/`┴`,
  títulos no topo): `renderPanels`, `panelWidths`/`panelInnerWidths` (60/40 com soma exata),
  `viewportFor` (offset clampado), `scrollThumb`+`scrollbarCol` (trilha `░` + thumb `█`
  proporcional; coluna em branco quando não rola), `panelTitle` (marcador `▸` + ciano no foco),
  `promptLine` (cursor in-band reverse video). `tui.go` ganhou `height`/`focus`/`sessionOff`/
  `activityOff`: `Tab`/`Shift+Tab`/`←→` ciclam foco, `↑↓`/PgUp/PgDown/Home/End rolam o painel
  focado, digitar devolve o foco ao prompt, `followBottom` mantém o rodapé (usa o total
  **anterior** ao append — senão nunca detecta "estava no fim"). **Sem dependência nova**
  (rejeitado `lipgloss`: borda Unicode + ANSI stdlib bastam). Corrigido no caminho o cursor do
  M9, que acrescentava espaços ao fim e quebrava edição no meio da linha. Gate/observer/parser/
  redação intactos; `tuiGate` segue o único `Approver`. Versão `0.10.0-m10`. Host serial 16/16,
  WSL `-race` 16/16 (Go 1.27 instalado no WSL), `gofmt`/`vet` limpos, `GOAMD64=v1` build ok.
  **Smoke Inspiron** (pty real + NIM, binário `/home/ancientcode/ancientcode-0.10.0-m10`): moldura
  completa, scrollbar (verificada com terminal de 11 linhas), marcador `▸`, rodapé; gate `y` →
  audit `write_file executed policy=ask decision=allow` + arquivo criado; gate `d` → `denied` +
  arquivo não criado. Artefatos removidos. Ver `docs/session-2026-09-30-m10-tui-paineis.md`.
- **M11 concluído (branch `m11-gate-informado`)** (D-2026-10-01-51/52): uso real do M10 mostrou que
  a UI não era auditável nem diagnosticável. (1) **Gate informado**: o `Summary` já chegava ao
  `Approver` (`summaryFor` = tool + args redigidos) e a TUI o descartava — aprovar sem ver o payload
  não é consentimento informado. Agora `gateSummaryFor(tool,args)` lê a chave decisiva do schema
  **real** de cada tool (`shell.cmd`, `docker.subcommand`+`args`, `apt.action`+`packages`, `*.path`,
  `code_scaffold.template`+`name`, `skill_run.skill`+`script`+`argv`), com fallback por chaves
  conhecidas e args crus quando nada é reconhecível; `Event{PhaseGateWaiting}` carrega `Detail`
  redigido na origem e a atividade mostra `* gate: shell  id -un`. (2) **Erro visível**: o banner
  existia mas `buildView` **nunca o renderizava** (7 escritas, 0 leituras) — agora é linha em
  vermelho acima do input **e** entrada `! erro: …` no painel de sessão. (3) **Sanitize no paint**:
  `sanitize` remove C0/DEL/C1 (inclui ESC) de todo conteúdo não confiável — sem isso `\x1b[2J` no
  retorno de uma tool podia apagar/falsificar o gate, e `redact` só mascara segredos.
  `fixedRows` unifica o orçamento de linhas entre projeção e navegação. `shell` continua sendo o
  único `Approver`. Versão `0.11.0-m11`. Host 16/16, WSL `-race` 16/16, `gofmt` (blob)/`vet`
  limpos, `GOAMD64=v1` ok. **Smoke Inspiron**: gate `shell` exibindo `id -un`, gate `write_file`
  `y`→`executed policy=ask decision=allow` + arquivo criado, `n`→negado. Ver
  `docs/session-2026-10-01-m11-gate-informado.md`.
- **RESSALVA (D-2026-10-01-52, retratado)**: uma versão anterior desta lista afirmava que a tool
  `shell` estava inutilizável (schema `cmd` vs struct `json:"n"`). **Era falso** — vi a saída
  corrompida do `rg -r` (flag de *replace*) e a tratei como conteúdo do arquivo. Schema, struct e
  mensagem de erro usam `cmd` consistentemente. Nenhum trabalho pendente daí; não levar adiante.
- **M12 concluído e mergeado** (`efeee64`, PR #5; branch `m12-catalogo-modelos`) (D-2026-10-01-53/54/55): faltava **listar os
  modelos do provedor e escolher um**. Medido no Inspiron: NIM 81 modelos devolvendo só
  `id`/`object`/`owned_by`; OpenRouter 464 com `architecture`/`pricing`/`supported_parameters` —
  logo a mesma filtragem tem fidelidades diferentes. **Três camadas** (D-53): (1) grátis, exclusão
  de famílias não-chat por nome; (2) grátis quando existe, metadados do provedor; (3) sob demanda,
  `--check` com request real de tool calling (pede confirmação, gasta tokens). `ToolSupport` é
  **tri-estado**: ausência de metadado vira `?`, nunca "sim" — evita oferecer modelo sem tool
  calling em silêncio. `--free` só onde há preço: **OpenRouter sim, NIM não** (a UI declara a
  indisponibilidade). Camadas: `internal/provider` (`ListModels`, leitura limitada, chave nunca em
  erro) + `internal/catalog` (predicados puros) + `internal/loop` (`ListModels`/`SelectModel`,
  fail-closed). REPL: `/models [--free|--all|--check|texto]` numerado + `/model <N>`. TUI: `/models`
  abre **painel dedicado** (digitar filtra, `↑↓` move, `Enter` seleciona, `Esc` cancela, `Ctrl+F`
  alterna gratuitos). **Defeitos achados no smoke e corrigidos**: (a) **deadlock que travava a TUI**
  — `SwitchModel` dentro de `Update` → `emit` → observer → `tea.Program.Send` bloqueia a event loop
  (D-54; `/model` e `/provider` tinham o bug latente desde o M8/M9; agora tudo via `tea.Cmd`); (b)
  comando não limpava a linha de input (`> /modelsoi`, D-55, bug do M8/M9); (c) painel substituído
  herdava offset de rolagem; (d) "nenhum modelo corresponde" mentindo durante o fetch. Versão
  `0.12.0-m12`. Host 17/17, WSL `-race` 17/17, `gofmt`/`vet` limpos, `GOAMD64=v1` ok. Smoke Inspiron:
  REPL e TUI listam/filtram/selecionam e um turno real responde com o modelo escolhido. Ver
  `docs/session-2026-10-01-m12-catalogo-modelos.md`.
- **M13 concluído** (branch `m13-painel-sessao`) (D-2026-10-02-56..61): o painel de sessão
  cortava prompt/resposta. **Causa dupla**: `runeCap` no paint (`tui_panels.go:553`) e
  `truncatedLine`/`lineCap=90` **no append** (`tui.go:647`) — o texto morria antes de qualquer
  projeção, então corrigir só o desenho não recuperava nada. Entregue: **`panelLines`** (projeção
  única compartilhada por sessão/atividade/picker/gate, quebra por caractere + continuação
  indentada), remoção do corte no append, **offset em linhas físicas** (com offset lógico uma
  resposta de 40 linhas contava como uma e `↓` pulava a resposta inteira), geometria **70/30** com
  **piso de 28 células** para a atividade (o `min=12` virava pilha de `↳`), rolagem horizontal
  (`Shift+←/→`) com indicador `col N/M`, **spinner Braille** via `tea.Tick` só enquanto `inTurn`
  (degradação ASCII; spinner parado seria pior que nenhum), **`Esc` cancela** e **devolve o prompt**
  ao input, `/continue` reenvia o cancelado **avisando quais tools já rodaram** (opção escolhida
  pelo usuário), `PhaseTurnCancelled` como estado terminal próprio. **Defeitos achados no caminho,
  todos com teste**: (a) `View()` **mutava** `m.activity` — prefixava o spinner a cada frame (~10/s)
  e destruía o log de eventos (D-57); (b) `sanitize` **comia o `\n`** (é C0) e colapsava saída
  multilinha de tool numa linha só (D-58); (c) `handleRunes` **engolia `r`/`c`** quando havia banner
  — "verifique" virava "veifique", no caminho de recuperação pós-erro/pós-cancel (D-61); (d) o gate
  cortava o payload em 2 linhas **e** `fixedRows` reservava 1 linha para um diálogo de 3, empurrando
  o prompt para fora da tela (D-60, defeito do M11); (e) rodapé fatiando **bytes** (UTF-8 partido);
  (f) cancelamento pintado como **erro** vermelho. Versão `0.13.0-m13`. Host 17/17 (127 testes em
  `cmd/ancientcode`), WSL `-race` 17/17 (**sequencial por pacote**: em paralelo sobre `/mnt/d` o
  pacote estoura o orçamento sem ser falha real), `gofmt`/`vet` limpos, `GOAMD64=v1` ok. Smoke
  Inspiron: prompt de 152 caracteres em 3 linhas com continuação, spinner animando, `Esc` cancela e
  devolve o prompt, gate aberto com input visível. **Não validado**: integridade bit-a-bit via
  `--run` (cold start do NIM >600 s neste ambiente) e a camada 3 `/models --check` (dívida do M12).
  Ver `docs/session-2026-10-02-m13-painel-sessao.md`.
- **M13 mergeado em `main`** (PR #7, `9742e09`), e em seguida **rename `AncientAI` → `AncientCode`**
  (D-2026-10-02-63) + **licença** em `rename-ancientcode` (commits `99aa13f`, `4651294`, `08f6254`):
  module path `github.com/antonioghizidev/ancientcode` (habilita `go install`), `cmd/ancientcode/`,
  config `ancientcode.yaml`, env `ANCIENTCODE_*`, skills `~/.config/ancientcode/skills`, stamp
  `.ancientcode-verified`, banner e docs. Rename **duro, sem fallback**; a mensagem de config passou
  a **nomear o arquivo consultado** (com rename duro, "api_key_env não está definido" parece falta
  de segredo quando a causa é o arquivo ter parado de ser lido). **LICENSE Apache-2.0** e
  **CONTRIBUTING.md** (registra os invariantes que uma contribuição não pode quebrar). Três gates
  novos (`TestNoStaleOldNameInVersionedFiles`, `TestModulePathMatchesRepo`,
  `TestConfigDefaultFilenameMatchesBinaryName`) — o primeiro **não cobria a raiz do repo**, então o
  README nunca era verificado; descoberto ao injetar o nome antigo e ver o gate passar (D-64). Smoke
  Inspiron: binário, banner, config, sessão preservada e todos os comandos.
- **M14 especificado** (`specs/milestones/14-m14-connect-credenciais.md`, D-65..68), **não
  implementado** — é o próximo. Tira o usuário do `.env` no caminho principal: `internal/creds`
  (`~/.local/share/ancientcode/auth.json`, `0600`, escrita atômica, verificar antes de gravar),
  `internal/state` (último modelo por provedor, `state.json`, **separado** do `auth.json`), `/connect`
  na TUI e no REPL + `auth login|logout|list`, onboarding de primeiro uso antes do `Validate`
  (sem TTY nunca abre diálogo; rejeitar sai 1 sem escrever nada), e `--models` persistindo o modelo
  por provedor. **Três invariantes a reter**: (a) config **nunca** tem valor de chave — o que muda é
  que existe **um** arquivo `0600`; (b) `/connect` **fixa** o provedor, senão a chave colada seria
  ignorada em silêncio e a tela mentiria sobre a chave em uso (D-66); (c) **`0600` não protege
  contra o agente** — `shell` roda como `run_user`, sem privilégio; a defesa é o gate humano.
  Preparo para `go install`. Fora de escopo com motivo: keyring (não existe em server minimal),
  senha mestra (pior que `.env`), OAuth (nenhum provedor atual exige).
- **Próximo**: mergear `rename-ancientcode` → `main`; depois implementar o M14.
- **Backlog anotado para a PRÓXIMA sessão de specs** (pedido do usuário em 2026-10-02; **não
  implementar no M13**, seção 10 da spec M13): **`/compact`**, **`/new`** e **`/autocompact`** —
  **paridade com o opencode** (decisão do usuário em 2026-10-02, medida em `dev.opencode.ai/docs/tui`
  e no fonte `opencode-ai/opencode`): lá a unidade é a **sessão**, e os três criam **sessão nova**
  (`/new` = "start a new session", alias `/clear`, `Ctrl+N`; `/compact` = "summarize … and create a
  new one with the summary"; autocompact ≈95% da janela, também cria sessão nova). Isso **resolve** a
  pergunta da âncora: `/new` **não** é limpar o contexto da sessão atual, é trocar de `.jsonl`, logo
  é trivialmente reversível. **Risco principal: sem `/sessions` (listar/trocar, alias `/resume`) o
  operador perde o acesso à sessão anterior** — AncientCode já tem `--resume` (M5). Contexto hoje
  **não é estado**: `loop.go:185/202/266` recarrega do disco a cada turno e aplica `reduceHist` (§10.0).
  Ponto a decidir na spec: `session_context_bytes` mede **bytes de histórico** e `ContextUsage.pct`
  mede **fração da janela** — grandezas diferentes; e a ordem truncar-vs-resumir precisa ser
  invertida, senão o automático produz buraco. `/reset` (que **apaga** o `.jsonl`) fica como o
  comando perigoso sem equivalente no opencode — candidato a renomear.
- **Backlog Web** (SSE + shell-HTTP + `Approver` por canal + auth/TLS/CSRF) — reusa o contrato
  M8/M12; **agendamento depende do usuário**.

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
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags="-s -w" -o dist/ancientcode ./cmd/ancientcode
```