# M9 — TUI estilo lazydocker (Bubble Tea, camada de delivery)

**Status**: ⏳ candidato (decidido em D-2026-09-16-36/37; **não iniciar antes do M8**)
**Depende de**: M8 (contrato de eventos + comandos do domínio estáveis)
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Entregar `--tui` no estilo lazydocker/lazygit **como mais um frontend sobre o mesmo core** que o
REPL do M8: painel de sessão, painel de atividade, janela de prompt e footer com estado.
**Bubble Tea vive apenas em `cmd/ancientai` (delivery)**; `internal/loop` permanece 100% stdlib (D-36).
Paridade completa com o prompt (D-37): cada recurso tem **uma** semântica no domínio (M8), a TUI só
renderiza.

## 2. Camadas e regras de arquitetura

```
cmd/ancientai
  tui.go       → programa Bubble Tea (Model/Update/View)
  tui_gate.go  → Approver dialog (mesma interface do gate)
  tui_panels.go→ projeção de eventos + leitura do audit (append-only, já redigido)
  commands.go  → parser de /comandos — COMPARTILHADO com o REPL (um parser, dois renders)
internal/loop  → NÃO muda por causa da TUI; consumido, nunca possuído (DIP/OCP)
```

- **SOLID**: `tui_panels.go` = view-modules com uma responsabilidade; `Update` só encaminha
  intents→comandos do core; `View` é projeção determinística de estado + eventos.
- **Direção da dependência**: `tui.go → internal/loop`. Proibido `loop` conhecer Bubble Tea.
  Mantém-se o teste arquitetural de importação do M8.
- **DI**: o programa Bubble Tea recebe por construtor: `*loop.Loop` (comandos), observer (stream de
  `Event`), gate já resolvido (ask→diálogo). Nada de singletons/estado global.
- **Interface `Approver` é a única via de aprovação**: a TUI implementa o mesmo contrato que o REPL.
  Uma futura Web UI implementaria um terceiro `Approver` (por canal/HTTP) **sem mudar o loop** —
  este é o desacoplamento vital herdado do M8.

## 3. Layout (projeção — idêntico ao contrato do design, §3)

```
┌───────────────────────────────┬──────────────────────────────┐
│ Sessão (histórico, rolável)   │ Atividade                    │
│ > oi                         │ ● modelo: recebendo tokens…   │
│ < vou executar…               │ │ shell: id -u               │
│   ⚠ gate: write_file          │ │   (executando) +0.3s       │
│ < arquivo criado              │ ◌ apt: (fila)                │
├───────────────────────────────┴──────────────────────────────┤
│ > [________________________] ↵  (↑/↓ histórico, r/c, C-c)   │
├──────────────────────────────────────────────────────────────┤
│ ancientai 0.9.0-m9 | nvidia/nemotron… | run | default.jsonl  │
│ ctx ▓▓▓░░ 42% | ◉ ok            ← footer (estado + medidor)  │
└──────────────────────────────────────────────────────────────┘
```

- Foco único na janela de prompt (simplicidade); painel Sessão é o histórico persistido; painel
  Atividade é a projeção dos `Event`s + últimas linhas do audit.
- Footer monta de: versão/modo/sessão (config) + provider/modelo **ativos** (loop) + `ContextUsage()`.
- Teclas: `↑/↓` (histórico de prompts, mesmo buffer do M8), `r`/`c` (retry/continue), `C-c` (cancelar
  turno; C-c novamente sai), `y`/`n`/`d` no gate (allow/deny/veto — mesmos significados do REPL).

## 4. Segurança deste milestone

- [ ] Nenhuma mudança em: fail-closed do loop, aprovação (mesmo `Approver`), redação (`redact`),
      audit append-only, tetos, `GOAMD64=v1`, build `CGO_ENABLED=0`.
- [ ] TUI **nunca** executa tool por atalho: só o `Decide` humano aprova; erros de tecla são
      inócuos no domínio.
- [ ] Painel Atividade exibe apenas texto redigido (audit já redigido; eventos já redigidos).
- [ ] Sem `--tui` → REPL inalterado byte-a-byte (regressão guardada pela suíte).
- [ ] `tui` só roda TTY; pipe/`--run` rejeitam `--tui` com erro claro (sem degradação silenciosa).

## 5. Testes obrigatórios

> No host, testar o que é Go puro e determinístico; render de terminal valida-se em pty real
> (WSL `script -qec`) e no Inspiron. Regra do M8 continua: **segurança ataca o domínio**.

- [ ] `Model.Update` com mensagens sintéticas: cada tecla mapeada → comando do core correto
      (↓ submit, `r`→Continue, `c`→limpa banner, C-c→cancel, `y`/`d`→Allow/Veto).
- [ ] Projeção eventos→painéis: snapshot determinístico (View strings) por fase/estado.
- [ ] Footer: montagem determinística (versão|provider model|modo|sessão|ctx%|status; cor > 80%).
- [ ] Parser de `/comandos` compartilhado: mesmas entradas → mesmas intents do REPL (`commands.go`
      único — teste de unificação).
- [ ] `ContextUsage` no footer: `pct` correto com `session_context_bytes` setado e 0 (off → `--`).
- [ ] Smoke pty (WSL/Inspiron): gate ask `y`→executed (audit `policy=ask`), `d`→veto, cancelamento
      de turno não executa tool, troca `/provider` refletida no footer.
- [ ] Regressão: suíte serial do M8 passa inteira (REPL default intacto).

## 6. Dependência Bubble Tea (D-36, pinada)

- Adicionar `github.com/charmbracelet/bubbletea` + `code.maelström.dev/x/term` pinadas em `go.mod`,
  **somente importadas em `cmd/ancientai`** (padrão `internal/loop` sem dependência nova). Viável no
  Inspiron sem AVX (Go puro, `GOAMD64=v1`, ~+2–3 MB, console tty basta).
- Justificativa da dependência registrada em DECISOES (atendendo a constituição) na execução.

## 7. Critérios de aceite (DoD)

- [ ] **Suíte completa verde** (host serial + WSL `-race`), `gofmt -l .` vazio, `go vet`/`go build` limpos.
- [ ] Teste arquitetural: `internal/*` continua sem imports de Bubble Tea / `cmd/*`.
- [ ] `--tui` não altera política nem os formatos de pipe/`--run`; smoke real no Inspiron com NIM.
- [ ] `docs/DECISOES.md` e `docs/design-tui-prompt-features.md` atualizados; nota de sessão criada.
- [ ] Backlog **Web** (SSE + shell-HTTP + `Approver` por canal + auth/TLS/CSRF) registrado como
      milestone futuro — TUI prova o contrato, não fecha a porta para web.