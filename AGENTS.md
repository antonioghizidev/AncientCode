# AGENTS.md — Guia para agentes/sessões de desenvolvimento

> Leia **este arquivo** antes de qualquer mudança. Ele orquestra o trabalho em sessões limpas.

## Projeto

**AncientAI** — harness de agente de IA para terminal em hardware **sem AVX** (Dell Inspiron
1545 / Core 2 Duo / 4 GB RAM / Linux server minimal). Cliente fino em **Go**: o modelo roda no
provedor remoto (OpenRouter / NVIDIA NIM / LiteLLM; contrato OpenAI-compatible). Binário único,
`CGO_ENABLED=0`, `GOAMD64=v1` (sem SSE pós-SSE2).

## Onde as coisas vivem

| Artefato | Caminho |
|---|---|
| Especificação técnica (arquitetura, ameaças, segurança) | `docs/SPEC.md` |
| Registro de decisões (ADR) | `docs/DECISOES.md` |
| Notas por sessão | `docs/session-<data>-<slug>.md` |
| Metodologia de trabalho (SDD) | `specs/methodology.md` — **obrigatória** |
| Regras não-negociáveis | `specs/constitution.md` |
| Specs por milestone | `specs/milestones/` |
| Templates | `specs/templates/` |

## Estado atual

- **M1 concluído** (skeleton: config, provider OpenAI-compat + SSE, histórico redigido, loop
  fail-closed, REPL). Ver `docs/session-2026-09-14-m1.md`.
- **M2 concluído** (tool registry, filesystem confinado, tool `shell` com `ask`, loop
  multi-turno com gate e teto, fallback JSON, scan de exfiltração). 105 testes, 0 falhas.
  Ver `docs/session-2026-09-15-m2.md`.
- **M3 concluído** (toolbox por modo plan/run, `Approver` interface com veto, fluxo do plano
  y/n/feedback no REPL, `--run` com allow explícito, `approval.timeout_seconds`).
  112 testes, 0 falhas. Ver `docs/session-2026-09-15-m3.md`.
- **M4 concluído** (sandbox completo: internal/audit JSONL append-only, internal/sandbox
  policy whitelist+hard-block, executor run_user/workdir/timeout, shell refatorado sobre
  sandbox, auditoria no loop com denied/timeout/error, args redigidos no audit).
  Ver `docs/session-2026-09-15-m4.md`.
- **M5 concluído** (docker tool com subcomando whitelist + vetor de args; apt update/install
  com remove/purge negados; toggles docker_tool/apt_tool exigem ask; sessão: `--resume`,
  `--cleanup` explícito, `session_context_bytes` + `Store.Reduce` por turnos; bug M4 latente
  corrigido — timeout default do shell era 30ns). Host: 157 pass / 14 skip, 0 falhas; suíte
  Linux real verde no WSL. Ver `docs/session-2026-09-15-m5.md`.
- **M6 concluído** (agente de codificação §10: `internal/tools/code` com `code_scaffold`
  (templates embed validados, nunca executa `.sh`), `edit_file` (diff + `ask`), `code_review`
  read-only com heurísticas locais e relatório estruturado; filesystem tools enraizadas no
  `code_root` strict-child validado; política `code_scaffold`/`edit_file`=ask; injeção indireta
  etiquetada untrusted). Host: 178 pass / 14 skip, 0 falhas; suíte Linux real verde no WSL
  (incl. `TestM6*`). Ver `docs/session-2026-09-15-m6.md`.
- **M6 pós-commit (validação no Inspiron)**: smoke M1–M6 real no Dell via SSH (session
  `docs/session-2026-09-16-inspiron-m6.md`): config fail-closed, scaffold real com gate,
  review read-only + strict-child, edit_file com aprovação, docker real, apt real (exit 100
  sem root, propagado), `--cleanup`/`--resume`/`--reset`/`--run` verificados. Defeito do
  preview `edit_file` corrigido (diff LCS stdlib, D-2026-09-16-29, teste de regressão).
  Ajustes de implantação: `run_user` exige harness no usuário dedicado `ancientai` ou
  privilégio (D-2026-09-16-30); apt não-root sem efeito, docker ok via grupo
  (D-2026-09-16-31). Quota free do OpenRouter esgotou (429) durante o smoke — revalidar
  `shell`/`apt` com efeito no usuário dedicado.
- **Próximo**: **finalizar a validação/tuning no Inspiron como usuário dedicado** (shell/apt
  com efeito, `--resume` conversa real, quota renovada/outro modelo) — **depois** avaliar
  **M7 — Skills** (`specs/milestones/07-m7-skills.md`).
- M7/M8 são "futuro" — **não iniciar** antes do gate dos anteriores e da validação no Inspiron
  como usuário dedicado.

## Como trabalhar (resumo da metodologia)

1. Ler esta base + `specs/constitution.md` + `specs/methodology.md` + spec do milestone ativo.
2. Decompor escopo em tasks; registrar decisões em `docs/DECISOES.md`.
3. **TDD**: teste (falha) → implementação → verde; **testes de segurança para tudo que for
   relevante à segurança do milestone** (devem falhar se a proteção for removida).
4. Verificar qualidade antes de fechar:
   - `gofmt -l .` (vazio), `go vet ./...`, `go build ./...`
   - `go test -count=1 ./...` — **todos devem passar**
   - quando houver toolchain C (Linux/CI): `go test -race -count=1 ./...`
5. Documentar: DECISOES + nota de sessão (template `specs/templates/session.md`) + marcar
   checkboxes **somente o que foi feito**.
6. Commit padronizado: `feat(M<N>): <resumo imperativo em minúsculas>`; revisar `git diff`;
   nunca segredos/binários/`.obsidian/`.

## Regras de segurança inegociáveis (resumo)

- **Deny-by-default**: nada executa/altera sem aprovação explícita; config insegura falha na
  validação.
- **Confiança zero** na saída do modelo E de ferramentas; tool mutável só com gate humano.
- **Segredos jamais**: env/secret manager; redigir com `internal/redact` antes de log/persistir/
  enviar ao modelo; nunca citar valores de chave em erros.
- **Fail-closed**: parser estrito, tool desconhecida recusada, loop com teto de iterações.
- **Sem downgrade silencioso**: não desligar verificação TLS, não enfraquecer para passar teste.

## Convenções de código (Go)

- Stdlib + `gopkg.in/yaml.v3` (config) — não adicionar dependência sem justificativa +
  decisão registrada.
- `os/exec` sempre com vetor de args; proibido montar shell a partir de texto do modelo (só a
  tool `shell` deliberada, com `ask`).
- Todo I/O de rede e execução com `context`/deadline; leituras sempre limitadas.
- Path traversal: `filepath.Clean` + verificação de prefixo em toda escrita/leitura.

## Comandos úteis

```sh
go test -count=1 ./...                      # suíte completa (gate)
go vet ./...                                # estático
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags="-s -w" -o dist/ancientai ./cmd/ancientai
```