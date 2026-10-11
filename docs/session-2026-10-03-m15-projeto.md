# Sessão `2026-10-03` — `m15-projeto-rascunho`

> Resumo exclusivo desta sessão. Metodologia: `specs/methodology.md`.
> **Smoke no Inspiron EXECUTADO** — e validou o caso mais difícil do milestone: o harness roda
> numa conta (`ancientcode`) e o projeto fica na conta de outra pessoa. Ver §Smoke.

## Objetivo da sessão

Permitir que o operador **mude a pasta de trabalho durante a execução**, e que essa pasta seja a
**raiz do projeto**: onde o shell roda e onde as file/code tools leem e escrevem — separada da
**casa do agente** (config, `sessions/`, `audit.jsonl`, scratch), que fica em outro caminho.

O pedido nasceu de uma impossibilidade concreta: a topologia real do usuário (agente instalado
num caminho, fonte em `/home/USUARIO/workspace/PROJETO`) **não subia**, porque `config.Validate()`
exigia `code_root ⊂ workdir`.

## O que foi feito

- **`internal/workspace`** — dono das duas pastas. Recusa a raiz que seja, esteja dentro **ou
  contenha** a casa do agente; `Widens` distingue ampliar de estreitar; `Grant` concede escrita
  até o fim da sessão.
- **`workspace.Ref`** — a indireção que faz a troca chegar nas tools. `files.Files`,
  `code.Toolset` e `sandbox.Executor` passaram a ler a raiz por `func() string`.
- **Config** — `project_root` e `scratch` novos; `workdir` passa a significar só a casa do
  agente; **invariante `code_root ⊂ workdir` removido**.
- **`/cd <pasta>`** no REPL (com `y/N`) e na TUI (por repetição do comando); **`/cd`** sem
  argumento mostra onde estamos; **`/grant <pasta>`** libera escrita fora do projeto.
- **UI sempre visível** — a pasta aparece no banner do REPL e no rodapé da TUI.
- **Persistência** — `project_root` no `state.json`, gravado no `/cd` e restaurado no startup.
- **Docs** — `specs/milestones/15-...`, `D-2026-10-03-76..80`, SPEC §10.3, README.

## Decisões

- **D-2026-10-03-76** — raiz do projeto e casa do agente são **irmãs**. O invariante `⊂` saiu.
- **D-2026-10-03-77** — o **shell não é confinável por path**, e o harness diz isso por escrito.
  Leitura segue pelo gate (comando inteiro visível); o que se aperta é **escrita** fora de
  projeto+rascunho.
- **D-2026-10-03-78** — a raiz tem de ser lida por função, porque `Registry.Register` recusa
  duplicata e não há setter em nenhuma das três.
- **D-2026-10-03-79** — confirmar por repetição na TUI, `y/N` no REPL; só quando **amplia**.
- **D-2026-10-03-80** — `project_root` no `state.json` é confiança; validado como entrada remota.

## Testes

| Pacote | Foco | Status |
|---|---|---|
| `internal/workspace` | raízes, symlink, grant, `Widens` | ✅ |
| `internal/config` | `project_root`/`scratch`, invariante removido, env | ✅ |
| `internal/state` | round-trip e validação do path | ✅ |
| `internal/tools/files` | raiz lida no momento do uso | ✅ |
| `cmd/ancientcode` | `/cd`, `/grant`, TUI, banner, rodapé | ✅ |
| `internal/sandbox` | executor com workdir dinâmico | ✅ |

`go vet` e `go build` limpos · suíte completa verde · `-race` no WSL verde nos 4 pacotes ·
**blobs `gofmt` 123/123 limpos** (o gate é por blob: no host Windows o `core.autocrlf` escreve
CRLF e `gofmt -l` acusa todos — ver AGENTS.md).

**Mutações: 11 workspace + 6 ref + 4 config + 9 cd + 7 persistência/UI = 37**, todas com
cobertura, mais **1 registrada como EQUIVALENTE** (ver lições).

## Três coisas que só apareceram ao implementar

1. **A checagem de symlink estava na ordem errada.** A recusa da casa do agente rodava sobre o
   caminho **literal**; se o confine usa o resolvido, valida-se uma coisa e prende-se outra.
   D-76.

2. **"A raiz contém a casa do agente" era uma lacuna real.** A primeira versão só recusava
   "dentro". Com a casa em `~/ancientcode` e o projeto em `~`, qualquer `write_file` alcançaria
   `auth.json` — o confine é por **diretório**, não por nome. Um teste meu pegou.

3. **A recusa vinha depois do prompt.** `/cd` para a casa do agente só falhava no
   `SetProjectRoot`, isto é, **depois** de perguntar. Pedir consentimento para algo que nunca
   seria aceito ensina que o prompt é decorativo. Agora `CanUse` valida antes.

## Quatro coisas que estavam órfãs

Achei ao marcar os checkboxes, e são a razão de o checklist existir:

- **`projectLine` só era usada por teste** — nenhuma tela a renderizava. Agora está no rodapé
  da TUI (`footerInfo.Project`).
- **`state.SetProjectRoot` não tinha chamador** — a persistência existia mas não acontecia.
  Agora o `/cd` grava em REPL e TUI, e o startup restaura.
- **Nenhum dos dois tinha mutação.** Adicionei 7, e foi aí que "a pasta some do rodapé" e "a raiz
  não é persistida" viraram testes que falham.

