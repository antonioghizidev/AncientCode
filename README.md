# AncientCode

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

## Instalação

Alvo único: **linux/amd64**, compilado com `GOAMD64=v1` para rodar em hardware sem AVX
(Core 2 Duo e mais antigo). Sem toolchain Go na máquina de destino.

### Via binário (recomendado)

```sh
curl -fsSL https://raw.githubusercontent.com/antonioghizidev/ancientcode/HEAD/install.sh | sh
```

Esse link é **estável**: aponta para o instalador do branch principal e **não muda de URL**
a cada release. O instalador em si tem uma versão **fixada** (`v0.16.0`), então rodar o comando
amanhã instala exatamente a mesma build de hoje — é reprodutível, e é por isso que `latest`
não é o padrão.

O instalador grava em `~/.local/bin`, sem `sudo`. Para outro local:

```sh
ANCIENTCODE_PREFIX=/usr/local/bin curl -fsSL .../HEAD/install.sh | sh
```

Para pedir explicitamente a última release estável:

```sh
ANCIENTCODE_VERSION=latest curl -fsSL .../HEAD/install.sh | sh
```

> **`latest` não alcança prereleases.** O GitHub não aponta `/releases/latest` para uma
> release marcada como pre-release. Se a mais recente for `-pre`, `latest` falha — e é o
> comportamento certo, porque `latest` caindo numa prerelease é surpresa. Nesse caso use a tag.

