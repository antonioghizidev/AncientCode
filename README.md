# AncientAI

Agente de IA para terminal, construído para rodar em **hardware antigo** — sem GPU, sem tokenizer
local, sem runtime JavaScript. Um binário Go, a inferência acontece na API do provedor, e **nada
é executado sem aprovação humana**.

Desenvolvido e validado num **Dell Inspiron 1545 (Core 2 Duo de 2008, sem AVX, 4 GB de RAM,
Linux server minimal)**. É essa restrição que dita o desenho: `GOAMD64=v1`, zero dependência
nativa, footprint de processo na casa das dezenas de MB.

```
❯ verifique o hardware deste computador e me informe: chipset, processador,
  memória ram, swap, e armazenamento

  Aqui está o resumo que consegui levantar neste sistema:

  ## Processador
  - **Modelo:** Intel(R) Core(TM)2 Duo CPU E8400  @ 3.00GHz
  ...
  ! turno cancelado (Esc) — edite e reenvie | tools que ja executaram: shell
```

---

## O que ele faz

| | |
|---|---|
| **Diagnóstico e Linux** | logs, processos, rede, disco, memória — via `shell`, com aprovação |
| **Containers** | `docker` com subcomando em whitelist e vetor de argumentos validado |
| **Pacotes** | `apt` install/remove, com `remove`/`purge` negados por política |
| **Arquivos** | `read_file`, `write_file`, `list_dir` enraizados em `code_root` |
| **Código** | `code_scaffold`, `edit_file` (diff + gate), `code_review` read-only |
| **Skills** | pacotes versionados em disco, com verificação de integridade |
| **Modelos** | `/models` lista o catálogo do provedor, com filtros e seleção por painel |
| **Sessão persistente** | histórico em JSONL append-only, retomável e auditável |
| **TUI** | painéis com quebra de linha, animação de atividade e controle do turno |

## Começando

**Requisito**: Go 1.27+ para compilar. No destino, só o binário.

```sh
# 1. configuração — a chave NUNCA vai no arquivo
cp ancientai.example.yaml ancientai.yaml
export OPENROUTER_API_KEY='...'      # ou NVIDIA_API_KEY / LITELLM_API_KEY

# 2. build para o alvo (o dev machine pode ser outro)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
  go build -trimpath -ldflags="-s -w" -o ancientai ./cmd/ancientai

# 3. executar
./ancientai              # REPL
./ancientai --tui        # interface de painéis (precisa de TTY real)
./ancientai --run "..."  # não interativo (exige approval.write_file=allow)
```

`GOAMD64=v1` não é sugestão: impede o compilador de emitir instruções pós-SSE2, o que garante
execução no Core 2 Duo **por contrato** — uma build futura não introduz AVX por acidente.

### Provedores

| `provider` | endpoint | chave |
|---|---|---|
| `openrouter` | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` |
| `nvidia` | `https://integrate.api.nvidia.com/v1` | `NVIDIA_API_KEY` |
| `litellm` | `http://127.0.0.1:4000/v1` | `LITELLM_API_KEY` |

Qualquer endpoint compatível com OpenAI funciona por `base_url`. Overrides por ambiente:
`ANCIENTAI_PROVIDER`, `ANCIENTAI_BASE_URL`, `ANCIENTAI_MODEL`, `ANCIENTAI_WORKDIR`.

## Uso

### Comandos

| | |
|---|---|
| `/models [filtro]` | lista o catálogo do provedor — `--free`, `--all`, `--check`, ou texto |
| `/model [nome\|N]` | troca o modelo **só nesta sessão** (não persiste no YAML) |
| `/provider [nome]` | mostra a base URL ou troca de provedor na sessão |
| `/historico`, `!N` | lista os prompts e reenvia o N-ésimo |
| `/retry`, `/continue` | repete o turno / retoma o turno **cancelado** |
| `/context` | uso de contexto em bytes e em % da janela |
| `/reset` | **apaga** o arquivo da sessão (destrutivo, explícito) |
| `/exit`, `/help` | |

### Teclas da TUI

| Tecla | Efeito |
|---|---|
| `Esc` | **interrompe o turno em andamento** (sem turno, não faz nada) |
| `Ctrl+C` | 1º interrompe o turno; 2º sai |
| `↑` `↓` | edita prompts anteriores / rola o painel focado |
| `Tab` | alterna o foco entre sessão e atividade |
| `Shift+←` `Shift+→` | rolagem horizontal do painel |
| `PgUp` `PgDn`, `Home` `End` | navegação |

`r` e `c` são atalhos de retry/limpar-erro **apenas com a linha de entrada vazia** — com texto
digitado, são letras.

### Bandeiras

`--tui` · `--run` · `--session ID` · `--resume ID` · `--reset` · `--cleanup N` ·
`--config PATH` · `--verify-skill NAME` · `--skill NAME` · `--version`