## Lições de processo (quarta vez nesta milestone-family)

- **Uma mutação EQUIVALENTE não é cobertura.** A [H] removia o `CanUse` do `/grant`, mas
  `workspace.Grant` checa a mesma condição: o comportamento não mudava e o teste ficava verde.
  Verde ali não era buraco — era **eu medindo a linha errada**. Registrei como equivalente em
  vez de inflar o placar.
- **PowerShell não serve para editar texto UTF-8.** Quebrei um arquivo e depois escrevi
  `cat\`logo` no lugar de `catálogo` numa entrada do DECISOES. A lição já estava registrada
  (D-2026-10-02-62) e eu a repeti. Usei a ferramenta de edição depois disso.
- **`gofmt -l` acusou 48 arquivos** depois de um `git checkout`. Não era defeito: `autocrlf`
  escreveu CRLF na árvore, e os **blobs estão limpos** (123/123). Registrado no AGENTS.md.
- **Um teste que passa pelo motivo errado continua passando pelo motivo errado.** Aconteceu três
  vezes nesta sessão (ranking, busca na paginação, grant) e em todas a mutação foi o que
  revelou.

## Um flake, honestamente

`TestExecOutputTruncated` (sandbox) falhou **uma** vez na suite completa. **Causa raiz
encontrada e corrigida:** o teste da **1 segundo** de timeout para um helper que **re-executa o
proprio binario de teste**. Medido: levava **0,96s de 1s disponivel** - margem de 40ms. Com as
mutacoes WSL carregando o disco, o helper demorava mais que 1s e o teste falhava por
**timeout**, que e uma propriedade diferente da truncagem que ele deveria verificar.

Subiu para 15s; o timeout tem teste proprio. Seis execucoes limpas depois da correcao. A licao
que fica: **um teste que mede uma coisa nao deve poder falhar por outra.** A margem de 40ms
escondia isso atras de um "flake".

## Critérios de aceite

| Critério | Status |
|---|---|
| Suíte completa verde | ✅ |
| `-race` no WSL | ✅ |
| `gofmt`/`vet`/`build` | ✅ (gofmt por blob) |
| Mutação de cada proteção | ✅ 37 + 1 equivalente |
| `docs/DECISOES.md`, SPEC, README, MANUAL | ✅ |
| Nota de sessão | ✅ esta |
| 8 testes de segurança da §6 | ✅ todos com mutação |
| **Smoke no Inspiron** | ✅ executado |

## Smoke no Inspiron — executado

O caso que nenhum teste pega: **o harness numa conta, o projeto na conta de outra pessoa.**

```
[aprovacao] tool "shell"
  shell {"cmd":"pwd > ONDE-EU-ESTOU.txt"}
>>> arquivo contém: /home/USUARIO/workspace/PROJETO
    dono: ancientcode:ancientcode   modo: 664
```

| | evidência |
|---|---|
| shell roda na raiz do projeto | o arquivo saiu com o path do projeto, gravado pelo processo de outra conta |
| `audit.jsonl` **não** se move | gravou em `<agent_home>/`; `find` no projeto → nenhum |
| rascunho não suja o repo | arquivo no scratch; projeto e subpastas limpos |
| `/cd` persiste entre execuções | `state.json` gravou; a execução seguinte reabriu nela |
| topologia antes impossível | projeto sob `~/workspace`, scratch sob `<agent_home>` — **irmãs** |
| `/cd` para a casa do agente | recusado, **antes** de pedir confirmação |

### O que o hardware achou que o host não achava

1. **`/cd` aceitou uma pasta não gravável, calado.** O harness lia o projeto e não escrevia;
   o operador trocava a raiz e a escrita falhava três comandos depois, com mensagem sem
   relação. Corrigido: o `/cd` **avisa** e diz qual usuário precisa de acesso e com o quê
   (MEDIANTE, nunca por bit de permissão — ACL e mount discordam do modo).
2. **Restauração ressuscitava pasta apagada.** O `state.json` apontava para uma pasta removida;
   o startup tentou restaurar e o `MkdirAll` só falhou por acaso (o usuário do harness não
   pode criar em `/home`). Agora restaurar **recusa criar** — `/cd` cria porque o operador pode
   estar digitando o projeto que vai clonar; um startup não pode desfazer uma remoção.
3. **`/grant` era bookkeeping.** O smoke mostrou que a concessão não mudava nada: o shell
   escrevia lá de qualquer jeito e as file tools recusavam. Agora as file/code tools consultam
   a concessão.

## Não validado

- **`--check` dentro da TUI**: a TUI recusa a flag e aponta o REPL. Antes ela a ignorava em
  silêncio — o mesmo defeito que este milestone remove do REPL.
- **`litellm`**: sem instância para medir.
- **`OpenRouter` sem crédito** (402 até em `:free`): o turno real do smoke passou pelo NVIDIA.
- **Integração bit-a-bit via `--run`**: o cold start do NIM excede 600 s neste ambiente.

## Próxima sessão

- [ ] Mergear o PR #11.
- [ ] `--check` dentro da TUI.
- [ ] Instância LiteLLM para medir a estratégia de verificação.
- [ ] Driver raw do Windows (`golang.org/x/term`), se a experiência de edição justificar.
