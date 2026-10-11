# AncientCode — Manual do Usuário

> Harness de agente de IA para o terminal. Notebook antigo (Core 2 Duo, sem AVX): o peso da
> inteligência fica no provedor remoto (OpenRouter / NVIDIA NIM / LiteLLM — contrato
> OpenAI-compatible). Tudo o que você digita é processado lá; aqui roda só o cliente fino.

## 0. Plataformas

O alvo é Linux amd64 sem AVX. **Linux é o único alvo onde tudo funciona**; nas outras
plataformas há ausências que o harness **recusa explicitamente**, em vez de degradar calado.

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

**O que isso significa na prática.**

1. **`/connect` não funciona fora do Linux.** Não existe driver de terminal raw para
   mascarar o que você digita, e o harness **não** faz fallback para ecoar a chave. Ele
   falha e manda usar `--use-env`:

   ```powershell
   $env:OPENROUTER_API_KEY = "sk-..."
   .\ancientcode.exe --tui
   ```

   O `auth.json` em si é multiplataforma — só a *digitação mascarada* que não é.

2. **No REPL fora do Linux a edição fica primitiva.** Modo *cooked*: `←`/`→` e `↑` para
   histórico não funcionam, e `Ctrl+C` chega como byte. A TUI (`--tui`) não depende de raw
   mode e é a melhor opção no Windows.

3. **No Windows o `auth.json` fica legível por qualquer processo local.** O bit `0600` é
   ignorado pelo sistema de arquivos (medido: `-rw-rw-rw-`), então `tightenDir` não roda lá.
   Isso **não** é a defesa contra o agente de qualquer forma (§5.6 da SPEC: a defesa real é
   o gate humano), mas num host compartilhado, prefira `--use-env`.

## 1. Execução

Baixe o binário `ancientcode` (um único arquivo, Linux amd64, `GOAMD64=v1`) e configure:

```sh
export NVIDIA_API_KEY=...            # ou OPENROUTER_API_KEY
./ancientcode                          # REPL interativo (modo padrão)
./ancientcode --tui                    # interface gráfica em terminal (M9)
./ancientcode --version                # imprime a versão e sai
```

A configuração fica em `ancientcode.yaml` (ou outra com `--config`). Exemplo mínimo:

```yaml
provider: nvidia
base_url: https://integrate.api.nvidia.com/v1
model: nvidia/nemotron-3.5-lightning-30b-a3b
api_key_env: NVIDIA_API_KEY
workdir: /opt/ancientcode
code_root: /opt/ancientcode/code
run_user: ancientcode
toolbox_mode: run          # run (tools imediatas) ou plan (planeja antes)
approval:
  default: ask             # toda tool mutante pede aprovação
```

### Flags principais

| Flag | O que faz |
|---|---|
| `--tui` | TUI (Bubble Tea). Exige stdin E stdout TTY; recusa pipes e `--run`. |
| `--run` | Modo não-interativo (script/automação). Exige `approval.write_file: allow` e ban tomadas interativas. |
| `--session <id>` | Sessão: guarda em `workdir/sessions/<id>.jsonl`. |
| `--resume <id>` | Reutiliza o histórico da sessão `<id>` (continua de onde parou). |
| `--reset` | Apaga a sessão atual antes de começar (incompatível com `--resume`). |
| `--cleanup <dias>` | Remove sessões com mais de N dias (nunca automático). |
| `--skill <nome>` | Ativa uma skill (M7). |
| `--verify-skill <nome>` | Verifica/estampa uma skill baixada antes de usar. |

## 2. Uso diário (REPL)

Dê um prompt e o modelo responde:

```
ancientcode 0.9.0-m9 — provider=nvidia model=nvidia/nemotron-3.5-lightning-30b-a3b session=default.jsonl
toolbox_mode=run (plan: nada mutante executa sem o seu 'y')
comandos: /exit /quit /reset /help /historico (!N repete) /retry /continue /model [nome] /provider [nome] /context | no plano: y aprova e executa, feedback:<msg> replaneja
> liste os arquivos em /opt/ancientcode/code
```

Se o modelo quiser rodar uma ferramenta mutante, aparece o **gate de aprovação**:

```
[aprovacao] tool "write_file"
  write_file path=/opt/ancientcode/code/relatorio.txt
executar? [y/N, d = negar sempre] y
```

| Resposta | Efeito |
|---|---|
| `y` / `yes` | aprova e executa uma vez |
| `n` / qualquer outra | nega (nada roda) |
| `d` / `deny` | nega **e veta essa tool pelo resto da sessão** |