`--cleanup` só roda **no startup**, remove `.jsonl` antigos por mtime e nunca é automático.
`--reset` apaga a sessão. `/new` (que preserva) ainda está no backlog — ver abaixo.

## Segurança

Este é o eixo do projeto. O modelo **propoe**; o operador **decide**.

1. **Deny-by-default.** Nada mutante executa sem `approval: ask` explícito. Config insegura falha
   na validação, na inicialização.
2. **Confiança zero na saída do modelo.** Uma resposta do LLM nunca é executada: passa por parser
   estrito → política → gate. Tool desconhecida é recusada.
3. **Gate informado.** A aprovação mostra **o comando real**, com os argumentos decisivos lidos do
   schema da tool — aprovar sem ver o payload não é consentimento.
4. **Sandbox em camadas.** Whitelist de subcomandos, vetor de argumentos (nunca shell montado a
   partir de texto do modelo), timeout, usuário não-privilegiado, teto de bytes de saída, e
   auditoria JSONL append-only.
5. **Segredos fora do arquivo.** A chave vive em variável de ambiente; `redact` mascara antes de
   logar, persistir ou enviar ao modelo; erros nunca citam o valor.
6. **Defesa contra prompt injection**, direta e indireta: saída de tools é etiquetada como
   conteúdo não confiável, arguments de `tool_calls` passam por scan de exfiltração, e há teto de
   iterações do loop com falha fechada.

Testes de segurança são **obrigatórios e falham se a proteção for removida** — não são testes de
cobertura, são guardas.

## Arquitetura

```
cmd/ancientai/        REPL, TUI (Bubble Tea), wiring, portões de UI
internal/loop/        o agente: parse, políticas, gate, observador, teto de turnos
internal/provider/    streaming SSE (OpenAI-compat) + catálogo de modelos
internal/tools/       shell, docker, apt, files, code, skills
internal/sandbox/     executor com vetor de args, política e timeout
internal/skills/      parser de skill com frontmatter estrito e verificação
internal/audit/       gravador JSONL append-only
internal/redact/      redação de segredos
internal/catalog/     classificação e filtros do catálogo (puro)
internal/config/      YAML validado no startup
internal/model/       sessão em disco e redução por turnos
```

`internal/*` é **stdlib puro** e não importa nada de `cmd/` — regra verificada por teste
(`TestLoopMustNotImportCmdOrUI`). As únicas dependências diretas são `gopkg.in/yaml.v3` e Bubble
Tea (entrega-única, só na camada de UI).

## Desenvolvimento

```sh
go test -count=1 ./...                          # suíte completa
go vet ./... && gofmt -l .                      # estático
```

Com toolchain C (Linux/CI): `go test -race -count=1 ./...`. Os testes de segurança são o portão,
não a cobertura.

O projeto é conduzido por **Spec-Driven Development**: cada milestone tem uma spec em
`specs/milestones/`, decisões registradas em `docs/DECISOES.md`, e nota de sessão em `docs/`.

| Documento | Para quê |
|---|---|
| [`docs/SPEC.md`](docs/SPEC.md) | arquitetura, modelo de ameaça, contrato de segurança |
| [`docs/MANUAL.md`](docs/MANUAL.md) | manual de uso: config, comandos, teclas, auditoria |
| [`docs/DECISOES.md`](docs/DECISOES.md) | registro de decisões e do porquê de cada uma |
| [`AGENTS.md`](AGENTS.md) | contexto para agentes de desenvolvimento |
| [`specs/`](specs/) | metodologia e spec por milestone |

## Estado

M1 a M13 entregues e validados em hardware — skeleton, tool loop, modo plano com gate, sandbox,
sessão persistente, docker/apt, agente de codificação, skills, interação de prompt, TUI, painéis,
gate informado, catálogo de modelos e painel de sessão legível.

**Não validado em hardware**: a camada `--check` do catálogo (pede confirmação, ainda não executa os
probes de tool calling) e a verificação bit-a-bit de prompt longo via `--run` (o cold start do
provedor no Core 2 Duo ultrapassa 600 s neste ambiente).

### Roadmap

- **`/compact`, `/new`, `/autocompact`** — especificados em paridade com o opencode (os três criam
  **sessão nova**, não apagam a atual). Requer `/sessions` para listar e trocar, senão o operador
  perde o acesso à sessão anterior.
- **Transporte Web** — SSE + shell-HTTP + `Approver` por canal, com auth/TLS/CSRF. O contrato de
  eventos já é agnóstico de transporte desde o M8.
- **`undo` de prompts executados** — reenviar um prompt antigo repete efeitos de tools sem
  rollback; exige rebranching explícito.

## Licença

Ainda não definida — o repositório ainda não tem arquivo `LICENSE`. Defina antes de qualquer
distribuição.
