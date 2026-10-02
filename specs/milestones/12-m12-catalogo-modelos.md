# M12 — Catálogo de modelos: listar, filtrar e selecionar

**Status**: ▶ em andamento
**Depende de**: M11 (gate informado, sanitize no paint)
**Sessão de referência**: `docs/session-2026-10-01-m12-catalogo-modelos.md`

## 1. Objetivo

Permiter **ver os modelos que o provedor oferece e escolher um** sem decorar IDs. O critério de
"funciona aqui" é mais estreito que "é um modelo de chat": o agente depende de o modelo emitir
**`tool_calls`**.

## 2. Abordagem em camadas (D-2026-10-01-53)

Medido no Inspiron: **NIM 81 modelos** (só `id`, `object`, `created`, `owned_by` — sem metadados)
e **OpenRouter 464 modelos** (`architecture`, `context_length`, `pricing`, `supported_parameters`,
`name`, …). Logo a mesma filtragem tem fidelidades muito diferentes:

| Camada | Custo | NIM | OpenRouter |
|---|---|---|---|
| 1. Excluir famílias não-chat por nome + curadoria | zero | heurística | heurística |
| 2. Metadados do provedor (text input, `tools`, contexto, preço) | zero | **não existe** | preciso |
| 3. Verificação real (mini request com `tool_calls`) | 1 request/modelo | sob demanda | sob demanda |

- **Padrão = camadas 1+2.** Não adivinha: o que não há metadado, é marcado como *desconhecido*,
  não como "funciona".
- **`/models --check`** = camada 3, explícita (gasta requests; pede confirmação).
- **`--free`** só é offerta onde há preço (`pricing.prompt == "0"`): **OpenRouter sim, NIM não** —
  a UI declara a indisponibilidade em vez de mostrar lista vazia.

## 3. Escopo / entregáveis

- [x] `internal/provider`: `ListModels(ctx)` → `[]ModelInfo`, via `GET {baseURL}/models`.
- [x] `internal/catalog` (novo, stdlib): `Classify` (chat/embedding/rerank/guard/vision/audio/
      moderation/unknown), `SupportsTools` (sim/não/**desconhecido**), `Filter` (texto livre,
      `FreeOnly`, `IncludeNonChat`), `Rank` (modelo ativo no topo, depois alfabético), `FreeKnown`.
- [x] REPL: `/models [--free|--all|--check]` (numerado, `*` no ativo) e `/model <N>` seleciona
      pela posição da última lista.
- [x] TUI: `/models` abre **painel dedicado**: digitação filtra, `↑↓` move, `Enter` seleciona,
      `Esc`/`Ctrl+C` cancela, `Ctrl+F` alterna só gratuitos, contadores no rodapé do painel.
- [x] `ProviderNames` inalterado; `/provider` e `/model <nome>` seguem funcionando (trocas agora **ass�ncronas**: D-54).

## 4. Requisitos de segurança

- [x] **Leitura limitada** (`io.LimitReader`) e `ctx` com timeout: uma lista gigante ou um host
      lento não pode travar a UI nem exaurir memória.
- [x] **A chave da API nunca aparece** em erro, log ou lista (o env é lido por request, como já
      é feito no `Chat`).
- [x] **A lista é dado não confiável**: IDs passam por `redact.Redact` + `sanitize` antes de ir
      para a tela; um id com `\x1b[2J` ou com cara de token não pode injetar ANSI nem vazar
      segredo.
- [x] **Fail-closed**: `ListModels` com erro → o frontends **não troca** de modelo nem mostra
      lista parcial silenciosa; mensagem explícita e o modelo atual permanece.
- [x] Trocar de modelo continua sendo **por sessão**, fail-closed do `SwitchModel` (M8) intacto;
      nada é persistido no YAML.
- [x] `--check` exige confirmação explícita antes de gastar requests (1 por modelo candidato).

## 5. Testes obrigatórios

### Funcionais
- [x] `ListModels` com `httptest`: parse de `data[].id`, `name`, `context_length`, `pricing`,
      `architecture`, `supported_parameters`; resposta sem `data` → erro.
- [x] `Classify`: `nvidia/llama-3.1-8b-instruct`→chat; `nvidia/nv-embedqa-e5`→embedding;
      `nvidia/llama-3.1-nemoguard`→guard; `meta/*-vision`→vision; desconhecido→unknown.
- [x] `Filter`: texto casa por substring (case-insensitive) em id e nome; `FreeOnly` mantém só
      preço 0; `IncludeNonChat` traz embedding/guard de volta; filtro sem `pricing` → **não** é
      gratuito (não inventa).
- [x] `Rank`: modelo ativo primeiro; resto alfabético; estável.
- [x] REPL: `/models` imprime numerado; `/model 2` troca para o da posição 2; índice inválido →
      erro fail-closed sem trocar.
- [x] TUI: `/models` abre o painel; digitar filtra; `↑↓` move o destaque; `Enter` seleciona e
      fecha; `Esc` cancela sem trocar; `Ctrl+F` alterna gratuitos.

### Segurança
- [x] Resposta enorme é truncada no limite (teste com corpo > limite).
- [x] id malicioso com ESC não chega ao paint; id com formato de token sai mascarado.
- [x] Falha do endpoint não troca o modelo (o `CurrentModel` é o mesmo antes e depois).
- [x] A chave não aparece no erro de `ListModels` (env ausente, HTTP 401, corpo inválido).

## 6. Critérios de aceite (DoD)

- [x] Suíte verde (host serial + WSL `-race`), `gofmt`/`vet` limpos, `GOAMD64=v1` ok.
- [x] Smoke Inspiron (NIM): `/models` lista, `/model N` troca, picker TUI seleciona.
- [x] Sem OpenRouter no ambiente, a parte de `--free` fica coberta por teste com resposta
      realista (fixture) — declarado explicitamente como não validada em hardware.
- [x] `docs/DECISOES.md` (D-53), `MANUAL.md`, `AGENTS.md`, nota de sessão.
- [x] Commit `feat(M12): ...`.