`Ctrl+C`: cancela o turno que está em voo (`turno cancelado (Ctrl+C)`); no prompt, sai.

### Comandos `/`

| Comando | O que faz |
|---|---|
| `/exit` `/quit` | sai |
| `/reset` | zera a sessão atual |
| `/help` | lista os comandos |
| `/historico` | lista prompts recentes numerados (redigidos) |
| `!1`, `!2`, … | reenvia o prompt 1 = mais recente, 2 = o anterior… |
| `/retry` `/continue` | reexecuta a última linha (retry) ou pede continuação |
| `/model` | mostra o modelo atual |
| `/model <nome>` | troca o modelo **só nesta sessão** (não grava no yaml) |
| `/model <N>` | escolhe o **Nº da última listagem** do `/models` |
| `/models [filtro]` | lista os modelos do provedor (ver §3.1) |
| `/provider` | mostra a base URL e as opções disponíveis |
| `/provider <nome>` | troca de provedor nesta sessão (ex.: `openrouter`) |
| `/context` | mostra uso de contexto: `ctx: 12% (3400/28000 bytes)`; `off` se `session_context_bytes=0` |

### Modo plano (`toolbox_mode: plan`)

O turno começa no modo leitura. O modelo apresenta o plano e você decide:

```
[plano] executar? [y/N, feedback:<msg>] y        # aprova e executa o plano
[plano] executar? [y/N, feedback:<msg>] feedback:nao apague logs antigos
[plano] executar? [y/N, feedback:<msg>] n        # descarta
```

## 3. A TUI (`--tui`)

Modo gráfico em terminal, no estilo lazydocker/lazygit. Tudo o que vale no REPL vale aqui:
mesmas políticas, mesma auditoria, mesmo `Approver` — só muda a cara.

```
┌─────────────────────────▸ sessão─────────────────────────┬───────────────atividade───────────────┐
│> liste os arquivos em code                             ░│* modelo: aguardando o primeiro toke.. ░│
│< Aqui está a lista: 3 arquivos                         █│* modelo: recebendo tokens...        █│
│                                                        ░│* gate: write_file                  ░│
│                                                        ░│* tool: write_file ok 0.01s         ░│
└────────────────────────────────────────────────────────┴──────────────────────────────────────┘
────────────────────────────────────────────────────────────────────────────────────────────────────
[aprovar] write_file | y aprova, n nega, d nega sempre
> escreva o arquivo relatorio.txt▏
ancientcode 0.10.0-m10 | nvidia | nvidia/nemotron-3.5-lightning-30b-a3b | run | default.jsonl
ctx ▓░░░░ 20% | ◎ ok
```

**Estrutura da tela**

| Parte | O que é |
|---|---|
| Painel **sessão** (esquerda, ~60%) | histórico da conversa (`>` você, `<` modelo) |
| Painel **atividade** (direita, ~40%) | fases do loop: modelo, gate e execução de tools |
| Divisória `─` | separa os painéis da área de comando |
| **Gate** | aparece acima do input quando há aprovação pendente |
| Linha de **input** | `> ` + texto, cursor destacado no caractere sob o caret |
| **Rodapé** (2 linhas) | versão/provedor/modelo/modo/sessão + contexto e saúde |

**Scrollbar**: cada painel tem uma coluna própria (`░` trilha, `█` posição do thumb). Ela só
aparece quando o conteúdo é maior que o visível — o tamanho do texto nunca muda.

**Foco**: o painel sob controle das setas é marcado com `▸` e fica em ciano. `Tab` alterna entre
sessão e atividade; as setas `↑`/`↓` passam a rolar esse painel. Digitar qualquer tecla (ou
`Enter`) devolve o controle para a linha de input.

O rodapé mostra: versão | provedor | modelo | modo | sessão (linha 1); medidor de contexto em 5
células + indicador de saúde (linha 2):

```
ancientcode 0.10.0-m10 | nvidia | nvidia/nemotron-3.5-lightning-30b-a3b | run | default.jsonl
ctx ▓░░░░ 20% | ◎ ok
```

Sem orçamento de contexto (`session_context_bytes=0`): `ctx -- | ◎ ok`. Acima de 80% aparece `!`

### Teclas

