# AGENTS.md — Guia para agentes/sessões de desenvolvimento

> Leia **este arquivo** antes de qualquer mudança. Ele orquestra o trabalho em sessões limpas.

## Projeto

**AncientCode** — harness de agente de IA para terminal em hardware **sem AVX** (Dell Inspiron
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

**M1–M16 mergeados em `main`, e o projeto é instalável por terceiros.** A `v0.16.1` está
publicada e verificada a partir das URLs públicas; o repositório é público. A `v0.16.0-pre`
continua ininstalável e não é a `latest` (D-87, D-92).

| | |
|---|---|
| Estado por milestone | [`specs/README.md`](specs/README.md) — tabela M1..M16 com spec e nota de sessão |
| O que está aberto agora | dívidas do M16 (§5 da spec): `SHA256SUMS` assinado, multi-plataforma, auto-update. O fallback de versão do `go install` entrou na `v0.16.1` (D-94) |
| Backlog anotado | `/compact`, `/new`, `/autocompact` (paridade com o opencode — a unidade é a **sessão**, os três criam sessão nova); `/sessions` para não perder acesso à anterior; backlog Web (SSE + shell-HTTP + `Approver` por canal) |

**Não reescreva o histórico de milestones aqui.** Ele vive em `specs/milestones/` (spec) e
`docs/session-*.md` (o que foi de fato validado). Este arquivo carrega convenções e
metodologia; um changelog de 250 linhas aqui envelhece e vira mentira — foi o que aconteceu.

Duas armadilhas já registradas, porque os detalhesmorrem e o padrão se repete:

- **Ausência de teste não é evidência de ausência de defeito.** Já corrigimos 4 bugs sem
  nenhum teste os pegando (banner nunca renderizado, `--next` inoperante, filtro que nunca
  ligou, `--check` no-op).
- **Helper certo, chamador errado.** `catalog.Filter` devolve cópias; o veredito sumia
  porque o `main.go` passava o slice errado. Nenhuma mutação de helper pega isso — só um
  teste de *call site* (D-75).

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

## Portabilidade — o que NÃO assumir

O dev machine é **Windows**; o alvo é **Linux sem AVX**. A suíte roda no host Windows, mas
os testes de permissão/POSIX são **pulados** lá (`runtime.GOOS == "windows"`), então o
`-race` no WSL continua sendo gate.

Medido neste host, não de memória:

| | Linux | Windows | macOS |
|---|---|---|---|
| `0600` no `auth.json` | real | **ignorado** (`-rw-rw-rw-`) | real |
| entrada mascarada (`/connect`) | ✅ | ❌ (`secret_other.go`) | ❌ |
| edição raw no REPL | ✅ | cooked (`raw_other.go`) | cooked |
| `run_user` sem privilégio | ✅ | usuário da sessão | usuário da sessão |

Regras que decorrem disso, e que **não** devem ser afrouxadas:

- **Ausência ≠ degradação.** `readSecret` falha fora do Linux em vez de ecoar a chave; é
  intencional. Não "simplifique" esse fallback.
- **Permissão de arquivo não é controle de segurança aqui.** `0600` no `auth.json` não protege
  contra o agente (`shell` roda como `run_user`); a defesa é o gate humano. Não escreva texto
  que sugira o contrário.
- **Testes de segurança que dependem de `0600`/`0700` devem pular no Windows**, e o WSL é
  quem valida de fato.
- Ao medir permissão num host novo, **meça** (`os.Stat().Mode().Perm()`); não deduza do
  código que a chamada existe.

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
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags="-s -w" -o dist/ancientcode ./cmd/ancientcode
```