# M4 — Sandbox completo

**Status**: ⏳ pendente
**Depende de**: M2, M3
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Camadas físicas de execução em defesa em profundidade (SPEC §5.2): usuário não-privilegiado,
workdir fixo, timeouts, limites de saída, hard-block de comandos e auditoria JSONL.

## 2. Escopo / entregáveis

- [ ] Sandbox de execução:
  - rodar tools via `su`/`sudo -u <run_user>` (nunca root); reservar `run_user` testável com
    binário falso;
  - workdir fixo (config) — escrita fora bloqueada;
  - `context.WithTimeout` por tool (config `tool_timeouts`);
  - limite de saída `output_max_bytes` (já no M2 o truncamento existe); repositório do vetor
    exato (args) antes da execução;
  - limites opcionais `ulimit` (memória/CPU) para tools longas.
- [ ] Whitelist de comandos por prefixo (`ls`, `cat`, `git`, `docker`, `ip`, `sysctl`,
      `journalctl`, `apt`...) com hard-block **inalterável** (`rm -rf /`, `dd ... of=/dev/`,
      `mkfs`, `chmod -R 777 /`, `sudo`, format).
- [ ] Auditoria JSONL append-only (SPEC §5.7): timestamp, vetor real, status, hash da saída,
      política aplicada, decisão do operador; perm `0600`; rotação por tamanho.
- [ ] Módulo `internal/audit` separado (SPEC §3.2).

## 3. Contratos

- `internal/sandbox.Policy{ AllowCommand(executable string, argv []string) error }` — camada 2;
- `internal/sandbox.Executor` — camada 4 (envia pra execução com usuário/timeout/limites e
  retorna `Result` firmando o vetor aprovado);
- `internal/audit.Recorder` com `Record(entry)` append-only.

## 4. Requisitos de segurança deste milestone

- [ ] Hard-block se aplica **independente de aspas/encadeamento**: `rm -rf /`, pipes/`&&`/`;`
      recusados salvo allowlist por subcomando.
- [ ] Whitelist por prefixo de `executable` real (resolve o binário, não o texto do modelo).
- [ ] Processos rodam como usuário não-root (falha a execução se não conseguir baixar
      privilégio — deny-by-default).
- [ ] Auditoria registra **recusas** também (não só execuções).
- [ ] Timeout mata o processo e reporta truncado (sem matar o harness).
- [ ] Saída com segredo é redigida na auditoria (hash da saída; se incluída, redigida).

## 5. Testes obrigatórios

### Funcionais

- [ ] Executor roda com timeout, retornando ExitCode/Truncated/Output.
- [ ] Whitelist permite prefixos listados; comando desconhecido → negado.
- [ ] Auditoria escreve/roda/rotação.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [ ] "rm -rf /" (com e sem aspas/encadeamento) **bloqueado em 4 camadas** e **registrado na
      auditoria na recusa**.
- [ ] `dd ... of=/dev/sda`, `mkfs`, `chmod -R 777 /`, `sudo` → rejeitados (hard-block).
- [ ] Execução como root falha (se a camada de usuário for removida o teste falha).
- [ ] Timeout curto (ex.: 10ms) com `sleep 5` encerra e reporta erro — sem vazar processos.
- [ ] Saída > limite é truncada; hash correto na auditoria; segredo na saída não vai ao audit
      em claro.
- [ ] Mudar WORKDIR/escrita fora da raiz → falha (criação de arquivo fora do diretório
      rejeitada).

## 6. Critérios de aceite (DoD)

- [ ] **Todos os testes do projeto passam** (`go test -count=1 ./...`).
- [ ] `gofmt`/`go vet`/`go build` limpos.
- [ ] `docs/DECISOES.md` atualizado.
- [ ] Nota de sessão criada.
- [ ] Status deste arquivo e de `docs/SPEC.md` atualizados.
- [ ] Aceite funcional SPEC §12 M4: `rm -rf /` bloqueado em 4 camadas com registro de recusa.
- [ ] Artefato `GOAMD64=v1` compilado (sandbox depende de bins do SO, validar no alvo).