| Tecla | Efeito |
|---|---|
| `Enter` | envia o prompt / submete |
| `Tab` / `Shift+Tab` | alterna o foco entre os painéis sessão ↔ atividade |
| `←` `→` | com painel focado: troca de painel; no input: move o cursor |
| `↑` `↓` | com painel focado: rola o painel; no input: histórico de prompts |
| `PgUp` `PgDown` | pagina o painel focado |
| `Home` `End` | topo/fim do painel focado (no input: início/fim da linha) |
| `r` `R` | com erro no banner: refaz /retry |
| `c` `C` | com erro no banner: limpa o erro |
| `Backspace`/`Delete` | edição da linha |
| `Ctrl+C` | em turno: cancela; no gate: nega; sem turno: sai da TUI |
| `/…` | qualquer comando do REPL funciona igual (`/model`, `/provider`, `!1`, `/exit`…) |

### Aprovações na TUI

Quando o modelo pede uma tool, o gate mostra **o que será executado** — o comando em destaque,
não um JSON:

```
[aprovar] shell | y aprova, n nega, d nega sempre
  rm -rf /tmp/build
```

O painel de atividade registra a mesma informação:

```
* gate: shell  rm -rf /tmp/build
* tool: shell (executando)
* tool: shell ok 0.31s
```

O que aparece em destaque depende da tool:

| Tool | Mostrado |
|---|---|
| `shell` | a linha de comando (`cmd`) |
| `docker` | `subcommand` + `args` (ex.: `ps -a`) |
| `apt` | `action` + `packages` (ex.: `install nginx`) |
| `write_file` / `edit_file` / `read_file` | o `path` |
| `code_scaffold` | `template` + `name` |
| `skill_run` | `skill` + `script` + `argv` |

Texto longo quebra em até 2 linhas com `…` no fim (nunca é cortado em silêncio). Se a tool
mandar argumentos que a UI não reconhece, os args crus são mostrados — em nenhum caso o payload é
oculto.

| Tecla | Efeito |
|---|---|
| `y` | aprova e executa (auditoria grava `policy=ask decision=allow`) |
| `n` | nega esta vez |
| `d` | nega **e veta** a tool pelo resto da sessão |
| `Ctrl+C` | cancela o gate (equivale a `n`) |

Nenhuma outra tecla mexe no gate: ele só fecha com y/n/d/Ctrl+C. O resultado de cada decisão fica
no painel de atividade e no rodapé (`◉ ok` → `◦ espera` → `✗ erro`).

### Erros

Quando um turno falha, o erro aparece em **dois lugares**:

- **banner em vermelho** logo acima do input, enquanto durar:
  ```
  erro: loop: aprovacao de "shell" expirou; recusado (fail-closed)
  > escreva o relatorio▏
  ```
- **linha no histórico** do painel de sessão (`! erro: …`), que continua disponível ao rolar.

Digitar qualquer tecla ou `c` limpa o banner; o histórico permanece.

> **Segurança do paint**: nada que venha do modelo ou de uma tool consegue emitir sequências de
> controle ANSI na tela. Caracteres de controle (inclusive ESC) são removidos antes de desenhar —
> caso contrário um `\x1b[2J` no retorno de uma tool poderia apagar o próprio gate e torná-lo
> inconfiável. Segredos são mascarados antes disso.

### 3.2 Dar acesso do agente às suas pastas (`/cd`)

O harness roda como **usuário próprio** (`ancientcode`), não como você. É deliberado: o agente
fica com menos privilégio que o operador. O preço é que ele não enxerga — nem escreve — nas
suas pastas até você dar acesso.

O `/cd` **avisa** quando a pasta não é gravável, e diz qual usuário precisa de acesso e com
o quê. Se você viu só isso, é o que falta:

```sh
# 1. o caminho precisa ser atravessável (+x sem listagem: o agente entra, não vasculha)
sudo chmod 711 /home/SEU_USUARIO

# 2. a pasta de trabalho passa a ser do grupo do harness
sudo chgrp -R ancientcode /home/SEU_USUARIO/workspace/SEU_PROJETO

# 3. e o grupo ganha escrita
sudo chmod -R g+rwX /home/SEU_USUARIO/workspace/SEU_PROJETO

# 4. setgid nas pastas — ESSA é a etapa que se esquece
sudo find /home/SEU_USUARIO/workspace/SEU_PROJETO -type d -exec chmod g+s {} \;
```

**Por que o passo 4 não é opcional.** Sem `setgid`, os arquivos que **você** criar depois
nascem com o grupo do dono e sem escrita para o grupo. O acesso que você acabou de liberar
vale só para o que já existe, e some no primeiro `git clone` ou no primeiro arquivo novo —
sem nenhum aviso. Com `setgid`, o grupo é herdado pelas pastas e o acesso se sustenta.

