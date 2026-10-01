# Sessão 2026-10-01 — m11-gate-informado

> Branch `m11-gate-informado`, a partir de `m10-tui-paineis`. Método: `specs/methodology.md`.

## Objetivo da sessão

Uso real do M10 no Inspiron revelou que a UI **não era auditável nem diagnosticável**:

1. o gate dizia apenas `[aprovar] shell` e a atividade `gate: shell` — sem o comando, não dava
   para saber o que estava sendo autorizado;
2. o rodapé virava `✗ erro` sem nenhuma mensagem na tela;
3. (derivado) conteúdo do modelo chegava ao paint sem remover sequências de controle.

## Decisões do usuário

- Gate: **destacar só o comando** (não o JSON cru); sem chave reconhecível, mostrar os args.
- Erro: **banner + entrada no histórico**.

## O que foi feito

- **`gateSummaryFor(tool, args)`**: lê a chave decisiva do schema **real** de cada tool
  (`shell.cmd`, `docker.subcommand`+`args`, `apt.action`+`packages`, `*.path`,
  `code_scaffold.template`+`name`, `skill_run.skill`+`script`+`argv`). Tool desconhecida → varre
  chaves conhecidas; shape irreconhecível → args crus (nunca esconde o payload).
- **`gateDetail`**: re-redige (defesa em profundidade), sanitiza, quebra em no máx. 2 linhas.
- **Atividade**: `Event{PhaseGateWaiting}` passou a carregar `Detail` (args **redigidos na
  origem**, em `internal/loop`), e `activityFromEvent` mostra `* gate: shell  id -un`.
- **Banner de erro**: renderizado em vermelho acima do input; o erro também vira linha
  `! erro: …` no painel de sessão (sobrevive à rolagem). `c` limpa.
- **`sanitize`**: remove C0/DEL/C1 (inclui ESC) de todo conteúdo não confiável antes do paint.
  Motivação de segurança: sem isso, `\x1b[2J` no retorno de uma tool podia apagar/falsificar o
  gate — e `redact` só mascara segredos.
- **`fixedRows`**: fonte única do orçamento de linhas (projeção e navegação concordam na altura).
- Versão `0.11.0-m11`.

## Defeito encontrado pelo smoke (regressão coberta)

O schema da tool `shell` declara `{"cmd": …}` (`internal/tools/shell/shell.go:55`), mas o struct
decodifica `json:"n"` — **toda chamada `shell` vinda do modelo falha** com `shell: n is required`.
Um extrator que procurasse apenas `command` cairia no fallback e o operador continuaria vendo JSON.
Coberto por `TestGateDetailReadsRealShellSchemaKey`; a correção do contrato ficou registrada em
**D-2026-10-01-52** (não corrigida no M11 — exige decisão e smoke próprios).

## Testes

| Pacote | Foco | Status |
|---|---|---|
| `cmd/ancientai` | M11 (gate, banner, sanitize, fixedRows) | ✅ |
| Host serial (16 pacotes) | suíte completa | ✅ 0 falhas |
| WSL `-race` (16 pacotes) | detector de corrida | ✅ 0 races |
| `gofmt` (blob commitado) / `go vet` | estático | ✅ |
| `GOAMD64=v1` build | 8.470.688 bytes | ✅ |
| Smoke Inspiron (pty + NIM) | gate com comando | ✅ |

### Segurança (falham se a proteção for removida)
- `TestSanitizeStripsControlSequences` — ESC/controles não chegam ao paint.
- `TestViewNeverPaintsRawControlSequences` — end-to-end na `View`.
- `TestGateDetailRedactsSecretInCommand` — segredo no comando não vaza no gate.
- `TestGateDialogShowsCommand` / `TestTurnErrorGoesToSessionHistory` — consentimento informado.
- `TestGateDetailReadsRealShellSchemaKey` — regressão do smoke.

## Smoke no Inspiron (pty real, NVIDIA NIM, `0.11.0-m11`)

| Verificação | Resultado |
|---|---|
| Gate `shell` | `[aprovar] shell \| y aprova…` + linha `  id -un` |
| Atividade | `* gate: shell  id -un` |
| Gate `write_file` (`y`) | mostra `{"content":"ola-m11","path":"smoke-m11.txt"}`; audit `executed policy=ask decision=allow`; arquivo criado |
| Gate `shell` (`n`) | negado, nada executado |
| Moldura | `┌┐└┘┬┴│` intacta |

**Lição de harness (não de produto)**: o driver lê o prompt do **stdin** (`-`) e a resposta em
`argv[2]`; passar `y` em `argv[1]` fazia o modelo receber o prompt literal `"y"` e responder
`"ok"` — o gate "desaparecia" por erro de invocação, não por bug. Confirmado rodando o binário
M10 (sem as mudanças) com o mesmo driver: mesmo comportamento.

Artefatos de teste e drivers temporários removidos do `/tmp` e de `/opt/ancientai/code`.

## Próximos passos

1. Abrir PR `m11-gate-informado` → `main`.
2. **D-2026-10-01-52**: alinhar o schema da tool `shell` (`cmd`) com a struct (`json:"n"`), com
   teste schema↔decode e smoke `shell` real — hoje a tool está inutilizável pelo modelo.
3. **Backlog Web** (SSE + shell-HTTP + `Approver` por canal + auth/TLS/CSRF).