# Sessão 2026-10-02 — m13-painel-sessao

> Branch `m13-painel-sessao`, a partir de `main`=`efeee64` (M12 mergeado, PR #5).
> Método: `specs/methodology.md`. Spec: `specs/milestones/13-m13-painel-sessao.md`.

## Objetivo da sessão

Pedido do operador (2026-10-02), em três partes mais uma decisão de geometria:

1. **Painel de sessão** cortava prompt e resposta — "inviabilizava o propósito".
2. **Animação**: com a tela estática ele não distinguia "processando" de "abortado" de
   "congelado".
3. **Interromper, corrigir, continuar** sem redigitar.
4. Reduzir o painel de atividade (não precisa de 40%) — **e dar quebra de linha a ele também**.

## Diagnóstico: a perda era dupla, e a pior parte era invisível

O exemplo reportado foi este:

```
│> verifique o hardware deste computador e me informe: Chipset, processador, memória ram, swap, e armaz
│< Aqui está o resumo do hardware que consegui levantar neste sistema:## Processador- **M…
```

Duas perdas numa linha só. No código:

| Local | O que fazia | Consequência |
|---|---|---|
| `tui_panels.go:553` | `runeCap(..., sessBody)` — corte no paint | linha cortada na largura do painel |
| `tui.go:647` | `truncatedLine` corta a `lineCap = 90` **no append** | **texto morto**: nenhuma correção no paint recupera |
| `tui_panels.go:216` | 60/40 com `min = 12` | ~55 células para a conversa; 12 para o catálogo do M12 |

O segundo é o que importa: corrigir só o desenho não devolveria nada.

## Entregue

- **`panelLines`** — projeção única compartilhada por sessão, atividade, picker e gate. Quebra por
  caractere, continuação indentada (`  `), prefixo `> `/`< ` preservado na primeira linha física.
  O invarianté testado é "nenhum caractome some" (o espaço no ponto de quebra é o único custo).
- **`truncatedLine` removido** — o modelo guarda o texto inteiro; o corte é da projeção.
- **Offset em linhas físicas** — com offset lógico, uma resposta de 40 linhas contava como *uma* e
  `↓` pulava a resposta inteira: exatamente a reclamação que abriu o milestone.
- **Geometria 70/30** com **piso de 28 células** para a atividade e soma exata preservada.
- **Rolagem horizontal** (`Shift+←/→`) + indicador `col 40/120` no título: caminho, URL e JSON de
  uma linha não têm ponto de quebra.
- **Spinner Braille** (`⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`) via `tea.Tick` de 100 ms **só enquanto `inTurn`**, com
  degradação ASCII (`|/-\`) e detecção de `TERM`. Um spinner **parado** é pior que nenhum: confirma
  o congelamento que o operador quer excluir.
- **`Esc` cancela** o turno em voo (sem turno, não sai), **devolve o prompt** ao input com cursor no
  fim, e `/continue` reenvia o cancelado **avisando quais tools já executaram**.
- **`PhaseTurnCancelled`** — estado terminal próprio no enum de fases (append-only).

## Defeitos encontrados no caminho (todos com teste de regressão)

| # | Defeito | Origem |
|---|---|---|
| 1 | **`View()` mutava `m.activity`**: prefixava o spinner a cada frame (~10/s), empurrando o log de eventos para fora. Durante um turno o painel ficava só com spinner. | M12 (mesma classe do `m.activityOff = 0`) |
| 2 | **`sanitize` comia o `\n`** (é C0): saída multilinha de tool e respostas com parágrafo eram **colapsadas numa linha só**. | M11 |
| 3 | **`handleRunes` engolia `r` e `c`** quando havia banner: "verifique" virava "veifique". O banner aparece depois de erro/cancelamento — ou seja, atingia o caminho de recuperação que o M13 criou. | M9 |
| 4 | **Gate cortava o payload em 2 linhas** com reticências, contradizendo a doutrina do M11. | M11 |
| 5 | **`fixedRows` reservava 1 linha** para um diálogo que renderizava 3: com o gate aberto o prompt e o rodapé saíam da tela. | M11 |
| 6 | **Rodapé fatiando bytes** (`line1[:lineCap-2]`): `✗ erro` é multibyte, podia cortar UTF-8 no meio. | M10 |
| 7 | **Cancelamento pintado como erro** vermelho (`context canceled`), dizendo ao operador que a ação deliberada dele quebrou algo. | novo |

## Gate

| Verificação | Resultado |
|---|---|
| Host serial | **17/17 pacotes, 0 falhas** (127 testes em `cmd/ancientcode`) |
| WSL `-race` (sequencial por pacote) | **17/17, 0 races** |
| `go vet` | limpo |
| `gofmt` (visão LF da árvore) | limpo |
| `GOAMD64=v1` build | 8.548.512 bytes, `0.13.0-m13` |

> Nota de método: `go test -race ./...` em paralelo sobre `/mnt/d` (mount Windows) estourou o
> orçamento do pacote `cmd/ancientcode` (900 s) embora os testes passem em segundos isolados. O gate
> de corrida passou a rodar **sequencial por pacote** — mesma cobertura, sem o artefato de contenção.

### Testes novos

- `cmd/ancientcode/m13_panels_test.go` — projeção (sem perda, prefixo, indentação, larguras
  degeneradas), geometria 70/30 com soma exata e piso, `hscrollRange`, frames do spinner.
- `cmd/ancientcode/m13_turn_test.go` — `Esc` cancela e não sai, devolução do prompt, tools que já
  rodaram, `/continue` uma vez e fail-closed, spinner só durante o turno, cancelamento não é erro,
  histórico de cancelamentos limitado, **digitação nunca comida pelo atalho**.
- `cmd/ancientcode/m13_view_test.go` — o prompt e a resposta longos **inteiros no `View()`**, cauda
  alcançável por rolagem, `sanitize` preserva `\n`, saída multilinha mantém uma linha por linha,
  `View()` não altera o log de atividade.

## Smoke no Inspiron (Core 2 Duo sem AVX, pty real, `0.13.0-m13`)

| Verificação | Resultado |
|---|---|
| Prompt de 152 caracteres | renderizado em **3 linhas físicas**, com continuação indentada |
| Atividade estreita (30%) | quebra junto, inclusive o erro do provider |
| Spinner | **frames distintos** capturados no stream (`⠋⠏⠇⠧⠦⠴⠙⠹⠸⠼`) |
| `Esc` em voo | cancela o contexto, registra `! turno cancelado (Esc) — edite e reenvie` |
| Cancelamento ≠ erro | nenhum banner/linha `! erro: context canceled` |
| Prompt devolvido | linha de input volta com o prompt e o cursor no fim |
| Gate aberto | aparece com o payload; **linha de input continua visível** |

**Conclusão do smoke: 9/11.** As 2 falhas são a **mesma célula** ("tamanho" chega "tamano") e são
**artefato do driver**, não do produto: para destravar o input no pty cru o driver precisa
responder as queries do terminal (OSC11/DSR), e no master do pty **essas respostas entram como
entrada** — a linha de input aparece literalmente com `]11;rgb:1e1e/…`. Um probe dedicado
(`charprobe.py`) provou que a digitação não perde letras.

### Não validado em hardware

- **Integridade bit-a-bit do prompt no caminho completo.** O canal limpo seria `--run` com pipes
  (`run_integrity.py`), sem emulador de terminal — mas o cold start do NIM no Core 2 Duo passou de
  **600 s** neste ambiente, então ficou inconclusivo. A prova equivalente está no host:
  `TestViewShowsTheWholeLongPrompt` exige `tamanho` presente no `View()`.
- **Camada 3 do `/models` (`--check`)** continua pendente do M12: pede confirmação e não executa os
  probes. Anotado como dívida conhecida.

## Dívidas e backlog

- `/compact`, `/new`, `/autocompact` em paridade com o opencode: **especificados** na seção 10 da
  spec do M13, **não implementados** (a especificação completa fica para a próxima sessão).
- `--check` (camada 3 do catálogo) ainda não faz os requests reais.
- `/reset` continua sendo o comando que **apaga** o `.jsonl`, ao lado de `/new` (que vai preservar).
  Candidato a renomear.

## Documentos

DECISOES D-2026-10-02-56..61 · spec M13 · `docs/MANUAL.md` §4.1 (teclas novas) · `AGENTS.md`.
