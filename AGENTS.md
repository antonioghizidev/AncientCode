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
  fail-closed, REPL). 66 testes, 0 falhas.
- **Próximo**: **M2 — Tool loop** (`specs/milestones/02-m2-tool-loop.md`).
- M7/M8 são "futuro" — **não iniciar** antes do gate dos anteriores.

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