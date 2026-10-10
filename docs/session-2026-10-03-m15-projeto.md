# Sessão `2026-10-03` — `m15-projeto-rascunho`

> Resumo exclusivo desta sessão. Metodologia: `specs/methodology.md`.
> **Smoke no Inspiron NÃO executado** — o Dell estava desligado. Tudo abaixo está validado em
> host (Windows) e WSL, e o hardware continua sendo o portão de aceitação.

## Objetivo da sessão

Permitir que o operador **mude a pasta de trabalho durante a execução**, e que essa pasta seja a
**raiz do projeto**: onde o shell roda e onde as file/code tools leem e escrevem — separada da
**casa do agente** (config, `sessions/`, `audit.jsonl`, scratch), que fica em outro caminho.

O pedido nasceu de uma impossibilidade concreta: a topologia real do usuário (agente instalado
num caminho, fonte em `/home/USUARIO/workspace/projeto`) **não subia**, porque `config.Validate()`
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
| `docs/DECISOES.md`, SPEC, README | ✅ |
| Nota de sessão | ✅ esta |
| **Smoke no Inspiron** | ❌ **não executado** |
| 2 dos 8 testes de segurança da §6 | ❌ sem teste explícito |

## Não validado — o smoke precisa confirmar

1. `/cd` troca a raiz **e o shell acompanha** (`pwd`), que é o defeito que o `RootRef` existe
   para impedir.
2. `audit.jsonl` **não** muda de caminho depois do `/cd`.
3. Rascunho: `go build > log.txt` no scratch não suja o repositório.
4. `/grant` libera escrita de verdade no caminho concedido.
5. A raiz volta em `state.json` na execução seguinte.

## Próxima sessão

- [x] Escrever os **2 testes de segurança que faltavam** (§6): `/cd` não exposto ao modelo, e
      `audit.jsonl` imutável após `/cd` — ambos com mutação.
- [ ] **Smoke no Inspiron** com o binário `GOAMD64=v1`:
  - [ ] os 5 itens de "Não validado" acima;
  - [ ] renomear o usuário de execução do smoke (o `run_user` do config já é
        `ancientcode`, então hoje ele **não casa** com o usuário real do host);
  - [ ] verificar se a casa do agente atual (o diretório pessoal com o nome antigo do projeto)
        pode ser removida depois da renomeação, e limpar os resíduos de smoke.
- [ ] Push da branch e PR.
