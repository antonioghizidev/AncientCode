# M10 — TUI painéis: bordas fixas, scrollbar e navegação entre painéis

**Status**: ✅ concluído em 2026-10-01 (host + WSL `-race` + smoke Inspiron)
**Depende de**: M9 (TUI Bubble Tea delivery-only, contrato de eventos + gate + parser)
**Sessão de referência**: `docs/session-2026-09-30-m10-tui-paineis.md`

## 1. Objetivo

Elevar o `--tui` entregue no M9 ao padrão lazydocker **real**: painéis fixos com borda
desenhada e título, **scrollbar** de proporção em cada painel, e **foco navegável** entre
painéis (`Tab` cicla; `↑↓/PgUp/PgDn/Home/End` rolam o painel focado; borda fica destacada por
cor ANSI). Tudo envolve o **mesmo core**: gate (`tuiGate` continua o único `Approver`),
observer de eventos, parser de `/comandos`, redação e auditoria — zero mudança em
`internal/*`.

## 2. Escopo / entregáveis

- [x] Layout painéis: **sessão** à esquerda (~60% da largura) e **atividade** à direita
      (~40%), moldura dupla box-drawing (─│┌┐└┘┬┴) com título no topo e borda vertical
      compartilhada.
- [x] **Viewport por painel** (offset clampado) + **scrollbar** proporcional: trilha `░` com
      thumb `█` na coluna interna direita de cada painel (coluna em branco quando não rola).
- [x] **Foco navegável**: `Tab`/`Shift+Tab` (e `←`/`→`) ciclam foco sessão ↔ atividade; o painel
      focado ganha marcador `▸` + cor (legível também sem suporte a cor); `↑/↓` rola 1 linha,
      `PgUp`/`PgDown` pagina, `Home`/`End` vai ao topo/fim do painel focado.
- [x] Prompt: **digitação nunca perde o foco de edição** (o "foco" de viewport é separado do
      teclado; digitar uma tecla devolve o foco ao prompt); `Enter` envia; `↑/↓` no prompt
      continuam histórico (como no M8/M9); `r`/`c` retry/clear; `C-c` cancela turno / sai;
      `/comandos` idênticos ao REPL.
- [x] Cursor desenhado in-band (reverse video) sem truncar/padding do texto da linha de input.
- [x] Gate `y`/`n`/`d`/`C-c` com diálogo renderizado no mesmo posicionamento do M9.
- [x] Footer 2 linhas (versão|provider modelo|modo|sessão; `ctx ▓▓▓░░ % | ◉ ok`) intacto.

## 3. Contratos (render)

Projeções puras em `cmd/ancientcode/tui_panels.go` (só stdlib, determinísticas, sem TTY):

- `renderPanels(sess, act, sessOff, actOff, width, rows, focus) []string` — moldura dupla
  (sessão ~60% à esquerda, atividade à direita) com borda vertical compartilhada `┬`/`┴`;
  cada painel tem coluna de scrollbar (`░` trilha + `█` thumb) e o título do painel focado
  recebe `▸` + cor.
- `viewportFor(off, total, visible, delta) int` — novo offset clampado em `[0, total-visible]`;
  `total <= visible` mantém `0`.
- `scrollThumb(off, total, visible) (row int, scrollable bool)` — linha do thumb (proporcional).
- `scrollbarCol(visible, thumbRow, scrollable) []rune` — a coluna de scrollbar; painel que não
  rola recebe coluna em branco (a largura do texto nunca salta).
- `promptLine(text, cursor) string` — linha de input com o cursor destacado in-band (reverse
  video), sem truncar nem padding do texto.

## 4. Requisitos de segurança deste milestone

- [x] Redação antes do paint **mantida**: projeções seguem usando `redact.Redact`; conteúdo
      de painel NUNCA é montado com texto não-redigido (regressão testada: token cru na sessão
      não vaza para `View`).
- [x] `tuiGate` continua o único caminho de aprovação; teclas de navegação/rolagem não
      disparam turno; `PgUp`/`PgDn`/`Tab` com gate aberto não interferem no diálogo (só
      `y/n/d/C-c`).
- [x] `internal/*` sem Bubble Tea e sem imports de `cmd/*` (enforcement do M8/M9 mantido).
- [x] Sem `--tui` → REPL default byte-estável (regressão M8/M9).

## 5. Testes obrigatórios

### Funcionais

- [x] `TestPanelFrameDrawsBorders`: moldura correta (`┌┐└┘┬`) + títulos + conteúdo, em 60×12.
- [x] `TestViewportForClamps`: offset 0 → delta negativo fica em 0; delta positivo sobre o topo;
      volta para `total-visible` no fim; `total <= visible` → offset 0 invariante.
- [x] `TestScrollThumb` + `TestScrollbarColTrackAndThumb`: proporção do thumb (início/fim/…) e
      trilha `░`; coluna em branco quando `total <= visible`.
- [x] `TestModelTabCyclesFocus`, `TestModelArrowsScrollFocusedPanel`,
      `TestModelPgUpDownScrollFocusedPanel`, `TestModelArrowsOnPromptKeepHistory`: `Tab`/`←`/`→`
      trocam foco; `↑/↓/PgUp/PgDn/Home/End` rolam o painel focado e NÃO alteram o input; no
      prompt, `↑/↓` mantêm histórico (regressão M8).
- [x] `TestPanelFrameFocusedTitleHighlighted`: exatamente um painel com destaque ANSI.
- [x] `TestPromptLineKeepsFullTextWithCaretInside`: cursor no meio não trunca/padding o texto.
- [x] `TestPanelWidthsKeepRatio`: `panelWidths(100) = 60,40` e soma 100.

### Segurança

- [x] **Token cru na sessão não aparece na `View`** (`TestModelViewHidesRawSecret` — falha se a
      proteção for removida).
- [x] **Tecla de navegação com gate aberto é inócua** (`TestModelTabOnGateIsInnocuous`).
- [x] **Teste arquitetural**: `internal/*` sem `bubbletea`/`cmd/*` continua passando.

## 6. Critérios de aceite (DoD)

- [x] Suíte completa verde no host serial (16 pacotes, 0 falhas), `gofmt`/`go vet` limpos,
      `GOAMD64=v1` build ok (`dist/ancientcode`, 8.458.400 bytes, versão `0.10.0-m10`).
- [x] WSL `-race` 16/16 (Go 1.27, toolchain baixado; nenhuma data race).
- [x] Smoke no Inspiron (pty real + NIM, binário `0.10.0-m10`): moldura completa
      (`┌┐└┘┬┴│`), scrollbar presente quando o conteúdo transborda (verificado com terminal de
      11 linhas), marcador de foco `▸`, títulos, rodapé; **gate `y`** → `write_file executed
      policy=ask decision=allow` + arquivo criado; **gate `d`** → `status=denied decision=denied`
      + arquivo não criado. Artefatos de teste removidos.
- [x] `docs/DECISOES.md` (D-50) atualizado; `AGENTS.md` atualizado; `docs/MANUAL.md` seção TUI
      atualizada; nota de sessão criada.
- [x] Commit padronizado `feat(M10): ...` no branch `m10-tui-paineis`.