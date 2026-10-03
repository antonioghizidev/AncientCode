# Sessão 2026-10-01 — m12-catalogo-modelos

> Branch `m12-catalogo-modelos`, a partir de `m11-gate-informado`. Método: `specs/methodology.md`.

## Objetivo da sessão

Pedido do usuário: **listar os modelos disponíveis no provedor e poder escolher um numa lista**.
Com o desenho aprovado em conversa: (1) painel dedicado na TUI; (2) filtrar só o que funciona;
e, acrescentado depois, (3) opção de filtrar só modelos gratuitos.

## Medição que definiu o desenho

Sondado o endpoint real nos dois provedores (Inspiron):

| | NIM | OpenRouter |
|---|---|---|
| Modelos | 81 | 464 |
| Campos | `id`, `object`, `created`, `owned_by` | `id`, `name`, `architecture`, `context_length`, `pricing`, `supported_parameters`, `alias_target`… |

Conclusão: a **mesma** filtragem teria fidelidades muito diferentes. NIM não expõe
capacidades nem preço — logo, "gratuito" e "faz tool calling" são **desconhecidos** lá, e a UI
precisa dizer isso em vez de fingir.

## Abordagem em camadas (D-53)

1. **Grátis** — exclusão de famílias não-chat (embedding/rerank/guard/vision/audio/moderation) por
   nome + curadoria.
2. **Grátis quando existe** — metadados do provedor: `tools`, modalidades de entrada, contexto,
   preço.
3. **Sob demanda** — `--check`: um request real de tool calling por modelo; **pede confirmação**
   porque gasta tokens.

`ToolSupport` é tri-estado (`yes`/`no`/`?`): ausência de metadado nunca vira "sim". É o que impede
oferecer em silêncio um modelo que não faz tool calling.

## Entregue

- **`internal/provider/models.go`** — `ListModels` (`GET {baseURL}/models`), `ModelInfo`,
  `Free()`/`PriceKnown()`/`SupportsTextInput()`. Leitura limitada (4 MiB) e teto de entradas,
  contexto, chave lida por request e **nunca** em erro.
- **`internal/catalog`** (novo, stdlib puro) — `Classify`, `Build` (dedup + descarta alias),
  `Filter`, `Rank` (ativo no topo, verificados antes, alfabético), `FreeFilterAvailable`,
  `Summarize`, `Index`.
- **`internal/loop/catalog.go`** — `ListModels` e `SelectModel` (índice fora da faixa é recusado
  **antes** de qualquer troca).
- **REPL** — `/models [--free|--all|--check|texto]` numerado, `*` no ativo, nota de honestidade
  ("provedor nao informa preco: filtro --free indisponivel aqui"); `/model <N>` seleciona pela
  posição da última listagem.
- **TUI** — `/models` abre **painel dedicado**: digitar filtra, `↑↓` move, `Enter` seleciona,
  `Esc` cancela, `Ctrl+F` alterna gratuitos, contadores no rodapé. Estado de carregamento explícito.

## Defeitos encontrados pelo smoke (corrigidos, todos com teste)

1. **Deadlock que travava a TUI inteira** (D-54). `SwitchModel` chamado de dentro de `Update` →
   `loop.emit` → observer → `tea.Program.Send` → bloqueia a event loop que deveria drenar o canal.
   Confirmado com `SIGQUIT` (stack completo). Corrigido movendo toda troca de modelo/provedor para
   `tea.Cmd`. **`/model` e `/provider` tinham o mesmo bug, latente desde o M8/M9.**
2. **Comando não limpava a linha de input** (D-55): `submit()` só chamava `resetPrompt()` no
   caminho de prompt; após `/model x` o texto ficava e o próximo typed virava `/modelsoi`. Bug do
   M8/M9, invisível no REPL.
3. **Painel substituído herdava o offset de rolagem**: o picker abria rolado, escondendo a linha
   de filtro e o item destacado.
4. **Mentira na UI**: durante o fetch o painel dizia "(nenhum modelo corresponde)" — nada havia sido
   filtrado ainda. Agora diz "carregando catalogo…".

## Testes

| Pacote | Foco | Status |
|---|---|---|
| `internal/provider` | parse, dedup, limite de leitura, sanitização, chave fora de erro | ✅ |
| `internal/catalog` | classificação, precedência, filtros, ranking, `free` só com preço | ✅ |
| `internal/loop` | `ListModels`, fail-closed, `SelectModel` fora de faixa | ✅ |
| `cmd/ancientcode` | parser, teclado do picker, alternância assíncrona, input limpo | ✅ |
| Host serial (17 pacotes) | suíte completa | ✅ 0 falhas |
| WSL `-race` (17 pacotes) | detector de corrida | ✅ 0 races |
| `gofmt` (blob commitado) / `go vet` | estático | ✅ |
| `GOAMD64=v1` build | 8.532.128 bytes, `0.12.0-m12` | ✅ |

### Segurança (falham se a proteção for removida)
- `TestListModelsBoundsResponseSize` — corpo gigante falha fechado.
- `TestListModelsSanitizesProviderStrings` / `TestListModelsRedactsSecretInIDs` — dado remoto não
  injeta ANSI nem vaza segredo.
- `TestListModelsNeverLeaksKeyInErrors` (4 caminhos) / `TestListModelsFailsClosedWithoutKey`.
- `TestListModelsHonoursContext` — o cancelamento do caller vale.
- `TestSelectModelEmptyListRefuses`, `TestPickerFetchFailureIsFailClosed`,
  `TestPickerClosedWhileFetchingDoesNotResurrect`.

## Smoke no Inspiron (pty real, NVIDIA NIM, `0.12.0-m12`)

| Verificação | Resultado |
|---|---|
| `/models` no REPL | 81 modelos, `*` no ativo, nota de `--free` indisponível |
| `/models llama` | filtra |
| `/model 9999` | `erro: indice 9999 fora da lista (66 modelos); nada foi trocado` |
| `/models --free` | `provedor nao informa preco: filtro --free indisponivel aqui` |
| TUI: picker | abre, mostra "carregando…", depois contadores e candidatos |
| TUI: digitar | filtra ao vivo (12 candidatos com `nemot`) |
| TUI: `Enter` | `model: … (vale nesta sessao)` e painel fecha |
| TUI: turno real depois | `aguardando o primeiro token` → `recebendo tokens` |

## Não validado em hardware

`--free` e a camada 2 (metadados do provedor) só existem de fato no **OpenRouter**, e o ambiente
não tem chave desse provedor. Eles estão cobertos por teste com fixture realista e declarados como
não validados em hardware na spec.

## Próximos passos

1. PR `m12-catalogo-modelos` → `main`.
2. Se quiser, uma `--check` real: custa 1 request por modelo candidato, por isso fica explícita.
3. **Backlog Web** (SSE + shell-HTTP + `Approver` por canal + auth/TLS/CSRF).