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
| `cmd/ancientcode` | M10 (viewport, scrollbar, foco, navegação, prompt) | ✅ |
| Host serial (16 pacotes) | suíte completa | ✅ 0 falhas |
| `gofmt` / `go vet` | estático | ✅ |
| `GOAMD64=v1` build | `dist/ancientcode` 8.458.400 bytes, `0.10.0-m10` | ✅ |
| WSL `-race` (16 pacotes) | detector de corrida | ✅ 0 races |
| Smoke Inspiron | pty real + NIM | ✅ |

## Critérios de aceite

| Critério | Status |
|---|---|
| Painéis com moldura, scrollbar e foco navegável | ✅ |
| `viewportFor`/`scrollThumb`/`scrollbarCol`/`panelWidths` cobertos por testes | ✅ |
| Gate `y/n/d/C-c` inalterado; navegação inócua com gate aberto | ✅ |
| Segredo cru nunca vaza na `View` | ✅ (`TestModelViewHidesRawSecret`) |
| `internal/*` sem Bubble Tea / `cmd/*` | ✅ (teste arquitetural) |
| WSL `-race` + smoke Inspiron | ✅ |

## Smoke no Inspiron (pty real, NVIDIA NIM, binário `0.10.0-m10`)

Deploy em `/home/ancientcode/ancientcode-0.10.0-m10` (`0.10.0-m10`, GOAMD64=v1). Driver pty Python
(fora do repo) respondendo as queries OSC11 + DSR, Enter = `\r`, janela 100×30 (e 100×11 para
forçar overflow de scrollbar):

| Verificação | Resultado |
|---|---|
| Moldura completa | `┌┐└┘┬┴│` presentes no stream |
| Títulos dos painéis | `sessão` + `atividade` |
| Marcador de foco | `▸` presente após `Tab` |
| Scrollbar | presente com conteúdo > viewport (terminal de 11 linhas) |
| Rodapé | `ctx …` + `0.10.0-m10` |
| Gate `y` | audit `write_file status=executed policy=ask decision=allow`; arquivo criado |
| Gate `d` | audit `status=denied decision=denied`; arquivo NÃO criado |

Nota: a primeira tentativa de gate usou caminho **absoluto** e a tool recusou
(`status=error`, `decision=allow`) — comportamento fail-closed esperado desde o M6
(D-2026-09-16-33: arquivos recusam caminho absoluto). Repetido com caminho relativo: executado.

Artefatos de teste (`smoke-m10.txt`, `smoke-m10-veto.txt`) e drivers temporários removidos.

## Próximos passos

1. Abrir PR `m10-tui-paineis` → `main`.
2. **Backlog Web** (SSE + shell-HTTP + `Approver` por canal + auth/TLS/CSRF) como milestone
   futuro — o contrato M8/M10 já o permite sem tocar em `internal/loop`.

## Observações / riscos

- **SmarTTY (Sysprogs)** foi instalado no host para inspeção visual, mas **não serve para teste
  automatizado**: é cliente SSH gráfico, sem `send-keys`/`capture-pane`. O smoke automatizado
  continua no driver pty Python; para conferência visual, o SmarTTY abre uma sessão real com
  emulador de terminal de verdade.
- `gofmt -l` no host acusa vários arquivos pré-existentes por CRLF de checkout (artefato); o
  gate real de formatação roda no WSL.