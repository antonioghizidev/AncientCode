# M13 — Painel de sessão legível, animação de atividade e controle do turno

**Status**: ⏸ pendente (especificado a pedido do usuário em 2026-10-02, após o M12)
**Depende de**: M12 (`m12-catalogo-modelos`)
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Tornar o **painel de sessão** da TUI capaz de mostrar prompts e respostas longas **por inteiro**,
tornar o estado "o modelo está trabalhando" **perceptível sem leitura**, e dar ao operador
controle sobre o turno em andamento: **interromper, corrigir o prompt e continuar de onde parou**.

O painel de **atividade** sofre do mesmo corte e também ganha quebra de linha — e deixa de ocupar
40% da largura, porque 40% para linhas curtas de evento é espaço desperdiçado quando a conversa é o
que precisa de espaço.

Hoje a sessão é um log truncado e estático: em 60% da largura, com 40% reservado à atividade, uma
resposta de shell como `nproc` + `lscpu` + `free -h` não cabe — e o operador não tem como saber
nem ler o resto, nem se o turno está andando, nem se congelou.

> **Escopo apenas de UI.** Nada aqui pode afrouxar gate, sanitização, redação ou o teto de
> iterações do loop (`internal/loop`). Cancelar um turno é **o mesmo** `turnCancel` que já existe.

## 2. motivating example (observado no Inspiron, 2026-10-02)

```
│> verifique o hardware deste computador e me informe: Chipset, processador, memória ram, swap, e armaz
│< Aqui está o resumo do hardware que consegui levantar neste sistema:## Processador- **M…
```

Duas perdas de informação em **uma** linha: o prompt é cortado no meio de "armazenamento" e a
resposta perde o sumário inteiro. O painel não oferece rolagem lateral nem quebra — o texto
**inexistente** é irrecuperável.

## 3. Causa raiz (medida no código, não estimada)

| Local | Comportamento | Efeito |
|---|---|---|
| `cmd/ancientai/tui_panels.go:553` | `runeCap(lineAt(sess, sessOff+i), sessBody)` — e o mesmo para `act` | **corte duro** na largura do painel; sem `…`, sem marcação, **nos dois painéis** |
| `cmd/ancientai/tui.go:647` | `truncatedLine` corta a `lineCap = 90` runes **no append** | perda **irreversível**: o texto nunca chega ao painel |
| `cmd/ancientai/tui_panels.go:525` | `renderPanels` recebe `[]string` já achatado | o painel é um viewport de 1 linha = 1 linha lógica |
| `cmd/ancientai/tui_panels.go:216` (`panelWidths`) | 60/40 fixo, `min = 12` | sessão com ~55 células: pouco mais que 2/3 do terminal para a conversa; e o piso de 12 é curto demais para o catálogo do M12 |
| `cmd/ancientai/tui.go:681` | nenhum ticker; `m.statusTone()` é estático por fase | sem sinal temporal de "trabalhando" |
| `cmd/ancientai/tui.go:214-223` | `Ctrl+C` cancela, mas o texto do prompt foi descartado no submit | para retomar, o operador **retypes** o prompt |

Ou seja: a falha do item 1 é **dupla** (corte no append + corte no paint) e o pior deles é o
primeiro — corrigir só o paint **não** recupera o que já foi perdido.

## 4. Escopo / entregáveis

### 4.1 Ambos os painéis leem inteiro (quebra de linha)

Vale para o painel de sessão **e** para o de atividade. O motivo é o mesmo nos dois: a atividade
carrega payloads longos (argumentos redigidos de `shell`, saída de tool, erro de provider) e perder
a cauda de um erro é perder a causa.

- [ ] **Projeção única e compartilhada**: `panelLines(lines []string, width int) []string` quebra
      sessão e atividade. Uma implementação, um conjunto de testes, nenhum drift entre os dois.
- [ ] `sessionLines()` passa a usar `panelLines()`; o picker (`pickerLines`) e o gate (`gateLines`)
      também, porque nomes de modelo do M12 e detalhe de tool sofrem do mesmo corte.
