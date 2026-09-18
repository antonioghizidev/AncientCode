# Sessão 2026-09-18 — m9(tui)

> Resumo da sessão M9 — TUI Bubble Tea estilo lazydocker sobre o contrato M8. Metodologia:
> `specs/methodology.md`.

## Objetivo da sessão

Entregar o milestone **M9** (spec `specs/milestones/09-m9-tui.md`): frontend de UI interactiva
`--tui` (Bubble Tea) **em `cmd/ancientai` apenas**, consumindo o contrato que o M8 estabilizou —
parser de comandos puro compartilhado, `loop.Approver` como única via de aprovação, observer de
eventos estruturado, `ContextUsage`. O core (`internal/*`) permanece 100% stdlib.

## O que foi feito

- **Dependência pinada** (D-2026-09-17-49): `github.com/charmbracelet/bubbletea v1.3.10` +
  transitives (x/term, lipgloss, ansi, termenv, go-isatty, etc.), Go puro, compatível
  `GOAMD64=v1`/`CGO_ENABLED=0`. `go mod tidy` moveu para `require` direto.
- **`tui_panels.go`** (stdlib-only, determinístico): `ctxBar` (5 células, `<0` → `--`),
  `buildFooter` (2 linhas, trunc `lineCap=90`, `>80%` → `!`), `sessLine`, `activityFromEvent`
  (vocabulário D-35 + re-redação defensiva), `readAuditTail`, `buildView`, `capLines`,
  `truncated`, `canUseTUI(stdinTTY && stdoutTTY)`.
- **`tui_gate.go`**: `tuiGate` implementa `loop.Approver` (mesma interface do REPL) com diálogo
  único serializado (slot de capacidade 1), veto por tool (`d`), fail-closed em `ctx` cancelado
  (deny + erro). `opened()/closed()` para a UI.
- **`tui.go`**: `tuiModel` (tea.Model) — teclas → comandos do core (Enter submit, `r` retry/
  Continue, `c` limpa banner, ↑/↓ histórico, y/n/d no gate, C-c cancela turno / C-c no prompt
  sai), comandos `/...` via parser compartilhado, View com painel de sessão (re-redação
  defensiva), atividade e footer; `runTUI` wiring (observer → tea messages, bridge do gate).
- **`main.go`**: flag `--tui`; recusa com `--run` e sem stdin+stdout TTY (`canUseTUI`) — pipes
  e captures continuam no caminho byte-estável; branch TUI desvia o approver para `tuiGate`;
  versão `0.9.0-m9`.
- **Gate**: host `gofmt` (só os 6 novos, CRLF e novo final de linha corrigidos) + suíte serial;
  WSL `-race` completo; build Linux `GOAMD64=v1`; `go vet` limpo.

## Decisões

- **D-2026-09-17-49** — Bubble Tea v1.3.10 pinado, delivery-only (`cmd/ancientai`); render ASCII
  puro no View (sem lipgloss por ora); enforcement arquitetural já existente
  (`TestLoopMustNotImportCmdOrUI`).

## Testes

| Pacote | Foco | Status |
|---|---|---|
| `cmd/ancientai` (novos) | gate (approve/deny/veto/cancel/fail-closed/serialização), panels (footer/ctx/atividade/audit), Model (teclas→core, gate, redação no View) | ✅ |
| `internal/*` | regressão (M1–M8) intacta | ✅ |

**Total: host serial 16/16 + WSL `-race` 16/16, 0 falhas**; `gofmt`/`go vet`/`go build` limpos.
Números exatos contados no WSL (serial + `-race`) e no host (`go test -count=1 ./...`).

## Critérios de aceite

| Critério | Status |
|---|---|
| Suíte completa verde (host + WSL `-race`) + gofmt/vet/build | ✅ |
| Testes de segurança do milestone: gate fail-closed, veto, redação no View, `--tui` TTY-only | ✅ |
| Arquitetura: `internal/*` sem Bubble Tea/`cmd/*` (imports_test) | ✅ |
| `--tui` não altera política nem formatos de pipe/`--run`; REPL default intacto | ✅ |
| Smoke Inspiron real com NIM | ⏳ (pós-commit, validação no Dell) |
| Docs: DECISOES (D-49) + design doc + nota de sessão + AGENTS.md | ✅ |

## Próximos passos

1. Commit `feat(M9)` e smoke Inspiron como `ancientai` com NVIDIA NIM (`--tui` real, raw/PTY:
   gate ask y→executed auditado, `d`→veto, C-c cancel, `/provider` refletido no footer).
2. Backlog Web (SSE + shell-HTTP + `Approver` por canal + auth/TLS/CSRF) como milestone futuro
   reusando o contrato do M8/M9 (registrado no DoD do M9).

## Observações / riscos

- `gofmt -l` no host acusa arquivos pré-existentes por CRLF de checkout (artefato); o gate real
  de `gofmt` roda no WSL (LF). Ao copiar para Linux usar `tr -d '\r'` (nunca `sed 's/\r$//'` da
  GNU sed que remove letras `r` no fim de linha — corrompeu `buf[m.cursor] = r` no rsync).
- O smoke de render de terminal (RawMode/alt screen do Bubble Tea) não roda em testes host —
  validação é o pty no Inspiron, conforme `09-m9-tui.md` §5.