Pelo mesmo motivo, o seu shell precisa de um `umask` que deixe o grupo escrever:

```sh
umask 002        # arquivos 664, pastas 775
echo 'umask 002' >> ~/.bashrc
```

**Conferindo:**

```sh
sudo -u ancientcode touch /caminho/do/projeto/.prova && echo "ESCREVE" || echo "NAO ESCREVE"
sudo -u ancientcode rm -f /caminho/do/projeto/.prova
cd ~ && ./ancientcode && /cd          # onde o harness está agora
```

Se `ESCREVE` falhar depois de um tempo, quase sempre é um arquivo novo em diretório sem
`setgid` — refaça o passo 4.

**Para ler sem escrever**, basta o passo 1 (o `711`). O `/cd` aceita e segue: revisar um
projeto somente-leitura é legítimo, e recusar transformaria um aviso em obstáculo.

**Se preferir rodar com o seu usuário**, nenhum `chgrp` é necessário. O custo é real: o agente
passa a ter **os seus** privilégios, e o `run_user` precisa ser o seu login — o executor
recusa `run_user` diferente do usuário atual (ele não faz `su`, recusa).

## 3.1 Escolher o modelo (`/models`)

O provedor expõe um catálogo (`GET {base_url}/models`). O critério de "funciona aqui" é mais
estreito que "é um modelo de chat": o agente precisa de um modelo que emita **tool calls**.

### No REPL

```
> /models
*   1. nvidia/nemotron-3.5-lightning-30b-a3b  [?, tools?]      <- * = modelo ativo
    2. meta/llama-3.1-8b-instruct  [chat, tools?]
    3. deepseek/deepseek-chat  [chat, tools, free, ctx 64000]
    4. nvidia/nv-embedqa-e5-v5  [embedding, tools?]            <- escondido, use --all
  66 de 81 visiveis | 0 gratuitos | 0 sem tool calling confirmado | 15 nao-chat ocultos
  selecione com /model <numero>
```

| Filtro | Efeito |
|---|---|
| `/models` | só o que pode servir de agente (oculta embedding/rerank/guard/vision/…) |
| `/models llama` | busca por substring no id, no nome **ou na família** |
| `/models --all` | inclui as famílias não-chat (útil para diagnóstico) |
| `/models --free` | só modelos com preço zero — **só onde o provedor informa preço** |
| `/models --check` | verificação real: 1 requisição por modelo candidato (gasta tokens; pede confirmação) |

Depois, escolha pela posição:

```
> /model 3
model: deepseek/deepseek-chat (vale nesta sessao — nao persiste no yaml)
```

Índice fora da lista é recusado e **nada é trocado**: `erro: indice 9999 fora da lista (66 modelos)`.

### Na TUI

`/models` abre um **painel dedicado** com busca embutida:

```
┌──────────────────────────sessão──────────────────────────┬── atividade ────────┐
│  catalogo: 81 modelos no provedor, 66 uteis para este age │  filtrando: nemot    │
│                                                            │  > * 1. nvidia/nemot…│
│                                                            │    2. nvidia/llama-3…│
│                                                            │    3. nvidia/nemotro…│
│                                                            │  1 de 81 visiveis |…│
│                                                            │  Enter seleciona …   │
└────────────────────────────────────────────────────────────┴─────────────────────┘
```

| Tecla | Efeito |
|---|---|
| qualquer caractere | filtra a lista ao vivo |
| `Backspace` | apaga um caractere da busca |
| `↑` `↓` `PgUp` `PgDown` `Home` `End` | move o destaque |
| `Enter` | troca para o modelo destacado |
| `Esc` / `Ctrl+C` | cancela sem trocar |
| `Ctrl+F` | alterna "só gratuitos" |

### O que os rótulos significam

| Rótulo | Significado |
|---|---|
| `chat`, `?` | família reconhecida / **não reconhecida** (nada é inventado) |
| `tools?` | o provedor **não informa** se suporta tool calling (caso do NIM) |
| `tools` / `sem tools` | o provedor **declarou** suporte (ou ausência) |
| `free` | preço zero declarado pelo provedor |
| `ctx 128000` | tamanho de contexto, quando informado |
| `verificado` | passou no `--check` (request real com tool calling) |

