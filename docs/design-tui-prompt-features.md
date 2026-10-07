# Design — TUI (estilo lazydocker) + recursos de interação prompt/UI

> **Status**: **M9 concluído** (`specs/milestones/09-m9-tui.md`, branch `m9-tui`; TUI Bubble Tea
> `--tui` TTY-only em `cmd/ancientcode` consumindo o contrato do M8 — parser comandos compartilhado,
> `tuiGate`/`loop.Approver` como única via de aprovação, observer de eventos no painel de
> atividade, footer com ctx/status). O REPL de texto continua default e byte-estável. Web futura
> reusa o mesmo contrato (backlog em `09-m9-tui.md` DoD). Decisões: D-2026-09-16-36 (Bubble Tea),
> D-2026-09-16-37 (paridade prompt↔UI + semânticas), D-2026-09-17-49 (pin v1.3.10, render ASCII
> puro, sem lipgloss em View por ora).

## 1. Objetivo

Dar ao operador percepção contínua e controle fino sobre o agente, sem mudar política de
segurança. DUAS superfícies equivalentes:

- **Prompt (REPL de texto)** — default, como hoje, enriquecido;
- **UI (TUI)** — opcional via `--tui`, no estilo lazydocker/lazygit: prompt numa janela,
  footer com estado do app/modelo/provedor, painéis de atividade e, futuramente, painel de
  agentes ativos (M8).

Princípio (D-37): **cada recurso é implementado uma vez no domínio** (comando/método), e os
dois frontends apenas renderizam. Testes de segurança atacam o domínio, nunca o render.

## 2. Arquitetura

```
┌──────────────────────────────────────────────────────────────┐
│ cmd/ancientcode                                                 │
│   repl.go        → frontend de texto (readLine + render inline)│
│   tui.go         → frontend Bubble Tea (--tui)                │
│   commands.go    → domínio: /historico /retry /continue       │
│                    /model /provider /context                  │
├──────────────────────────────────────────────────────────────┤
│ internal/loop                                                 │
│   Observer (eventos estruturados)   + Loop.ContextUsage()     │
│   Loop.SwitchModel / Loop.SwitchProvider (provider mutante)   │
│   Continue("") = re-rodar sem duplicar (retry)                │
├──────────────────────────────────────────────────────────────┤
│ internal/provider  → Client.SetModel / SetBaseURL            │
└──────────────────────────────────────────────────────────────┘
```

O **loop continua sem saber de UI**: os dois frontends usam o observador de eventos (extensão
do `SetStatus` da D-35) e a interface `Approver` (o gate vira diálogo na TUI).

## 3. Layout da TUI (proposta)

```
┌───────────────────────────────┬──────────────────────────────┐
│ Sessão (histórico, rolável)   │ Atividade                    │
│ > oi                         │ ● modelo: recebendo tokens…   │
│ < vou executar…              │ │ shell: id -u               │
│   ⚠ aprovação: write_file    │ │   (executando) +0.3s       │
│ < arquivo criado             │ ◌ apt: (fila)                │
├───────────────────────────────┴──────────────────────────────┤
│ > [___________________________________] ↵   ↑↓ histórico    │
├──────────────────────────────────────────────────────────────┤
│ ancientcode 0.6.0-m6 | nvidia/nemotron… | run | default.jsonl │
│ ctx ▓▓▓░░ 42% | ◉ ok          ← footer (estado + medidor)    │
└──────────────────────────────────────────────────────────────┘
```

- Foco único na janela de prompt (simplicidade); painéis são projeção de eventos.
- Painel **Atividade** mostra: fase atual do modelo, execuções de tool (em andamento +
  último resultado com duração/status), fila.
- Painel **agentes ativos** = novo view-module futuro (M8), mesmo canal de eventos.
- Footer: `versão | provider model | modo | sessão | ctx% | status(◉ ok/○ espera/✗ erro)`.

## 4. Observador de eventos (estende D-35)

Mantém compatibilidade: o REPL de texto hoje recebe strings de `SetStatus`; a TUI recebe um
strução estruturada. Wrapper converte o estruturado→string para quem só quer o texto.

```go
type Event struct {
    Phase  Phase   // ModelWaiting | ModelStreaming | GateWaiting | ToolRunning | TurnOK | TurnError
    Tool   string  // ex.: "shell" (vazio p/ fases de modelo/gate)
    Detail string  // summary redigido (ver redact) ou texto de erro redigido
    Dur    time.Duration // quando ToolRunning/TurnOK
    At     time.Time
}
```

Emitidos em: início de `p.Chat` (ModelWaiting), primeiro evento SSE (ModelStreaming), antes de
`appr.Decide` (GateWaiting), início e fim de `execOne` (ToolRunning → TurnOK em caso de tool),
e em erro de turno (TurnError). A TUI também consome o **audit** para o painel de atividade
(nada de novo em termos de acesso a segredos — já é append-only redigido).

## 5. Recursos (semântica por comando + comportamento nas duas superfícies)

### 5.1 Histórico de prompts (≥ 10)

- **Domínio**: buffer circular em memória com os últimos `N=20` prompts submetidos
  (redigidos; nunca segredos em texto claro no render — `redact` já garante).
- **Prompt**: setas ↑/↓ reeditam o prompt anterior (requer raw mode no REPL de texto — ver §6);
  contagem visível via `/historico` (lista indexada) e `!N` reenvia a entrada N.
- **UI**: ↑/↓ na janela de prompt (mesmo buffer), lista visível num popup (`/historico`).

