# Sessão 2026-09-18 — inspiron-m9

> Resumo exclusivo desta sessão. Método: `specs/methodology.md`. Smoke real do M9 (TUI `--tui`)
> no Inspiron (Dell, GOAMD64=v1), usuário `ancientcode`, provider NVIDIA NIM.

## Objetivo da sessão

Validar no hardware real (pto smoke via SSH/pty cru) todos os pontos fáceis de enganar em teste
host: render do Bubble Tea em pseudo-terminal real, gate humano `y`/`d`, cancelamento de turno,
comandos `/provider`/`/model`, guarda `--tui` com pipe, saída limpa (`/exit`). Binário
`ancientcode-0.9.0-m9-tui` (8.4 MB) deployado em `/home/ancientcode`.

## O que foi feito

- **Driver de pty em Python** (`tui_drv.py`/`smoke_gate.py`/`smoke_cancel.py`/`tui_cmd.py`,
  em `/tmp` do remoto — não entram no repo): `pty.fork()` + `TIOCSWINSZ` 80/100x24 + `TERM=
  xterm-256color`. Descoberto que o Bubble Tea envia queries de terminal (OSC11 `\x1b]11;?` e
  DSR `\x1b[6n`) no início e **fecha o modelo de input do pty cru se não receber resposta** —
  respondendo `\x1b]11;rgb:…\x1b\\` + `\x1b[1;1R` o keyboard desbloqueia (defeito do *driver de
  teste*, não do produto; terminal real responde sozinho). Enter = `\r` no modo raw.
- **Gate ask `y`**: prompt `escreva um arquivo … smoke-m9.txt …`; audit
  `write_file … status=executed output_hash=… policy=ask decision=allow duration=8ms`; arquivo
  criado com conteúdo `hello-smoke-m9`; atividade renderizada `modelo: aguardando o primeiro
  token… → recebendo tokens… → gate: write_file → tool: write_file (executando) → tool:
  write_file ok 0.00s`; footer `● espera` durante e `▪ ok` ao final.
- **Veto `d`**: mesmo prompt p/ `smoke-m9-veto.txt`; audit `status=denied decision=denied`;
  arquivo NÃO criado; diálogo `[aprovar] write_file | y aprova, n nega, d nega sempre` visível;
  footer `✗ erro` após.
- **Cancelamento `C-c`** (0x03) durante turno em voo: `erro: provider: SSE read: context
  canceled` no banner; 0 registros da tool no audit; arquivo NÃO criado; loop vivo após o cancel
  (footer re-renderizado) — combina com `planejCalls só executa após Decide aprovar no curso do
  turno`.
- **`/provider`** → `provider base_url: https://integrate.api.nvidia.c…` no painel de sessão;
  **`/model`** → `model: nvidia/nemotron-3.5-lightning-30b-a3b`; **`/exit`** → seq `^[?2004l
  ^[?25h ^[?1049l` restaura o terminal (sem travas).
- **Guarda pipe**: `echo ola | ancientcode --tui` → `erro: --tui exige stdin E stdout TTY
  (pipes/arquivos recusados)`, RC=2.
- Artefatos de teste removidos do `/opt/ancientcode/code` no final; audit do smoke preservado
  (append-only).

## Decisões

- Managing o pty em teste (cartório): responder OSC11 + DSR. Não é decisão de produto — registrado
  como observação do harness de smoke (evita re-descoberta).

## Testes

| Pacote | Foco | Status |
|---|---|---|
| Host (serial) | 16 suítes verdes | ✅ |
| WSL `-race` | 16/16 | ✅ |
| Inspiron smoke | gate y/d, cancel, `/provider`, `/model`, exit, pipe guard | ✅ |

## Critérios de aceite

| Critério | Status |
|---|---|
| Smoke Inspiron NIM: gate `y`→executed auditado, `d`→veto, cancel sem execução, `/provider` no footer | ✅ |
| `--tui` não altera política nem formatos de pipe/`--run`; REPL default intacto | ✅ |
| Binário `GOAMD64=v1`, `CGO_ENABLED=0` roda no Core 2 Duo sem AVX | ✅ |
| Docs: DECISOES (D-49) + AGENTS + nota de sessão | ✅ |
| Manual do usuário (`docs/MANUAL.md`) | ✅ |

## Próximos passos

1. Commit `feat(M9)` ampliado com nota Inspiron + manual do usuário.
2. Backlog **Web** (SSE + shell-HTTP + `Approver` por canal + auth/TLS/CSRF) como milestone
   futuro no DoD do M9.

## Observações / riscos

- Ao interagir com Bubble Tea via pty programático é obrigatório responder às queries de terminal;
  no terminal real isso é do emulador. Não é bug do produto.
- `gofmt -l` no host acusa arquivos pré-existentes por CRLF de checkout (artefato); o gate real
  de `gofmt` roda no WSL (LF). Ao sincronizar p/ Linux usar `tr -d '\r'` (nunca `sed 's/\r$//'`).