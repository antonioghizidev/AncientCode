# AncientAI — Manual do Usuário

> Harness de agente de IA para o terminal. Notebook antigo (Core 2 Duo, sem AVX): o peso da
> inteligência fica no provedor remoto (OpenRouter / NVIDIA NIM / LiteLLM — contrato
> OpenAI-compatible). Tudo o que você digita é processado lá; aqui roda só o cliente fino.

## 1. Execução

Baixe o binário `ancientai` (um único arquivo, Linux amd64, `GOAMD64=v1`) e configure:

```sh
export NVIDIA_API_KEY=...            # ou OPENROUTER_API_KEY
./ancientai                          # REPL interativo (modo padrão)
./ancientai --tui                    # interface gráfica em terminal (M9)
./ancientai --version                # imprime a versão e sai
```

A configuração fica em `ancientai.yaml` (ou outra com `--config`). Exemplo mínimo:

```yaml
provider: nvidia
base_url: https://integrate.api.nvidia.com/v1
model: nvidia/nemotron-3.5-lightning-30b-a3b
api_key_env: NVIDIA_API_KEY
workdir: /opt/ancientai
code_root: /opt/ancientai/code
run_user: ancientai
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
ancientai 0.9.0-m9 — provider=nvidia model=nvidia/nemotron-3.5-lightning-30b-a3b session=default.jsonl
toolbox_mode=run (plan: nada mutante executa sem o seu 'y')
comandos: /exit /quit /reset /help /historico (!N repete) /retry /continue /model [nome] /provider [nome] /context | no plano: y aprova e executa, feedback:<msg> replaneja
> liste os arquivos em /opt/ancientai/code
```

Se o modelo quiser rodar uma ferramenta mutante, aparece o **gate de aprovação**:

```
[aprovacao] tool "write_file"
  write_file path=/opt/ancientai/code/relatorio.txt
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
ancientai 0.10.0-m10 | nvidia | nvidia/nemotron-3.5-lightning-30b-a3b | run | default.jsonl
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
ancientai 0.10.0-m10 | nvidia | nvidia/nemotron-3.5-lightning-30b-a3b | run | default.jsonl
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

## 4. Auditoria e limpeza

Todo comando mutante com decisão vai para `workdir/audit.jsonl` (append-only, argumentos
redigidos, só hash da saída):

```json
{"timestamp":"...","tool":"write_file","args":"{\"content\":\"...\",\"path\":\"relatorio.txt\"}","status":"executed","output_hash":"1d14a...","policy":"ask","decision":"allow","duration":"8.204ms"}
```

Sessões ficam em `workdir/sessions/<id>.jsonl`; limpeza só explícita:
`./ancientai --cleanup 30`.

## 5. Dicas de segurança

- Chave de API **só** em variável de ambiente; nunca no yaml, nos prompts ou em arquivos gravados
  pelo agente.
- Modo `deny-by-default`: só executa o que **você** aprovar. Ferramentas que mudam o sistema
  (`shell`, `apt`, `docker`, `write_file`, `edit_file`, `code_scaffold`) exigem `ask`.
- `shell` é executada por vetor de argumentos em usuário não-privilegiado (`run_user`), com
  whitelist e timeout — o modelo nunca monta linha de shell.
- O agente confina arquivos em `code_root`: caminhos absolutos fora dali são recusados.