- [ ] **Indentação de continuação**: a quebra marca a continuação (2 espaços ou `↳`) para o operador
      distinguir continuação de novo parágrafo.
- [ ] **Preservar o prefixo semântico**: `> prompt`, `< resposta` e as linhas de evento mantêm o
      marcador; a quebra acontece **depois** dele.
- [ ] **Remover `truncatedLine` no append** (`tui.go:647`): o `m.session` guarda o texto **inteiro**;
      o corte passa a ser responsabilidade **exclusiva** da projeção.
- [ ] `lineCap` deixa de existir como teto **silencioso**; se algum teto for mantido, ele é aplicado
      no **append** com marcação visível (`… [N chars]`) e é configurável.
- [ ] **O gate humano não pode ser afetado**: o detalhe do gate já usa `wrapText` com `maxLines`
      (tui_panels.go:355) — Approvar exige ver o payload **completo**. Este item **não** pode reduzir
      a visibilidade do gate nem aumentar `maxLines` do gate acima do que a tela comporta.

### 4.2 Geometria: a atividade deixa de ocupar 40%

Medido no Inspiron: com sessão em 60% (~55 células úteis) e atividade em 40%, **nenhuma** das duas
lê um resumo de hardware com folga. A atividade não precisa de 40% para cumprir o papel dela — ela
mostra linhas curtas de evento (`tool ok 0.00s`, `gate: shell`), o spinner e, quando aberto, o
catálogo do M12.

- [ ] **Sessão 70% / atividade 30%**. `panelWidths`/`panelInnerWidths` já exigem soma exata: a
      mudança é ajuste de proporção, não uma exceção ao invariante.
- [ ] **Clamp por largura absoluta**: a atividade tem **mínimo** de ~28 células (o catálogo do M12 e o
      detalhe do gate precisam de largura para não virar pilha de `↳`) e a sessão fica com o
      restante. Proporção pura não pode gerar coluna de 4 células em terminal estreito.
- [ ] O scrollbar e o indicador `col N/M` continuam com 1 célula, para caber na coluna menor.
- [ ] Teste que falha se a soma deixar de ser exata **ou** se alguma coluna cair abaixo do mínimo.

### 4.3 Rolagem lateral (defensiva, não substitui a quebra)

- [ ] `Tab`/`←→`/`Home`/`End` já alternam foco e rolam **verticalmente**. Acrescentar rolagem
      **horizontal** por painel (`Shift+←/→`, ou `h`/`l`), necessária para: linhas sem espaço
      (caminho longo, URL, JSON de uma linha) que a quebra não desfaz.
- [ ] Indicador de posição horizontal junto ao scrollbar vertical (`col 37/120`), senão o operador
      não descobre que existe mais texto à direita.

### 4.4 Animação de atividade

- [ ] Spinner **na linha de status do painel de atividade**, frames `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` (Braille),
      com `tea.Tick` de ~100 ms **apenas enquanto `m.inTurn`** — zero CPU parado.
- [ ] Movimento **é vida**: o operador distingue "processando" de "congelado" pelo **movimento
      vertical do texto** das fases (`modelo: aguardando` → `recebendo tokens`), não só por glyph.
