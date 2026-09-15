# M5 — Docker/apt tools + sessão persistente

**Status**: ⏳ pendente
**Depende de**: M2, M4
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Ferramentas de operação de sistema (Docker e apt) sobre o sandbox, e persistência/roun
de sessões com controle de contexto (SPEC §9).

## 2. Escopo / entregáveis

- [ ] `internal/tools/docker` — wrappers `docker ps`, `docker logs`, `docker inspect`,
      `docker restart`, `docker stop` etc., com subcomandos permitido por whitelist de
      subcomando; sempre via vetor de args (`exec.Command("docker", "ps", ...)`), nunca
      "docker "+model-text.
- [ ] `internal/tools/package` — apt wrapper (update/install/remove) com `ask` sempre.
- [ ] Sessão persistente aprimorada:
  - `--resume <id>` reutiliza `sessions/<id>.jsonl`;
  - heurística de contagem de contexto (sem tokenizer local) e truncadura dos turnos mais
    antigos; opcionalmente sumarização via modelo (turno extra marcado não autoritativo);
  - máscara de segredos aplicada antes de persistir (já em M1 — manter e cobrir em teste).
- [ ] Limpeza de sessões antigas (ddl) sem auto-executar nada.

## 3. Contratos

- Docker tool: `Name="docker"`, args estruturados em JSON (`{"subcommand":"ps","args":[...]}`).
- apt tool: `Name="apt"`, `{"action":"install","packages":["..."]}`.
- `model.Store` ganha `Reduce(maxBytes)`/`SummaryMessage`.

## 4. Requisitos de segurança deste milestone

- [ ] Docker/apt nunca concatenam texto do modelo em um shell.
- [ ] Só metas definidas → subcomandos fora da whitelist recusados (ex.: `docker run -it` com
      montagem de volumes, `docker push`) requirem política reforçada/`deny`.
- [ ] Instalação de pacote (`apt install`) sempre `ask`; `apt remove`/`purge` recusado por
      default (mutante a mais).
- [ ] Truncadura de histórico não perde metadados de segurança (tool_calls permanecem mapeados).

## 5. Testes obrigatórios

### Funcionais

- [ ] Docker: `docker ps`/`logs`/`inspect` via fake `docker` binário (PATH injetado nos testes)
      retornam correctamente.
- [ ] apt: `install` com confirm aprova; structure de args.
- [ ] Sessão: `Resume` carrega e continua; truncadura de histórico funciona.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [ ] `docker run -v /:/host image` / `docker push` / meta fora da whitelist → recusado e
      auditado.
- [ ] `apt remove curl` sem aprovação → recusado (ask).
- [ ] Segredos nos logs de `docker logs` não persistem em claro (redação no histórico/audit).
- [ ] Sessão corrompida → Load falha fechado (não "recupera" parcialmente).
- [ ] Truncadura não remove `tool_calls` sem apagar também suas mensagens `tool` associadas.

## 6. Critérios de aceite (DoD)

- [ ] **Todos os testes do projeto passam** (`go test -count=1 ./...`).
- [ ] `gofmt`/`go vet`/`go build` limpos.
- [ ] `docs/DECISOES.md` atualizado.
- [ ] Nota de sessão criada.
- [ ] Status deste arquivo e de `docs/SPEC.md` atualizados.
- [ ] Aceite SPEC §12 M5: diagnóstico de container quebrado (logs/inspect/restart) com
      aprovações; `--resume` recupera contexto.