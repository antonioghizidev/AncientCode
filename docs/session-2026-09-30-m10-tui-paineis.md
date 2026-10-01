# Sessão 2026-09-30 — m10-tui-paineis

> Resumo exclusivo desta sessão. Método: `specs/methodology.md`. Branch `m10-tui-paineis`.

## Objetivo da sessão

O usuário aprovou o design do M9 mas miserou a entrega: esperava a UI lazydocker de verdade —
**painéis fixos com bordas desenhadas, scrollbar e foco navegável entre painéis**. O M9 entregou
as duas colunas como texto corrido, sem moldura, sem viewport e sem troca de painel por teclado.

Escopo decidido com o usuário: **sessão à esquerda (~60%) / atividade à direita (~40%)**;
**`Tab` cicla foco**; `↑↓/PgUp/PgDn/Home/End` rolam o painel focado.

## O que foi feito

- **Decisão registrada** (`docs/DECISOES.md`, D-2026-09-30-50): sem dependência nova — bordas
  box-drawing Unicode + ANSI via stdlib (rejeitado `lipgloss`); segurança inalterada.
- **Spec nova** `specs/milestones/10-m10-tui-paineis.md`.
- **Projeções puras** (`tui_panels.go`, stdlib, testáveis sem TTY):
  - `renderPanels` — moldura dupla com borda vertical compartilhada (`┬`/`┴`), títulos no topo;
  - `panelWidths`/`panelInnerWidths` — 60/40 com soma exata da largura;
  - `viewportFor` — offset clampado em `[0, total-visible]`;
  - `scrollThumb` + `scrollbarCol` — trilha `░` com thumb `█` proporcional; coluna em branco
    quando o painel não rola (a largura do texto nunca salta);
  - `panelTitle` — marcador `▸` + cor ciano no painel focado (legível sem suporte a cor);
  - `promptLine` — cursor desenhado **in-band** (reverse video).
- **Estado e teclas** (`tui.go`): `height` (via `WindowSizeMsg`), `focus`, `sessionOff`,
  `activityOff`; `Tab`/`Shift+Tab`/`←`/`→` ciclam foco; `↑↓`/page/home/end rolam o painel
  focado; digitar devolve o foco ao prompt; `followSessionBottom` mantém o painel no rodapé
  quando o usuário já estava lá.
- **Defeito corrigido no caminho**: o `promptLine` do M9 escrevia o texto inteiro e *depois*
  acrescentava espaços do cursor — com o cursor no meio da linha o texto ficava errado. Agora o
  caret é desenhado sobre o caractere sob ele (`TestPromptLineKeepsFullTextWithCaretInside`).

## Testes

| Pacote | Foco | Status |
|---|---|---|
| `cmd/ancientai` | M10 (viewport, scrollbar, foco, navegação, prompt) | ✅ |
| Host serial (16 pacotes) | suíte completa | ✅ 0 falhas |
| `gofmt` / `go vet` | estático | ✅ |
| `GOAMD64=v1` build | `dist/ancientai` 8.458.400 bytes, `0.10.0-m10` | ✅ |
| WSL `-race` | 16/16 | ⏳ pendente |
| Smoke Inspiron | pty real + NIM | ⏳ pendente |

## Critérios de aceite

| Critério | Status |
|---|---|
| Painéis com moldura, scrollbar e foco navegável | ✅ |
| `viewportFor`/`scrollThumb`/`scrollbarCol`/`panelWidths` cobertos por testes | ✅ |
| Gate `y/n/d/C-c` inalterado; navegação inócua com gate aberto | ✅ |
| Segredo cru nunca vaza na `View` | ✅ (`TestModelViewHidesRawSecret`) |
| `internal/*` sem Bubble Tea / `cmd/*` | ✅ (teste arquitetural) |
| WSL `-race` + smoke Inspiron | ⏳ |

## Próximos passos

1. `go test -race -count=1 ./...` no WSL.
2. Deploy do binário `0.10.0-m10` no Inspiron + smoke em pty real (responder OSC11 + DSR,
   Enter `\r`): bordas/scrollbar, `Tab`/setas, gate `y`/`d` auditado, `/comandos`.
3. Commit `feat(M10): ...`.

## Observações / riscos

- **SmarTTY (Sysprogs)** foi instalado no host para inspeção visual, mas **não serve para teste
  automatizado**: é cliente SSH gráfico, sem `send-keys`/`capture-pane`. O smoke automatizado
  continua no driver pty Python; para conferência visual, o SmarTTY abre uma sessão real com
  emulador de terminal de verdade.
- `gofmt -l` no host acusa vários arquivos pré-existentes por CRLF de checkout (artefato); o
  gate real de formatação roda no WSL.