# M8 — Interação prompt/REPL (domínio + comandos)

**Status**: ⏳ candidato (backlog aprovado em D-2026-09-16-36/37; depende de M7 concluído)
**Depende de**: M7 (fechado)
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Entregar os cinco recursos de interação de D-2026-09-16-37 **no domínio e no REPL de texto**,
com a fronteira **UI ↔ core definida de forma transport-agnóstica**. Tudo 100% stdlib — Bubble Tea
entra só no M9. Este milestone cria a superfície de comandos/eventos que a TUI (M9) e uma futura
**Web UI** consomem sem tocar no core.

## 2. Camadas e regras de arquitetura (DDD/SOLID/DI)

```
cmd/ancientai                     # delivery (uma fronteira por frontend)
  repl.go      → frontend texto: consome domínio e renderiza
  commands.go  → parser de /comandos -> chama o core (nunca o inverso)
  input.go     → raw-mode mín. (TTY-only) | cooked para pipe/--run
internal/loop                     # core do agente (núcleo hexagonal)
  Observer (Event estruturado, JSON-serializable)
  ContextUsage, SwitchModel, SwitchProvider, Continue("")
  Approver (interface do gate — já existente)
internal/provider                # contrato OpenAI-compat (mutadores)
internal/model                   # Store + métricas de bytes p/ medidor
```

- **DIP**: `loop` depende de abstrações (`provider.Provider`, `model.Store`, `Approver`,
  `Observer`). Nenhuma classe de UI existe dentro de `internal/loop`.
- **SRP**: um frontend = um adaptador (REPL hoje, TUI amanhã, Web depois) que implementa
  `Observer` + `Approver` e chama os métodos do loop. Zero mudança de core por frontend (OCP).
- **Direção da dependência é regra de build**: `cmd/* → internal/loop`; `internal/loop` NUNCA
  importa `cmd/*`. Reforço em CI via teste de importação raiz (`internal/loop` só importa o que já
  importa hoje + provider/model/redact/audit/tools).
- **O contrato de UI é arquitetural**: um frontend só pode usar (a) métodos/consultas do loop,
  (b) stream de eventos, (c) gate. O contrato é o que permitirá transportes novos (SSE/WebSocket)
  sem refatorar o core.

> **Web futura (fora de escopo, prevista)**: `Event` já nasce JSON-serializable e os comandos são
> métodos puros → um milestone Web posterior adiciona transporte (HTTP+SSE) + `Approver` por canal
> + auth/TLS/CSRF **sem tocar em `internal/loop`**.

## 3. Contrato de eventos (Observer)

```go
type Phase int // ModelWaiting | ModelStreaming | GateWaiting | ToolRunning | TurnOK | TurnError | ProviderSwitch

type Event struct {
    Phase  Phase        `json:"phase"`
    Tool   string       `json:"tool,omitempty"`
    Detail string       `json:"detail,omitempty"` // summary/erro já redigido (redact)
    Dur    time.Duration `json:"dur,omitempty"`
    At     time.Time    `json:"at"`
}
```

Emitidos em: início de `Chat` (ModelWaiting), 1º evento SSE (ModelStreaming), antes de `appr.Decide`
(GateWaiting), início/fim de `execOne` (ToolRunning → TurnOK), erro de turno (TurnError),
troca de modelo/provedor (ProviderSwitch). Mantém `SetStatus(string)` para consumidores texto
(wrapper ~20 linhas converte Event→fase). `Observer` pode ser nil (silencioso, fail-closed default).

### Teste de contrato obrigatório
- `Event` serializa/deserializa JSON com `roundtrip` estável (garante transporte futuro).

## 4. Recursos e semântica (comandos do REPL)

| Recurso | Domínio (loop) | Comando REPL |
|---|---|---|
| Histórico de prompts | buffer circular N=20 **redigido** em memória | `/historico`, `!N` reenvia, ↑/↓ reedita |
| Retry pós-falha | `Continue("")` (não duplica user; histórico nunca grava parcial) | `/retry` |
| Continue | estado de prompt normal (após banner de erro) | `/continue` |
| Modelo runtime | `SwitchModel(m)` → `Client.SetModel` | `/model [nome]` |
| Provedor runtime | `SwitchProvider(baseURL, apiKeyEnv)` → `Client.SetBaseURL` | `/provider [nome]` |
| Contexto | `ContextUsage()` | `/context` |
| Interromper | cancel do ctx do turno (teto 5 min respeitado) | Ctrl+C |

- `/model`/`/provider` sem argumento mostram o ativo (footer/estado).
- Troca é **por sessão**, não persiste no YAML (D-37); runtime vence env-overrides daqui em diante.

## 5. Requisitos de segurança deste milestone

- [ ] Nenhuma mudança em: ask/allow/deny, `redact`, audit append-only, tetos (turno/tool/SSE).
- [ ] `SwitchProvider` **nunca** desativa TLS nem troca redação; recusa switch com `api_key_env`
      ausente (fail-closed: erro antes de mudar estado, estado inalterado).
- [ ] Histórico de prompts e `/historico` exibem apenas texto **redigido**.
- [ ] Turno cancelado por Ctrl+C não dispara tool pendente: `planCalls` exige `Decide` **no curso
      do turno**; `execOne` herda o ctx cancelado → nada roda pós-cancelamento sem novo gate.
- [ ] Raw mode é **TTY-only** (`isCharDevice`); pipe e `--run` permanecem cooked/byte-estável
      (smoke/CI intactos).

## 6. Testes obrigatórios

> Regra: testes de segurança atacam o **domínio** (loop/provider/model), nunca o render.

- [ ] `ContextUsage`: `sentBytes ≤ budget` com e sem `session_context_bytes`; `pct` = 100% no teto.
- [ ] `SwitchModel`/`SwitchProvider` mudam a próxima `Chat` (defs/base_url); switch com env ausente →
      erro e estado inalterado.
- [ ] `Continue("")` não duplica a última user message (idempotência de retry).
- [ ] Event stream: ordenação de fases; `ToolRunning` com início+fim+dur; `TurnError` redigido.
- [ ] `Event` JSON roundtrip estável.
- [ ] Parsing de comandos: `!N` só índices válidos, `/model` sem arg = show, `/historico` redigido.
- [ ] Cancelamento via ctx (loop-level, sem tty): turno cancelado → erro limpo e nada executado.

## 7. Critérios de aceite (DoD)

- [ ] **Suíte completa verde** (`go test -count=1 ./...` host serial + Linux WSL `-race`).
- [ ] `gofmt -l .` vazio; `go vet ./...` e `go build ./...` limpos.
- [ ] Teste arquitetural de importação: `internal/loop` não importa `cmd/*`.
- [ ] `docs/DECISOES.md` atualizado com as decisões de execução deste milestone.
- [ ] Nota de sessão criada e status de SPEC/M8 atualizados.
- [ ] TUI (M9) ainda NÃO iniciada — M8 fecha com REPL rico e contrato de eventos estável.