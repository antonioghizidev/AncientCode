# M11 — Gate informado e erros visíveis

**Status**: ▶ em andamento
**Depende de**: M10 (painéis/scrollbar/foco)
**Sessão de referência**: `docs/session-2026-10-01-m11-gate-informado.md`

## 1. Objetivo

Aprovação só vale se o operador puder **ver o que está autorizando**, e um erro que muda o
rodapé para `✗ erro` precisa dizer **qual** erro foi. Três defeitos reportados no uso real:

1. o gate e o painel de atividade mostravam `gate: shell` **sem o comando** — impossível saber o
   que se estava autorizando;
2. o rodapé virava `✗ erro` sem nenhuma mensagem na tela: `buildView` nunca renderizava
   `m.banner` (7 escritas, 0 leituras);
3. (segurança, derivado de 1/2) conteúdo do modelo ia para o paint sem remoção de sequências de
   controle: `\x1b[2J` no output de uma tool podia apagar/falsificar o gate na tela.

## 2. Escopo / entregáveis

- [x] Gate mostra **o que será executado**: para `shell`/ferramentas com `command`, destaca o
      comando em vez do JSON bruto; sem `command`, mostra os args redigidos (até 2 linhas).
- [x] Painel de atividade mostra o mesmo detalhe na linha do gate (`* gate: shell  docker ps -la`).
- [x] Erro visível: linha de banner em vermelho acima do input **e** entrada `! erro: …` no
      painel de sessão (fica no histórico rolável). `c` limpa o banner.
- [x] **Hardening**: `sanitize` remove C0/C1 + ESC de todo conteúdo não confiável antes do paint
      (sessão, atividade, gate) — o modelo não pode emitir sequências ANSI na UI.
- [x] Gate `y/n/d/C-c`, `Approver` único, redação e auditoria inalterados.

## 3. Contratos

Puras em `cmd/ancientcode/tui_panels.go`:

- `sanitize(s) string` — remove C0/C1 + ESC de conteúdo não confiável antes do paint (o modelo
  não pode emitir sequências ANSI na UI); `\t`→espaço.
- `gateSummaryFor(tool, args) string` — resumo de uma linha a partir das chaves **reais de cada
  schema** (`gateArgKeys`/`gateListKeys`), com `gateFallbackKeys` para tool desconhecida e os args
  crus quando nada é reconhecível (nunca esconde o payload).
- `gateDetail(summary, width, maxLines) []string` — redige + sanitiza + quebra em `maxLines`.
- `fixedRows(gateOpen, banner bool) int` — fonte única do orçamento de linhas, usada por
  `buildView` e por `panelContentRows`.

> **Achado do smoke (regressão coberta)**: o schema da tool `shell` declara a chave **`cmd`**, não
> `command` — um extrator que só procurasse `command` cairia no fallback e o operador continuaria
> vendo JSON cru. Ver `TestGateDetailReadsRealShellSchemaKey`.

## 4. Requisitos de segurança

- [x] Aprovação é informada: o detalhe exibido vem do mesmo `summary` (já redigido no domínio)
      que o `Approver` recebe; nada é re-derivado do modelo.
- [x] `sanitize` aplicado a **toda** linha não confiável antes do paint — teste falha se removido.
- [x] Segredo no `command`/`args` nunca aparece no gate nem na atividade (redação dupla).
- [x] `internal/loop` só ganha `Detail` redigido no evento de gate; `Decide`/política inalterados.

## 5. Testes obrigatórios

### Funcionais
- [x] `gateDetail`: extrai `command`; sem `command` devolve args; JSON inválido não quebra;
      trunca em `maxLines` e em `width`.
- [x] Gate renderizado na `View` contém tool + comando.
- [x] `activityFromEvent(PhaseGateWaiting)` inclui o detalhe.
- [x] `buildView` renderiza o banner; erro também vira linha de sessão `! erro: …`.
- [x] `fixedRows` bate com a altura usada por `buildView` e por `panelContentRows`.

### Segurança (falham se a proteção for removida)
- [x] **`sanitize` remove ESC/controles**: `"\x1b[2Jshell"` não chega ao paint.
- [x] **Segredo nos args não vaza no gate**: token dentro de `command` sai mascarado.
- [x] Segredo no erro não vaza no banner nem na linha de sessão.

## 6. Critérios de aceite (DoD)

- [x] Suíte verde (host serial + WSL `-race`), `gofmt`/`vet` limpos, `GOAMD64=v1` ok.
- [x] Smoke Inspiron: gate exibindo o comando; `y` executa (audit `executed`), `d` nega;
      erro de turno visível com mensagem.
- [x] `docs/DECISOES.md` (D-51), MANUAL, `AGENTS.md`, nota de sessão.
- [x] Commit `feat(M11): ...`.