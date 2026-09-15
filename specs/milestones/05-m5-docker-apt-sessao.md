# M5 — Docker/apt tools + sessão persistente

**Status**: ✅ concluído
**Depende de**: M2, M4
**Sessão de referência**: `docs/session-2026-09-15-m5.md`

## 1. Objetivo

Ferramentas de operação de sistema (Docker e apt) sobre o sandbox, e persistência/round
de sessões com controle de contexto (SPEC §9).

## 2. Escopo / entregáveis

- [x] `internal/tools/docker` — wrappers `docker ps`, `docker logs`, `docker inspect`,
      `docker restart`, `docker stop` etc., com subcomandos permitido por whitelist de
      subcomando; sempre via vetor de args (`exec.Command("docker", "ps", ...)`), nunca
      "docker "+model-text.
- [x] `internal/tools/package` — apt wrapper (update/install; remove/purge recusados por
      default) com `ask` sempre. Implementado como `internal/tools/apttools` (`Name="apt"`).
- [x] Sessão persistente aprimorada:
  - [x] `--resume <id>` reutiliza `sessions/<id>.jsonl`;
  - [x] heurística de contagem de contexto (sem tokenizer local) e truncadura dos turnos mais
        antigos (`session_context_bytes` + `Store.Reduce`, turnos inteiros, sem quebrar tool_calls);
  - [ ] sumarização via modelo (turno extra marcado não autoritativo) — contrato
        `Store.SummaryMessage` pronto; gatilho por modelo fica como gancho opcional futuro;
  - [x] máscara de segredos aplicada antes de persistir (mantida de M1 + coberta em teste).
- [x] Limpeza de sessões antigas (ddl) sem auto-executar nada (`--cleanup <dias>`, só com flag).

## 3. Contratos

- Docker tool: `Name="docker"`, args estruturados em JSON (`{"subcommand":"ps","args":[...]}`).
- apt tool: `Name="apt"`, `{"action":"install","packages":["..."]}`.
- `model.Store` ganha `Reduce(maxBytes)`/`SummaryMessage`.

## 4. Requisitos de segurança deste milestone

- [x] Docker/apt nunca concatenam texto do modelo em um shell.
- [x] Só metas definidas → subcomandos fora da whitelist recusados (ex.: `docker run -it` com
      montagem de volumes, `docker push`) — `*tools.DeniedError` antes de tocar a CLI, auditado.
- [x] Instalação de pacote (`apt install`) sempre `ask`; `apt remove`/`purge` recusado por
      default (mutante a mais).
- [x] Truncadura de histórico não perde metadados de segurança (tool_calls permanecem mapeados).

## 5. Testes obrigatórios

### Funcionais

- [x] Docker: `docker ps`/`logs`/`inspect` via fake `docker` binário (PATH injetado nos testes)
      retornam correctamente (`TestDockerResolvesFromPATH`; roda no Linux/CI).
- [x] apt: `install` com confirm aprova; structure de args (`TestAptInstallVector`/`-UpdateVector`).
- [x] Sessão: `Resume` carrega e continua (`--resume` no main) e truncadura de histórico funciona
      (`TestStoreReduce`, `TestReduceKeepsFullTurns`, loop `TestM5ContextBudgetDropsOldestTurns`).

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [x] `docker run -v /:/host image` / `docker push` / meta fora da whitelist → recusado e
      auditado (`TestM5DockerDeniedSubcommandRecordedInAudit`).
- [x] `apt remove curl` sem aprovação → recusado por default no tool layer (`TestM5AptRemovalRefused`).
- [x] Segredos nos logs de `docker logs` não persistem em claro (redação no histórico/audit)
      (`TestM5DockerLogsRedactsSecret`, Linux).
- [x] Sessão corrompida → Load falha fechado (não "recupera" parcialmente) (`TestStoreCorruptSessionRejected`).
- [x] Truncadura não remove `tool_calls` sem apagar também suas mensagens `tool` associadas
      (`TestReduceKeepsToolCallsWithTheirResponse`).

## 6. Critérios de aceite (DoD)

- [x] **Todos os testes do projeto passam** (`go test -count=1 ./...`) — host: 157 pass / 14 skip;
      suíte Linux real (shell/sandbox/loop/fake-PATH) verde no WSL.
- [x] `gofmt`/`go vet`/`go build` limpos.
- [x] `docs/DECISOES.md` atualizado (D-2026-09-15-20..25).
- [x] Nota de sessão criada (`docs/session-2026-09-15-m5.md`).
- [x] Status deste arquivo e de `docs/SPEC.md` atualizados.
- [ ] Aceite SPEC §12 M5: diagnóstico de container quebrado (logs/inspect/restart) com
      aprovações; `--resume` recupera contexto — **exige validação no Inspiron (docker real)**.