> **Fidelidade depende do provedor.** O OpenRouter publica capacidades e preço, então a lista é
> precisa e `--free` funciona. O **NVIDIA NIM publica apenas o id**: por isso os rótulos aparecem
> como `tools?` e a UI avisa `provedor nao informa preco: filtro --free indisponivel aqui` em vez
> de mostrar uma lista vazia. Nada é suposto — quando não se sabe, aparece `?`.

### 4.1 Teclas da TUI

| Tecla | Efeito |
|---|---|
| `Enter` | envia o prompt |
| **`Esc`** | **interrompe o turno em andamento** (sem turno, não faz nada) |
| `Ctrl+C` | 1º interrompe o turno; 2º (sem turno) sai |
| `↑` `↓` | no prompt: navega/edita prompts anteriores; com painel focado: rola o painel |
| `Tab` / `Shift+Tab` | alterna o foco entre os dois painéis |
| `Shift+←` / `Shift+→` | rolagem **horizontal** do painel focado |
| `PgUp` / `PgDown` | rola uma página |
| `Home` / `End` | topo/fim do painel (ou início/fim da linha no prompt) |
| `r` | retenta o último turno — **só com a linha de entrada vazia** |
| `c` | limpa o banner de erro — **só com a linha de entrada vazia** |
| `/` | comando (mesmo parser do REPL) |

> `r` e `c` só são atalhos quando **não há nada digitado**. Com texto na linha, `r` é a letra `r`.

### 4.2 Ler respostas longas

Os dois painéis **quebram linha** (continuação indentada com dois espaços) e guardam o texto
inteiro — nada é cortado em silêncio. A rolagem é **vertical por linha física**, então uma resposta
de 40 linhas se lê linha a linha com `↑`/`↓`.

A atividade ocupa ~30% da largura e tem um piso de 28 células (o catálogo de modelos e o detalhe do
gate precisam de largura para não virarem uma pilha de `↳`).

**Colunas mais largas que o painel** — um caminho, uma URL, um JSON de uma linha — não têm ponto de
quebra; para elas use `Shift+←`/`Shift+→`. O título do painel mostra a posição
(`col 40/120`) quando há texto à direita.

### 4.3 Sinais de que o turno está em andamento

- Um **spinner** (`⠹ processando…`) no topo do painel de atividade, que só existe **dentro** de um
  turno. Terminal sem Braille recebe a versão ASCII (`|/-\`), que também gira.
- Um spinner **parado** não existe: ele confirmaria justamente o congelamento que o spinner serve
  para descartar.

### 4.4 Interromper, corrigir, continuar

| Situação | O que fazer |
|---|---|
| o agente foi pela rota errada | `Esc` — o prompt **volta para a linha de entrada**, cursor no fim |
| corrigir e reenviar | edite e `Enter` |
| repetir sem retipar | `/continue` — reenvia o prompt **cancelado** |
| cancelamento acidental | `/continue` de novo |

`/continue` **avisa quais tools já executaram** antes de reenviar, porque reenviar o prompt repete
o efeito delas:

```
! turno cancelado (Esc) — edite e reenvie | tools que ja executaram: write_file, shell
atencao: reenviando; ja executaram: write_file, shell
```

Cancelar **não** é registrado como erro: o turno interrompido é um estado próprio, e o rodapé não
fica vermelho.

## 5. Auditoria e limpeza

Todo comando mutante com decisão vai para `workdir/audit.jsonl` (append-only, argumentos
redigidos, só hash da saída):

```json
{"timestamp":"...","tool":"write_file","args":"{\"content\":\"...\",\"path\":\"relatorio.txt\"}","status":"executed","output_hash":"1d14a...","policy":"ask","decision":"allow","duration":"8.204ms"}
```

Sessões ficam em `workdir/sessions/<id>.jsonl`; limpeza só explícita:
`./ancientcode --cleanup 30`.

## 5. Dicas de segurança

- Chave de API **só** em variável de ambiente; nunca no yaml, nos prompts ou em arquivos gravados
  pelo agente.
- Modo `deny-by-default`: só executa o que **você** aprovar. Ferramentas que mudam o sistema
  (`shell`, `apt`, `docker`, `write_file`, `edit_file`, `code_scaffold`) exigem `ask`.
- `shell` é executada por vetor de argumentos em usuário não-privilegiado (`run_user`), com
  whitelist e timeout — o modelo nunca monta linha de shell.
- O agente confina arquivos em `code_root`: caminhos absolutos fora dali são recusados.