A página da última release estável é sempre
[`releases/latest`](https://github.com/antonioghizidev/ancientcode/releases/latest); ela
também aparece no menu **Releases** da barra lateral do repositório. As duas coisas só
existem quando há uma release **não** marcada como pre-release — enquanto a única for
`-pre`, o GitHub devolve 404 nesse caminho.

### Via `go install`

Só se a máquina **já tiver** Go 1.27+, o que o alvo não tem por definição.

```sh
# versão fixa (reprodutível)
go install github.com/antonioghizidev/ancientcode/cmd/ancientcode@v0.16.0

# última estável
go install github.com/antonioghizidev/ancientcode/cmd/ancientcode@latest
```

Atenção ao detalhe que só aparece aqui: `go install` **compila na máquina do usuário**, então um
`GOAMD64` exportado no shell quebra a compatibilidade com o Core 2 Duo silenciosamente. O caminho
do binário pré-compilado não sofre disso.

### Conferir o download à mão

```sh
TAG=v0.16.0
curl -fsSLO "https://github.com/antonioghizidev/ancientcode/releases/download/$TAG/SHA256SUMS"
curl -fsSLO "https://github.com/antonioghizidev/ancientcode/releases/download/$TAG/ancientcode_${TAG}_linux_amd64.tar.gz"
sha256sum -c SHA256SUMS
```

O `SHA256SUMS` não é assinado: protege contra corrupção e truncamento, **não** contra quem
controla o canal (ver D-86).

### Ainda não funciona para terceiros

O repositório está **privado**. As URLs acima devolvem 404 para quem não tem acesso — isso é
controle de acesso do GitHub, não limitação do instalador. Ver
[`specs/milestones/16-m16-distribuicao.md`](specs/milestones/16-m16-distribuicao.md) §6.

## Começando

**Requisito para build local**: Go 1.27+. No destino, só o binário.

```sh
# 1. configuração - a chave NUNCA vai no arquivo
cp ancientcode.example.yaml ancientcode.yaml
export OPENROUTER_API_KEY='...'      # ou NVIDIA_API_KEY / LITELLM_API_KEY

# 2. build para o alvo (o dev machine pode ser outro)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
  go build -trimpath -ldflags="-s -w -X main.version=dev" -o ancientcode ./cmd/ancientcode

# 3. executar
./ancientcode              # REPL
./ancientcode --tui        # interface de painéis (precisa de TTY real)
./ancientcode --run "..."  # não interativo (exige approval.write_file=allow)
```

`GOAMD64=v1` não é sugestão: impede o compilador de emitir instruções pós-SSE2, o que garante
execução no Core 2 Duo **por contrato** — uma build futura não introduz AVX por acidente.

Para build **local** (Windows ou macOS), dispense as flags de cross-compilação:

```sh
go build -o ancientcode ./cmd/ancientcode   # ou: go install ./cmd/ancientcode
```

### Provedores

| `provider` | endpoint | chave |
|---|---|---|
| `openrouter` | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` |
| `nvidia` | `https://integrate.api.nvidia.com/v1` | `NVIDIA_API_KEY` |
| `litellm` | `http://127.0.0.1:4000/v1` | `LITELLM_API_KEY` |

Qualquer endpoint compatível com OpenAI funciona por `base_url`. Overrides por ambiente:
`ANCIENTCODE_PROVIDER`, `ANCIENTCODE_BASE_URL`, `ANCIENTCODE_MODEL`, `ANCIENTCODE_WORKDIR`.

## Uso

### Comandos

| | |
|---|---|
| `/models [filtro]` | lista o catálogo do provedor — `--free`, `--all`, `--check`, ou texto |
| `/model [nome\|N]` | troca o modelo **só nesta sessão** (não persiste no YAML) |
| `/provider [nome]` | mostra a base URL ou troca de provedor na sessão |
| `/cd [pasta]` | troca a **raiz do projeto** (shell + file/code tools); sem argumento, mostra onde estamos |
| `/grant [pasta]` | libera escrita fora do projeto, até o fim da sessão; sem argumento, lista as concessões |
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

### Pastas: projeto, rascunho e casa do agente

São três coisas diferentes, e confundi-las é o que o `/cd` veio desamarrar.

| | onde é | muda quando |
|---|---|---|
| **projeto** | `project_root` | `/cd <pasta>` — shell e file/code tools passam a operar nela |
| **rascunho** | `<workdir>/scratch` | nunca; é saída descartável, sem gate |
| **casa do agente** | `workdir` | nunca: `sessions/`, `audit.jsonl`, config |

O `workdir` do YAML é a **casa do agente**, não a pasta onde você trabalha. É criada na
instalação e ninguém mexe. O `project_root` é a pasta do seu código e pode estar em qualquer
caminho — os dois são **irmãos**.

```
/cd ~/workspace/PROJETO     # vira a raiz do projeto
/cd                          # onde estamos?
/cd ~/workspace/PROJETO/pkg # desce: não pergunta
/grant ~/artefatos           # libera escrita ali, até o fim da sessão
/grant                       # lista as concessões
```

Confirmar é só quando o comando **amplia** o alcance (sobe na árvore). Descer não pergunta.
Na TUI, confirmar é **repetir o comando**.

**O que o harness não promete**: o `shell` não é confinado por pasta. O "caminho" de um comando
é texto livre, e `find / -name x` e `ls .` são ambos shell. Os controles reais dele são rodar
como `run_user` sem privilégio e passar pelo gate, que mostra o comando inteiro. O que se aperta
é a **escrita** fora do projeto e do rascunho.

`/cd` nunca aceita a casa do agente (nem dentro dela) — lá mora a credencial.

## Plataformas

O alvo é **Linux sem AVX** (Inspiron 1545 / Core 2 Duo). Linux é o único alvo onde tudo
funciona; o restante é suportado com ausências declaradas, não com degradação silenciosa.

| | Linux | Windows | macOS |
|---|---|---|---|
| TUI (`--tui`) | ✅ | ✅ | ✅ |
| `--run` (pipe/arquivo) | ✅ | ✅ | ✅ |
| Loop, tools, gate, auditoria | ✅ | ✅ | ✅ |
| `/models` paginado + `--check` | ✅ | ✅ | ✅ |
| `/connect` com entrada **mascarada** | ✅ | ❌ | ❌ |
| Edição de linha raw no REPL | ✅ | ⚠️ cooked | ⚠️ cooked |
| `0600` real no `auth.json` | ✅ | ❌ | ✅ |
| `run_user` sem privilégio | ✅ | ⚠️ usuário da sessão | ⚠️ usuário da sessão |

Três ausências, e cada uma **recusa** em vez de fingir:

1. **Entrada mascarada (`/connect`).** Não há driver raw fora do Linux
   (`secret_other.go`). O harness **não** faz fallback para ecoar a chave — em vez disso,
   `/connect` falha e manda usar `--use-env`. Uma chave aparecendo na tela não é um
   detalhe de UX.
2. **Edição raw.** Sem raw mode o REPL cai para modo *cooked*: `←`/`→` e histórico com
   `↑` não funcionam, e `Ctrl+C` chega como byte. O REPL e a TUI continuam utilizáveis —
   a TUI não depende de raw mode.
3. **Permissões de arquivo.** No Windows o bit `0600` é **ignorado** pelo sistema de
   arquivos: medido aqui, o `auth.json` fica `-rw-rw-rw-`. Por isso `tightenDir` não roda
   lá. **Consequência a levar em conta:** no Windows, qualquer processo local com acesso ao
   perfil do usuário consegue ler a chave. A defesa continua sendo o gate humano (§5.6 da
   SPEC), não a permissão do arquivo — mas se você não confia no host, use `--use-env`.

No Windows, para ter tudo, use a TUI (`--tui`): ela cobre os comandos que o raw mode do
REPLakia.

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
cmd/ancientcode/        REPL, TUI (Bubble Tea), wiring, portões de UI
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

[Apache-2.0](LICENSE) — inclui concessão explícita de patente, o que importa para uma ferramenta de
segurança que empresas podem instalar. Ver também [`CONTRIBUTING.md`](CONTRIBUTING.md), que registra
os invariantes que uma contribuição não pode quebrar.