### 5.2 Recuperação após falha (retry / continue)

- **Domínio**: `agent.Continue("")` re-roda o turno a partir do histórico persistido **sem
  duplicar** o texto (o histórico já tem a última user message). `erro` de turno nunca altera
  o histórico a meio (persistência só acontece em sucesso/plano aprovado).
- **Prompt**: após `erro: ...`, prompt sugere `[R]etry` (roda `Continue("")`) / `[C]ontinue`
  (deixa vir nova instrução) / Enter p/ ignorar. Comandos explícitos: `/retry`, `/continue`.
- **UI**: banner de erro no painel de atividade + tecla `r`/`c` com o mesmo efeito.

### 5.3 Escolher modelo ou provedor em runtime

- **Domínio**: `Loop.SwitchProvider(baseURL, apiKeyEnv)`, `Loop.SwitchModel(model)` —
  delegam a `Client.SetModel/SetBaseURL` (mutadores a adicionar; `apiKeyEnv` idem). Novo
  estado aparece no sistema; nada persiste em YAML (D-37: troca é por **sessão**; runtime
  vence env-overrides atual).
- **Ptévia de segurança**: a troca muda apenas o endpoint que recebe o histórico **redigido**
  e as tool defs; nenhum segredo de ferramenta sai do loop. Futuro: registrar evento de troca
  no audit (`event: provider_switch`) como marcador auditável.
- **Prompt**: `/model <nome>` (ex.: `nvidia/nemotron-3.5-lightning-30b-a3b`), `/provider
  <openrouter|nvidia|litellm>` (usa base_url padrão do provedor + certifica `api_key_env`
  do par), `/provider` e `/model` sem argumento mostram o ativo.
- **UI**: comandos idênticos no prompt da TUI; fica refletido no footer (razão principal).

### 5.4 Interromper, adicionar informação e continuar

- **Domínio**: Ctrl+C cancela o `ctx` do turno corrente (respeitando teto de 5 min de turno).
  O histórico **já persistido** é preservado; o turno cancelado não grava parcial.
  "Continuar" = nova mensagem normal (append → run) ou `/continue`.
- **Prompt**: em cooked/raw, capturar SIGINT → cancelar ctx; linha interrompida na edição é
  descartada. Banner `turno interrompido; digite nova instrucao ou /continue`.
- **UI**: C-c na janela de prompt cancela; toast no painel de atividade; footer volta a
  ◉ ok/○ espera.
- **Segurança**: turno cancelado não dispara execução pendente — `planCalls` só executa
  ferramenta após `Decide` aprovar **no curso do turno**; se o ctx morreu, nada roda depois
  do cancelamento.

### 5.5 Tamanho e percentual do contexto

- **Domínio**: `Loop.ContextUsage()`:
  - `msgs` = mensagens do histórico (arquivo, full; `Store.Load`),
  - `fileBytes` = bytes da sessão (soma de `turnBytes`),
  - `sentBytes` = bytes do trecho **efetivamente enviado** (pós `model.ReduceMessages`,
    `session_context_bytes`),
  - `estTokens` = `sentBytes/4` (heurística chars/4 — documentada como aproximação),
  - `pct` = `sentBytes/budget` (budget = `session_context_bytes`; 100% = teto do agente).
  - Se futuramente houver `context_window_tokens` no config, pct também sai contra a janela
    do provedor.
- **Prompt**: `/context` imprime tabela (linhas acima) e o `pct` como barra.
- **UI**: barra compacta no footer (`ctx ▓▓▓░░ 42%`, cor muda >80% ⚠) + `/context` abre
  popup com detalhes.
- **Observação**: o `ReduceMessages` já protege o contexto (M5); o medidor é **percepção**
  (o usuário vê quando o agente começa a podar histórico), sem tocar na mecânica.

## 6. Input no REPL de texto (raw mode)

O REPL atual usa `bufio.ReadString` (cooked). Para ↑/↓, edição e C-c no prompt, o REPL de
texto passa a usar raw mode via `x/term.MakeRaw` (dep já entra com Bubble Tea) no modo
interativo **TTY-only**: para pipe (smokes, `--run`) mantém o `readLine` atual — garantia de
que scripts continuam byte-a-byte estáveis. Mapa de teclas mínimo: setas ↑/↓ (histórico),
←/→/Backspace (edição), Enter, C-c (cancelar/interromper). Editor completo (ctrl+setas,
paste bracketing) fica para ajuste fino.

## 7. Segurança

- Nenhuma mudança em: fail-closed do loop, política de aprovação (o `Approver` da TUI é a
  mesma interface), redação (`redact`), audit append-only, tetos (turno 5 min, tool timeouts,
  SSE 16 MiB), `GOAMD64=v1` (TUI é Go puro — sem AVX não afeta).
- Troca de provider nunca, sob nenhuma hipótese, desativa TLS nem troca de redação.
- O medidor de contexto é informativo: `ReduceMessages` continua o único mecanismo de poda e
  seu comportamento não é alterado.

## 8. Fora de escopo agora (registrado)

- Persistir escolha de modelo/provedor em YAML (voltaria a discutir: rompe fail-closed de
  "config é a fonte de verdade"; só com flag explícita).
- Painel de múltiplos agentes ativos (M8 já prevê — a TUI e o observador foram desenhados
  para adicioná-lo como view-module).
- Temas/cores customizadas, mouse bindings, suporte OCR/terminal gráfico.
- Janela de contexto do provedor como número exato (fica como `context_window_tokens`
  opcional no config — não há API de limite confiável comum aos 3 provedores).