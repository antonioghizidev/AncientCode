# M4 — Sandbox completo

**Status**: ✅ concluído em 2026-09-15
**Depende de**: M2, M3
**Sessão de referência**: docs/session-2026-09-15-m4.md

## 1. Objetivo

Camadas físicas de execução em defesa em profundidade (SPEC §5.2): usuário não-privilegiado,
workdir fixo, timeouts, limites de saída, hard-block de comandos e auditoria JSONL.

## 2. Escopo / entregáveis

- [x] Sandbox de execução:
  - rodar tools via `su`/`sudo -u <run_user>` (nunca root); `run_user` testável com binário falso;
  - workdir fixo (config) — `cmd.Dir = workdir` em todo processo;
  - `context.WithTimeout` por tool (config `tool_timeouts`);
  - limite de saída `output_max_bytes` via `limitedBuffer`;
  - `Result.SignedArgs` firma o vetor aprovado antes da execução;
  - `run_user=root` é recusado pelo executor (fail-closed, deny-by-default).
- [x] Whitelist de comandos por prefixo (`ls`, `cat`, `git`, `docker`, `apt`...) com hard-block
      **inalterável** normalizado (`rm -rf /`, `dd ... of=/dev/`, `mkfs`, `chmod -R 777 /`, `sudo`).
- [x] Auditoria JSONL append-only (SPEC §5.7): timestamp, vetor redigido, status, hash SHA-256 da
      saída, política aplicada, decisão do operador; perm `0600`; rotação por tamanho com `keepCount`.
- [x] Módulo `internal/audit` separado (SPEC §3.2).

## 3. Contratos

- `internal/sandbox.Policy{ AllowCommand(cmd string) error; AllowExecutable(name string) error }` — camada 2;
- `internal/sandbox.DeniedError` — classificação de recusa para auditoria;
- `internal/sandbox.Executor` — camada 4 (envia pra execução com usuário/timeout/limites e
  retorna `Result` firmando o vetor aprovado);
- `internal/audit.Recorder{ Record(entry) error }` append-only;
- `tools.DeniedError` — classificação de recusa no nível de tools para o loop auditar.

## 4. Requisitos de segurança deste milestone

- [x] Hard-block se aplica **independente de aspas/encadeamento**: `rm -rf /`, pipes/`&&`/`;`
      recusados salvo allowlist por subcomando.
- [x] Whitelist por prefixo de executável real (primeiro token do comando do modelo).
- [x] Processos rodam como usuário não-root (`run_user=root` → deny; `run_user` diferente → `su - <user> -c`).
- [x] Auditoria registra **recusas** também (não só execuções).
- [x] Timeout mata o processo e reporta truncado (sem matar o harness).
- [x] Saída com segredo é redigida na auditoria (hash da saída; nunca texto plano de output).
- [x] Args redigidos no audit via `redact.Redact()` (jamais segredo em claro no JSONL).

## 5. Testes obrigatórios

### Funcionais

- [x] Executor roda com timeout, retornando ExitCode/Truncated/Output.
- [x] Whitelist permite prefixos listados; comando desconhecido → negado.
- [x] Auditoria escreve/roda/rotação.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [x] "rm -rf /" (com e sem aspas/encadeamento) **bloqueado em 4 camadas** (policy normalizado +
      chaining + shell whitelist + loop audited) e **registrado na auditoria na recusa**.
- [x] `dd ... of=/dev/sda`, `mkfs`, `chmod -R 777 /`, `sudo` → rejeitados (hard-block).
- [x] Execução como root falha (`run_user=root` → `*DeniedError`, deny-by-default).
- [x] Timeout curto (ex.: 10ms) com `sleep 5` encerra e reporta erro — sem vazar processos.
- [x] Saída > limite é truncada; hash correto na auditoria; segredo na saída não vai ao audit em claro.
- [x] Escrita fora da raiz → falha (files tool com workdir confinado).

## 6. Critérios de aceite (DoD)

- [x] **Todos os testes do projeto passam** (`go test -count=1 ./...`).
- [x] `gofmt`/`go vet`/`go build` limpos.
- [x] `docs/DECISOES.md` atualizado (D-2026-09-15-15..19).
- [x] Nota de sessão criada (docs/session-2026-09-15-m4.md).
- [x] Status deste arquivo e de `docs/SPEC.md` atualizados.
- [x] Aceite funcional SPEC §12 M4: `rm -rf /` bloqueado em 4 camadas com registro de recusa.
- [x] Artefato `GOAMD64=v1` compilado.
