# M15 — `/cd`: raiz do projeto separada da casa do agente, e pasta de rascunho

**Status**: ⛔ pendente (branch `m15-projeto-rascunho`)
**Depende de**: M14
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Permitir que o operador **mude a pasta de trabalho durante a execução**, e que essa pasta seja a
**raiz do projeto**: onde o shell roda e onde todas as file/code tools leem e escrevem — sem
confundir essa raiz com a **casa do agente** (config, `sessions/`, `audit.jsonl`, temporários),
que fica em outro caminho e nunca é o que o agente edita.

## 2. Motivação — a topologia real hoje é impossível

O usuário roda o harness instalado em `/home/ancientai` e trabalha em
`/home/USUARIO/workspace/projeto`. **Essa configuração não sobe.** `config.Validate()` exige:

```go
if !withinDir(c.Workdir, c.CodeRoot) {         // internal/config/config.go:265
    return errors.New("config: code_root must be inside workdir (SPEC §10.3)")
}
```

`/home/USUARIO/workspace/projeto` não está dentro de `/home/ancientai`, então a validação falha.
Esse invariante nasceu no M6, quando as duas pastas eram irmãs dentro de `/home/ancientcode` e
o `workdir` ainda era uma pasta genérica. Ele **não protege nada que importe** e é exatamente o
que impede a separação pedida.

A nomenclatura também está invertida em relação à intuição do operador: `workdir` **não** é a
pasta onde se trabalha — é a casa do agente; e `code_root` é a raiz do projeto, mas tem nome de
"código-fonte-só". Um `workdir: /home/ancientai/tasks` no YAML parece "pasta de trabalho" e na
verdade move logs e sessões.

### 2.1 Por que hoje o shell roda em pasta diferente da do projeto

Não foi decisão: foi acumulação. **M1** nasceu `workdir` como "onde o harness guarda o que é
dele" (`sessions/`, `audit.jsonl`); **M4** deu-lhe o papel de `proc.Dir` do shell; **M6** criou
`code_root` strict-child porque as tools de arquivo precisavam de uma raiz mais apertada que a do
shell (o agente de código é `allowlist` separado). **Nunca houve uma decisão de que o shell não
deveria enxergar o código** — só nunca foi pedido.

## 3. Decisões tomadas com o usuário

| decisão | resposta | consequência |
|---|---|---|
| O que `/cd` troca | **raiz do projeto** (shell + file/code tools) | `shell` passa a rodar junto com as tools, como no opencode/claude-code |
| Casa do agente | **continua no YAML**, imutável na sessão | audit e sessão **não** acompanham a troca |
| Destino | **qualquer pasta** | exige a remoção do invariante `code_root ⊂ workdir` |
| Confirmação | **só quando amplia** | estreitar não pede |
| Persistência | `state.json`, lembra do último projeto | campo novo + validação de path |
| Leitura fora do projeto | **segue pelo gate do shell**, sem confinamento por path | ver §5.2 |

## 4. Escopo / entregáveis

### 4.1 Config

- [ ] `project_root` (novo) — a raiz do projeto; default `~/.config/ancientcode/project` para
      não quebrar quem nunca configurou.
- [ ] `scratch` (novo) — default `<agent_home>/scratch`.
- [ ] `workdir` passa a significar **só** a casa do agente, com o comentário corrigido.
- [ ] **Remover** o invariante `code_root ⊂ workdir` (`config.go:265`). Registrar em DECISOES.
- [ ] Manter `ANCIENTCODE_WORKDIR` apontando para a casa do agente.

### 4.2 Estado mutável em runtime

- [ ] `internal/workspace` — dono de `project_root`, `scratch` e das concessões (`/grant`),
      com `sync.RWMutex` (molde: `Loop.SetToolboxMode` + `toolboxMu`, `loop.go:71`).
- [ ] `RootRef` — indireção lida por `files.Files`, `code.Toolset` e `sandbox.Executor`, porque
      `Registry.Register` recusa duplicata (`tool.go:102-104`) e **não há setter** hoje em nenhum
      dos três. `Registry.Register` não pode ser contornado reinstalando a tool.

### 4.3 Comandos

- [ ] `/cd <pasta>` — troca a raiz do projeto; **confirma só quando amplia** (nova raiz é
      ancestral da atual), mostrando o caminho resolvido.
- [ ] `/cd` sem argumento — mostra a raiz atual e o rascunho.
- [ ] `/grant <pasta>` — libera escrita naquele diretório, com registro no audit; o operador vê
      o que está liberado.
- [ ] `--use-env` não se aplica. `/cd` só na TUI e no REPL (não em `--run`, que é não-interativo).

### 4.4 Onde cada coisa opera