- [ ] Fim do turno → spinner **desaparece** (sem frame residual) e o rodapé volta ao tom anterior.
- [ ] **Degradação honesta**: se `TERM` não suportar os frames, mostra alternância ASCII
      (`|/-\`); nunca um spinner **parado**, que seria pior que nenhum.
- [ ] **Segurança**: o frame é ASCII/Unicode de fonte fixa, nunca conteúdo remoto; `sanitize`
      continua aplicado a toda linha.

### 4.5 Interromper, corrigir, continuar

- [ ] **`Esc` cancela o turno em andamento** (hoje só `Ctrl+C` faz). Sem turno, `Esc` **não** sai da
      aplicação — trocar `Esc` por "sair" seria uma regressão de gesto.
- [ ] **Cancelar devolve o prompt**: `m.input` é reposto com o texto exato do turno cancelado, cursor
      no fim, e a linha `! turno cancelado — edite e reenvie` entra na sessão. Hoje o prompt é
      **descartado** e o operador retyped tudo.
- [ ] **`Ctrl+R` / `↑` para reeditar**: navegar `promptHist` já existe (`histStep`, tui.go:485) e hoje
      é **preenchimento**; passa a ser **reedição** — o texto volta ao input editável, e o envio
      **não** duplica a entrada no histórico.
- [ ] **`continue` após cancelamento acidental**: `/continue` reenvia o **último prompt** cujo turno
      foi cancelado, sem retipar. Não reenvia se o turno **completou** (senão duplica efeitos de
      tools).
- [ ] **`Ctrl+C` mantém o comportamento atual**: 1º = cancela o turno; 2º (sem turno) = sai. Não
      inventar "cancelar e sair" num toque só.
- [ ] **Auditoria**: cancelamento é registrada como evento observável (`PhaseTurnCancelled`), para que
      `abortado` e `congelado` fiquem distinguíveis **no audit** e não só na tela.

## 5. Contratos (interfaces públicas)

```go
// projeção pura (testável sem terminal) — compartilhada pelos dois painéis
func panelLines(lines []string, width int) []string    // quebra + indentação de continuação
func sessionLines(sess []string, width int) []string  // aplica panelLines ao painel de sessão
func hscrollRange(total, width, off int) (lo, hi int)  // janela horizontal, clampada
func spinnerFrame(n int, ascii bool) rune
func panelWidthsFor(total int) (sess, act int)         // 70/30 com clamp de mínimo

// estado
type tuiModel struct {
    // ... existentes
    hoff        map[panelID]int // rolagem horizontal por painel
    frame       int             // contador do spinner (par, não renderizado)
    cancelled   []cancelInfo    // turnos cancelados, para /continue
}

// eventos (novos, no vocabulário de D-35)
PhaseTurnCancelled   // turno interrompido pelo operador
```

Sem novos campos em `Approver`, sem mudança em `internal/loop`, sem dependência nova: Bubble Tea
já entrega `tea.Tick`, e Braille é texto.

## 6. Requisitos de segurança deste milestone

- [ ] **Nenhum texto não confiável pula `sanitize`/`redact`**. A quebra de linha opera sobre a
      projeção **já** redigida; o conteúdo remoto nunca é reescrito para caber.
- [ ] **Gate não pode ficar mais ilegível** que hoje: o payload do Approver permanece completo na
      tela. Teste que falha se o gate passar a truncar silenciosamente.
- [ ] **O rodapé `>`80% continua mandão**: quebra de linha não pode sequestrar altura do painel de
      atividade sem respeitar `fixedRows`.
- [ ] **Cancelar não deixa tool órfã**: o `turnCancel` é o mesmo do M9/M11; nada de estado meio
      executado. Turno cancelado **não** é sucesso no audit.
- [ ] **`/continue` é fail-closed**: sem histórico de cancelamento, responde erro e **não** reenvia
      nada.
- [ ] **Spinner não vaza segredo**: frames são literais do código, nunca derivados de resposta.

## 7. Testes obrigatórios

### Funcionais
- [ ] Prompt de 400 caracteres em painel de ~70 células: **nenhum** carácter perdido após a projeção
      (concatenar as linhas físicas, tirando indentação, == linha lógica).
- [ ] **Atividade**: linha de evento de 200 caracteres (saída de tool) quebra em N linhas e a
      concatenação devolve o texto inteiro — sem `…` silencioso.
- [ ] Resposta multilinha com `\n` preservado: a contagem de linhas lógicas não muda.
- [ ] **Geometria**: `panelWidthsFor` devolve soma exata em 40, 80, 100, 200 colunas; nenhuma coluna
      abaixo do mínimo (~28 para a atividade) em terminal estreito.
- [ ] `Ctrl+C` em voo → prompt **de volta** no input, cursor no fim, linha de aviso na sessão.
- [ ] `Esc` em voo cancela; `Esc` **sem** turno não sai.
- [ ] `↑` no input recupera o prompt editável; reenviar **não** duplica `promptHist`.
- [ ] `/continue` após cancelamento reenvia **uma** vez; a segunda recusa com "nada a continuar".
- [ ] Spinner avança a cada tick **apenas** com `inTurn`; frame seguinte some ao fim do turno.
- [ ] `TERM=dumb` → spinner ASCII, nunca travado.
- [ ] Rolagem horizontal em linha sem espaços (caminho de 200 chars) revela o fim com `End`.

### Segurança (obrigatórios — devem falhar se a proteção for removida)
- [ ] `TestPanelProjectionNeverDropsUntrustedContent`: `\x1b[2J` num payload longo **não** escapa da
      projeção e **não** reaparece no `View()` — vale para **os dois** painéis.
- [ ] `TestGateStillShowsFullPayloadAfterWrap`: remover a quebra **não** pode truncar o detalhe do
      gate (falha se alguém "economizar" linhas no gate para caber a animação ou a nova geometria).
- [ ] `TestPanelWidthsNeverBelowMinimum`: largura de terminal estreito não gera coluna ilegível.
- [ ] `TestCancelIsAuditedNotCountedAsSuccess`: turno cancelado gera evento de cancelamento e
      **nenhum** `tool` executado é marcado como concluído.
- [ ] `TestContinueRefusesWithoutCancellation`.
- [ ] `TestSpinnerNeverContainsPayload`: nenhum frame contém texto de resposta.

## 8. Critérios de aceite (DoD)

- [ ] Prompt e resposta longos **legíveis por inteiro** sem corte silencioso, **nos dois painéis**
      (verificado em pty real no Inspiron, não só em teste).
- [ ] Atividade em ~30% da largura com quebra de linha funcional, e com piso que não a torna
      ilegível em terminal estreito.
- [ ] Operador distingue "processando" / "parou" / "congelou" **sem ler texto**.
- [ ] Cancelar → corrigir → reenviar **sem redigitar**, no REPL e na TUI.
- [ ] **Todos os testes passam**: `go test -count=1 ./...` e `go test -race -count=1 ./...` (WSL).
- [ ] `gofmt`/`go vet`/`go build` (`GOAMD64=v1`) limpos.
- [ ] `docs/DECISOES.md` atualizado; nota de sessão; `docs/MANUAL.md` com as teclas novas.
- [ ] Smoke no Inspiron: sessão com resposta longa legível, spinner visível em pty, `Esc` cancela,
      prompt retorna, `↑` reedita.

## 9. Fora de escopo

- **`undo` completo de prompts já executados** (como o "edit previous message" do Claude Code /
  opencode): reenviar um prompt antigo **repete efeitos de tools** sem rollback. Fica para fase
  própria, com semântica de rebranching explícita e aviso ao operador — **não** neste milestone.
- Reformatar o painel como `bubbletea/viewport` com scroll nativo (mudança grande de layout; a
  projeção pura é testável e cabe no orçamento de 4 GB).

> A proporção dos painéis **entrou** neste milestone (item 4.2, 70/30) a pedido do usuário.

## 10. Backlog anotado (próxima sessão de specs, **não** especificar agora)

Levantado pelo usuário em 2026-10-02 durante o M13. **Nada aqui deve ser implementado no M13**,
mesmo que pareça simples: mexer em histórico e orçamento de contexto **durante** a mudança de layout
mistura duas causas e nenhuma delas fica verificável. Anotado para a sessão de specs seguinte.

### 10.0 Separação conceitual: **histórico** ≠ **contexto**, e ≠ **sessão**

O usuário está certo, e a medição no código **fortalece** o argumento. Hoje os dois termos são
confundidos na nomenclatura, e é por isso que `/compact`, `/new` e `--cleanup` parecem a mesma
coisa quando não são.

| | **Histórico** | **Contexto** |
|---|---|---|
| **O que é** | o registro append-only do que aconteceu | o recorte do histórico que vai **no request** ao modelo |
| **Onde vive** | `sessions/<id>.jsonl` (`model.Store`, `internal/model/store.go`) | `reduceHist(Load())`, recalculado **a cada turno** |
| **Persiste** | sim, até `--cleanup`/`--reset` | **não** — é derivado, nunca salvo |
| **Perder é** | perder registro | só liberar espaço |
| **Hoje é controlado por** | `--cleanup` (apaga por mtime), `--reset`/`/reset` (`store.Reset()` = `os.Remove`) | `session_context_bytes` (0 = off) |

**Descoberta que reforça a separação:** `internal/loop/loop.go:185`, `:202` e `:266` carregam o
histórico **do disco a cada turno** e aplicam `reduceHist` em seguida. Ou seja, **o contexto não é
estado — é função do histórico**. Não existe "contexto acumulado" em lugar nenhum.

### 10.0.1 `/new` **idêntico ao do opencode** — o que isso implica (decisão do usuário, 2026-10-02)

O usuário pediu explicitamente paridade com o opencode. Consultando a documentação do opencode
(`dev.opencode.ai/docs/tui`, `docs/cli`) e o fonte (`opencode-ai/opencode`), o comportamento real é:

- `/new` — "Start a new session", **alias `/clear`**, keybind `ctrl+x n` (na TUI web, `Ctrl+N`).
- `/compact` — "Summarize the current session and **create a new one with the summary**".
- `/sessions` — "List and switch between sessions", **aliases `/resume` e `/continue`**.
- `--fork` (CLI) — cria uma sessão nova a partir de uma existente, para não destruir a original.
- autocompact — dispara por uso de contexto (≈95% da janela) e **também cria sessão nova** com o
  resumo; desativável por `OPENCODE_DISABLE_AUTOCOMPACT`.

**A correção que isso traz:** eu tinha deixado em aberto a pergunta "o `/new` mantém a mesma sessão
lógica ou abre uma sessão nova?". A resposta é **sessão nova** — e isso vale para os **três**
comandos. No opencode, a unidade é a **sessão**, e o contexto é derivado dela.

Consequências para o AncientAI, que **já tem a peça que falta**: `--resume <id>` (M5) reutiliza um
`.jsonl` existente. Então:

- [ ] `/new` **cria uma sessão nova** (`sessions/<id>.jsonl`), não apaga nada. A anterior continua
      intacta e recuperável.
- [ ] `/new` **não** é "limpar o contexto da sessão atual": é "começar outra sessão". Isso elimina a
      necessidade da **âncora persistida** que eu tinha derivada antes (§10.0) — não há âncora
      porque o arquivo **muda**. `/new` é então trivialmente reversível, o que é o ponto.
- [ ] Precisa existir o par **listar/trocar de sessão** (`/sessions`, alias `/resume`), senão o
      operador cria a sessão nova e **perde o acesso** à antiga — o que seria uma perda de
      registro, exatamente o que o usuário quer evitar. **Este é o entregável de maior risco.**
- [ ] `/compact` também cria sessão nova, com o resumo — coerente com o opencode, e significa que
      o resumo **é** o primeiro turno da sessão nova (sem camada de derivação separada).
- [ ] `Ctrl+N` como keybind de `/new`, seguindo o padrão do opencode.
- [ ] `/reset` (o que hoje **apaga** o `.jsonl`) passa a ser o comando perigoso e sem equivalente no
      opencode — considerar renomear para algo inequívoco (`/reset` que apaga é armadilha de
      mnemonic ao lado de `/new` que preserva).

### 10.1 `/compact` — compactar o contexto

Hoje existe **apenas truncamento**: `reduceHist` (`internal/loop/loop.go:278`) chama
`model.ReduceMessages`, que **descarta** turnos antigos quando `session_context_bytes > 0` (0 =
sem truncar, default fail-closed). Isso não é compactar: o operador perde a decisão e a saída sem
nada no lugar, e o modelo recebe um buraco.

- [ ] `/compact` produz um **resumo** do histórico e **substitui** os turnos por esse resumo +
      os últimos turnos intactos (âncora recente preservada).
- [ ] O resumo passa pelo **mesmo** caminho de conteúdo não confiável: `redact` + `sanitize`, e a
      marcação de untrusted injetada pelo M6/M7. Resumo **não** pode virar canal de injeção.
- [ ] `/compact` é **explícito** (ação do operador) e **auditado**: o audit registra que houve
      compactação, com bytes antes/depois.
- [ ] **Cria sessão nova com o resumo** (paridade opencode, §10.0.1), em vez de reescrever a atual. O
      `.jsonl` original fica **intacto**: é o que separa `/compact` de `/reset`.
- [ ] **Falha é fail-closed**: se o resumo falhar, a sessão atual permanece intacta e nenhuma sessão
      nova é criada.
- [ ] **Pergunta de design a resolver na spec**: `/compact` pede um LLM extra (custo + latência) ou
      resume localmente por extração (heurística, sem custo, perde nuance)? No opencode é LLM
      (≈95% de gatilho). Provavelmente LLM, com caminho local como degradação.

### 10.2 `/new` — nova sessão (paridade opencode)

Ver §10.0.1: `/new` **cria sessão nova**, preservando a anterior. Não é "limpar o contexto da sessão
atual".

- [ ] `/new` cria `sessions/<novo-id>.jsonl` e passa a apontar para ele. Nada é apagado.
- [ ] Alias `/clear`, exatamente como no opencode.
- [ ] Keybind `Ctrl+N` na TUI e REPL, seguindo o padrão do opencode.
- [ ] Confirmação via gate quando há **turno em andamento** (descartar trabalho em voo é perda real).
- [ ] **Entregável crítico**: `/sessions` para listar e trocar de sessão (alias `/resume`). Sem isso
      o `/new` cria a sessão nova e o operador **perde o acesso** à antiga — o oposto do objetivo.
- [ ] Teste de não-regressão: após `/new`, a sessão anterior **continua em disco e é recuperável**
      com `--resume <id> antigo` (falha se o `os.Remove` do `Store.Reset()` for chamado por engano).

### 10.3 `/autocompact` — compactação automática por tamanho, configurável

- [ ] Mesmo comportamento do `/compact` no opencode: cria **sessão nova** com o resumo (não compacta
      a sessão atual in-place), e deixa rastro visível (linha na sessão + tom no rodapé).
- [ ] Gatilho por **uso de contexto**, não por bytes brutos: `ContextUsage`
      (`internal/loop/interaction.go:76`) já produz `pct` com cap 100. O opencode usa ≈95%.
- [ ] **Ponto de spec obrigatório:** `session_context_bytes` mede **bytes de histórico** e
      `ContextUsage.pct` mede **fração da janela do modelo** — são grandezas diferentes e hoje só a
      primeira está no YAML. O novo limite precisa ser a segunda (ou as duas, com papel explícito).
- [ ] **Ordem entre truncar e resumir** é o ponto delicado: hoje `reduceHist` roda a cada turno e
      **descarta**. Com resumo automático, o resumo precisa acontecer **antes** do descarte, senão o
      operador recebe um buraco em vez de um resumo. **Decisão de spec obrigatória.**
- [ ] Config no YAML, validada no startup como o resto (fail-closed na config insegura):
      `autocompact.enabled`, `autocompact.threshold_pct`, com piso razoável (evitar compactar a cada
      turno em janela pequena).
- [ ] **Nunca compacta o turno em andamento**, e **nunca** durante um gate aberto (o operador está
      decidindo com base no histórico visível — muda-lo debaixo dele é erro).
- [ ] Auto-compactar **durante um turno** é o caso mais perigoso: o gate decide sobre um contexto
      que muda sozinho. Precisa de teste que garanta que o snapshot do gate é estável.
