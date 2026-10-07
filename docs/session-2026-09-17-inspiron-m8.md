# Smoke M8 no Inspiron — 2026-09-17 (raw-mode, comandos, Ctrl+C cancel)

> Validação real do binário `0.8.0-m8` (`GOAMD64=v1`, md5 `22bbc758948f2c86f3488f1c2c75cb7b`)
> no Dell Inspiron (kernel 6.8.0-139-generic, usuário `ancientcode`, provider NVIDIA NIM).

## Contexto do teste

- Binário em `/home/ancientcode/ancientcode-0.8.0-m8` (deploy via scp; `-version` imprime
  `ancientcode 0.8.0-m8`). Config `ancientcode.yaml` real (NVIDIA NIM, toolbox run, ask).
- Config de teste `~/m8-test.yaml` = mesmo YAML + `session_context_bytes: 20000`
  (multi-doc merge do yaml.v3 validado em smoke).
- Sessões de teste (`m8-pty*.jsonl`) em `/opt/ancientcode/sessions/`, removidas ao final.

## O que foi validado

### Pipe/cooked (sem pty)
- Banner, `/help`, `/historico`, `/context` (`ctx: 0% (0/20000 bytes)`), `/model`,
  `/provider`, `/exit` — tudo limpo.
- Comutação fail-closed: `/provider litellm` → `erro: provider: API key env
  "LITELLM_API_KEY" nao definida; switch recusado (fail-closed)` (env ausente no host).
  `/provider openrouter` ok, `/provider nvidia` ok, `/model` ok, lista `opcoes: litellm,
  nvidia, openrouter`.

### PTY real (raw-mode linux)
Drive via `pty.fork` no próprio Inspiron, sincronizado por marcadores de bytes
(`\r> \x1b[K` = prompt pronto; `recebendo tokens` = primeiro token SSE), não por sleep.
- **Editor raw**: echo caractere a caractere com cursor correto (sem o artefato do `\x1b[4D`
  do defeito pré-fix) — confirmado pós-correção D-2026-09-17-48 do `render`.
- **Histórico**: ↑↑ + Enter reenvia a penúltima entrada; `/historico` imprime entradas
  numeradas e redigidas (`1: oi`, `2: ola de novo`).
- **`!N`**: `!1` re-envia redigido; `!9` (fora do índice) → `erro: sem prompt !9 (historico
  tem 4 entradas)`.
- **Ctrl+C em voo**: com o turno NIM em `modelo: recebendo tokens...`, `\x03` cancelou o
  turno → `turno cancelado (Ctrl+C) — voce pode dar /retry ou /continue` e o prompt voltou;
  turno seguinte respondeu (loop vivo). Saída `/exit` limpa, exit status 0.
- **Comandos em raw**: `/context` (`0/20000 bytes`), `/model`
  (`nvidia/nemotron-3.5-lightning-30b-a3b`), `/provider` (`base_url` + `opcoes`) emitidos
  corretamente no pty.
- **Status phases**: `>>> modelo: aguardando o primeiro token...` → `>>> modelo: recebendo
  tokens...` → respostas da NIM (Nemotron 3.5 30B A3B) entregues.

## Defeitos encontrados durante a sessão

- **D-2026-09-17-48** (fix commitado `cf57fa9`): o `render` do editor contava `\r` e `\x1b[K`
  como colunas, movendo o cursor 4 posições a mais a cada echo no pty. Corrigido para
  `moveLeft := len(e.buf) - e.pos` + `TestEditorRenderCursor` (empty/end/middle/home,
  bytes exatos). ![+1 teste no `cmd/ancientcode`]

## Pendências / observações

- Quirk cosmético pré-existente (D-35): a resposta do modelo imprime na mesma linha do
  último status (`...recebendo tokens... Olá!`). Anotado para a TUI M9.
- Smoke com `--run` em pipe (cooked) não re-executado no Inspiron nesta sessão; coberto
  pela suíte M6/M7 e pelo caminho cooked do `raw_other.go`.

## Resultado

**Passou**: raw-mode + Gate/Ctrl+C + histórico/`!N` + comandos — Todos os critérios de
aceite do M8 cobertos no host real. Ver `docs/session-2026-09-17-m8.md`.