| | diretório | leitura | escrita |
|---|---|---|---|
| shell | raiz do projeto como `proc.Dir` | — | gate (comando visível) |
| file/code tools | raiz do projeto | allow | `ask` com diff (como hoje) |
| rascunho | `<agent_home>/scratch` | livre | **livre** — é para não poluir o repo |

- [ ] O shell começa em `project_root`, e o agente tem caminho explícito para o rascunho —
      `go build > log.txt` não pode sujar o repositório.
- [ ] `audit.jsonl` e `sessions/` **permanecem** em `workdir`. Trocar a raiz **não** pode abrir
      um log de auditoria novo: isso daria trilha limpa exatamente no momento de ampliar o
      alcance.

### 4.5 UI

- [ ] A pasta atual **sempre visível** (pedido explícito do usuário), no REPL e na TUI.
- [ ] `/grant` mostra as concessões ativas.

### 4.6 Persistência

- [ ] `state.json` guarda o último `project_root`; bump de `stateVersion` e validação de path
      análoga a `ValidateModelID` (path **remoto/não-confiável** passa pela gramática estrita).

## 5. Requisitos de segurança deste milestone

- [ ] **`/cd` é do operador.** Nenhuma tool o expõe ao modelo. Um agente que movesse a própria
      raiz sairia do confine sem nenhum registro.
- [ ] **Widening pede consentimento.** Subir a árvore amplia o blast radius; descer não.
- [ ] **A casa do agente não é destino válido.** `/cd` para dentro de `<agent_home>` onde mora
      `auth.json` é recusado — senão o agente edita a própria credencial.
- [ ] **`audit.jsonl` não acompanha a troca** (§4.4).
- [ ] Path com symlink é resolvido (`EvalSymlinks`) **antes** de virar raiz, como já faz
      `files.resolve` (`files.go:121-127`); uma raiz que resolve para fora do pedido é recusada.
- [ ] `/grant` é **auditado** e some no fim da sessão.

### 5.1 O que este milestone NÃO promete

- [ ] **Não** confinamos o `shell` por path. O "path" de um shell é texto livre; `find / -name x`
      e `ls .` são ambos "shell". Analisar comando seria frágil e contornável. O controle real
      do shell continua sendo `run_user` não-privilegiado + `approval.default: ask`, que mostra o
      comando inteiro. **Declarar isso por escrito** é parte do entregável (é a lição de D-70:
      prometer um controle que não existe foi o que fez `/connect` guardar chave errada).

### 5.2 Por que leitura fora do projeto NÃO vira gate de path

O usuário pediu pedir permissão para ler fora do projeto **e** pediu que o agente rode `ps`,
`df` e `free`. Se leitura fora do projeto pedir, `ps` pede — todo turno. O que se aperta é
**escrita** fora de projeto+rascunho, mais `/grant`.

## 6. Testes obrigatórios

### Funcionais

- [ ] `/cd <pasta>` troca a raiz e o shell passa a rodar nela
- [ ] `/cd` sem argumento mostra raiz e rascunho
- [ ] `/cd` para subpasta não pede consentimento
- [ ] `/grant <pasta>` libera escrita e o arquivo é criado
- [ ] a pasta atual aparece na UI
- [ ] `project_root` volta em `state.json` na próxima execução

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [ ] `/cd` não existe como tool: o registry não a expõe ao modelo
- [ ] `/cd` para `<agent_home>` é recusado (protegido `auth.json`)
- [ ] `widening` sem consentimento não troca a raiz
- [ ] `audit.jsonl` não muda de caminho após `/cd`
- [ ] root com symlink que resolve fora é recusado
- [ ] path inexistente ou não-diretório é recusado
- [ ] escrita fora de projeto+rascunho+`grant` é recusada
- [ ] o gate do shell continua **não** fingindo confinamento por path (§5.1)

## 7. Critérios de aceite (DoD)

- [ ] Implementação conforme escopo
- [ ] **Todos os testes do projeto passam** (`go test -count=1 ./...`)
- [ ] `go vet`/`go build` limpos; `gofmt` avaliado por **blob** (ver AGENTS.md — não é gate no
      host Windows por causa do `core.autocrlf`)
- [ ] `go test -race` verde no WSL
- [ ] **Mutação** de cada proteção de segurança; verde = sem cobertura
- [ ] `docs/DECISOES.md` atualizado
- [ ] Nota de sessão em `docs/`
- [ ] Binário `GOAMD64=v1` no Inspiron e smoke real: `/cd` troca a raiz, shell e tools seguem,
      e a auditoria não se move
- [ ] `docs/SPEC.md` §10.3 atualizado (o invariante removido é parte do contrato de segurança)
- [ ] README/MANUAL com as três camadas