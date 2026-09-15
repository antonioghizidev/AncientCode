# M1 — Skeleton funcional

**Status**: ✅ concluído em 2026-09-14
**Depende de**: nenhuma
**Sessão de referência**: `docs/session-2026-09-14-m1.md`

## 1. Objetivo

Esboço executável do harness: configuração, cliente OpenAI-compat (OpenRouter/NVIDIA/LiteLLM),
parser SSE, histórico persistente e redigido, e REPL básico — pronto para rodar no notebook
sem AVX.

## 2. Escopo / entregáveis

- [x] `internal/redact` — redação de segredos + `FindSecrets`.
- [x] `internal/config` — Load/Validate YAML + env com deny-by-default.
- [x] `internal/provider` — interface `Provider`; cliente OpenAI-compat com SSE; acumulação
      de `tool_calls`; erros redigidos preservando sentinela; limites de leitura.
- [x] `internal/model` — Store JSONL 0600 append-only, redação na escrita e na leitura.
- [x] `internal/loop` — turno M1; fail-closed se o modelo pedir tools.
- [x] `cmd/ancientai` — REPL (`-config`, `-session`, `-reset`, `-version`).
- [x] `ancientai.example.yaml`, `.gitignore` (segredos, binários, sessions, `.obsidian/`).

## 3. Contratos

- `provider.Provider.Chat(ctx, msgs, tools, onEvent) (ChatResult, error)` — ver
  `docs/SPEC.md` §4.
- `config.Config` + `config.Load(path)` — ver `docs/SPEC.md` §8.
- `model.Store` (Open/Append/Load/Reset).
- `loop.Loop.Turn(ctx, user) (string, error)`.

## 4. Requisitos de segurança deste milestone

- [x] Chave de API só via env; erro cita nome da variável, nunca valor.
- [x] Erros do provedor redigidos (`redactedError` preserva sentinela).
- [x] `base_url` http só loopback; `run_user` nunca root; `model`/key obrigatórios.
- [x] Leituras limitadas (anti-bomba): linha SSE 8 MiB, corpo 16 MiB, erro 64 KiB.
- [x] Histórico persistido sempre redigido (defesa em profundidade na leitura).
- [x] Loop fail-closed diante de `tool_calls` (tools são M2).

## 5. Testes obrigatórios

### Funcionais

- [x] Config: defaults, precedência env>file, timeouts.
- [x] SSE: conteúdo multievento, `[DONE]`, ferminação, lines não-`data:`.
- [x] Acumulação de `tool_calls` em múltiplos frames (SPEC §4.2).
- [x] Store round-trip; histórico completo no loop; erro propagado.

### Segurança (devem falhar se a proteção for removida)

- [x] `redact`: tipos conhecidos mascarados, idempotente; `FindSecrets`.
- [x] Config: http não-loopback rejeitado; `run_user=root` rejeitado; key ausente; aprovação
      inválida; `shell_tool=enabled` sem `ask` rejeitado.
- [x] Provider: erro não-2xx **não vaza a key**; erro do provedor redigido; corpo grande
      falha fechado; contexto cancelado.
- [x] Store: segredo **nunca chega ao disco** (content e tool args).
- [x] Loop: input do usuário chega redigido ao provider e ao disco; `tool_calls` → recusa sem
      persistir.

## 6. Critérios de aceite (DoD)

- [x] Implementação conforme escopo.
- [x] **Todos os testes do projeto passam** (`go test -count=1 ./...` → 66 exec., 0 falhas).
- [x] `gofmt`/`go vet`/`go build` limpos.
- [x] `docs/DECISOES.md` atualizado (D-01..D-12).
- [x] Nota de sessão: `docs/session-2026-09-14-m1.md`.
- [x] Artefato do notebook compilado: `dist/ancientai` com `GOAMD64=v1`/`CGO_ENABLED=0`.
- [x] Status atualizado no `docs/SPEC.md` (§12).
- ⏳ **Aceite final (exigência fora do gate M1)**: smoke test no notebook alvo com chave real
  ("responda em uma frase") via OpenRouter e NVIDIA.