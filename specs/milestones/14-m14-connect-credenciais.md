# M14 — `/connect`: credenciais e modelo por provedor, e preparo para `go install`

**Status**: ✅ implementado (branch `m14-connect-credenciais`) — ver `docs/session-2026-10-02-m14-connect.md`
**Depende de**: M13
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Tirar o usuário do `.env` no caminho principal: guardar a credencial e o **último modelo por
provedor** em disco, com `/connect` na TUI e no REPL e `ancientcode auth login|logout|list` na CLI,
de modo que instalar pelo GitHub (`go install github.com/antonioghizidev/ancientcode@latest`) já
funcione sem o usuário editar arquivo nem lembrar o nome de uma variável de ambiente.

## 2. Motivação

Hoje `AncientCode.yaml` referencia **apenas o nome** de uma variável de ambiente
(`api_key_env: OPENROUTER_API_KEY`). Isso obriga o usuário a:

1. descobrir o nome exato da variável do provedor;
2. editar (ou criar) um YAML;
3. manter **dois nomes em sincronia** — a variável no ambiente e a referência no config;
4. exportar a variável antes de cada execução, senão o processo nem sobe.

O caminho fail-closed atual está correto e continua existindo (é o que serve para CI), mas é o
**caminho difícil**. Para a maioria dos usuários, configurar `.env` não é um passo trivial.

Medição do `opencode` (referência de paridade): `/connect` na TUI + `opencode auth
login|logout|list` na CLI, credenciais em `~/.local/share/opencode/auth.json`, **sem cifragem
documentada**, e env var/`.env` do projeto continuando lidos no startup como caminho
não-interativo. Duas coisas dessa referência **não** serão copiadas: o arquivo único que mistura
segredo e preferência (§3.3) e o fato de a documentação não dizer qual é o limite de segurança do
`0600` (§4.7).

## 3. Escopo / entregáveis

### 3.1 `internal/creds` — store de credenciais

- [x] Store JSON em `~/.local/share/ancientcode/auth.json`, por id de provedor.
- [x] `Save`/`Get`/`Delete`/`List`, com escrita atômica (`tmp` + `rename`) e `0600`; diretório `0700`.
- [x] Schema estrito de chaves conhecidas (fail-closed em campo desconhecido, como o resto do projeto).
- [x] Limite de tamanho por entrada e total (256 KiB / 1 MiB), leitura sempre limitada.
- [x] `Get` **nunca** devolve a chave em erro, log ou `String()`.

### 3.2 `internal/state` — preferências (não-secreto)

- [x] `~/.local/share/ancientcode/state.json`, `0600`: **último modelo por provedor**.
- [x] Separado de `auth.json` de propósito: misturar segredo e preferência no mesmo arquivo é o
      caminho curto para um bug de permissão que vaza chave (§4.8).

### 3.3 Resolução de credencial (o invariante central)

Ordem na request, fail-closed, por provedor:

1. **Store, se o provedor estiver fixado por `/connect`** (§3.4) → vence.
2. **`api_key_env` presente no ambiente** → vence.
3. Store não-fixado e env ausente → erro, **nomeando as duas fontes tentadas**.
4. `APIKeyEnv` continua presente no config e continua sendo o caminho de CI: **sem regressão**.

> Regra de spec: *`/connect` é ato explícito do operador e substitui a resolução implícita do
> ambiente **para aquele provedor**. O ambiente só continua vencendo onde ninguémconnecting — é o
> caminho para CI e uso não-interativo, onde `/connect` nunca roda.*

Sem isso, `/connect` colaria uma chave e o harness **continuaria usando a do ambiente em silêncio**:
a tela diria "salvo" e todo request sairia com a chave antiga. É a mesma classe de defeito do gate
invisível (M11) e do painel que não limpava (M12).

### 3.4 `Fix` (pin) por provedor

- [x] `/connect` grava `{key, fixed: true, added_at}`.
- [x] `/connect` de um provedor já fixado **sobrescreve** a chave (é o que o usuário quer ao
      reconectar), não falha por já existir.
- [x] Soltar o pin sem apagar a chave: `/connect <p> --use-env` grava `fixed: false` e mantém o valor.
- [x] `auth logout` apaga a chave **e** o pin do provedor.
- [x] `/provider` e a CLI mostram a **origem** da chave em uso (`store fixada` / `store solta` /
      `env`); nunca "salvo" genérico (§4.6).

### 3.5 Interface `/connect`

- [x] **TUI**: reaproveita o picker do M12 (digita filtra, `↑↓`, `Enter`, `Esc`, `Ctrl+F`).
- [x] **REPL**: `/connect [provedor]`, prompt mascarado sobre o termios existente.
- [x] **CLI**: `ancientcode auth login|logout|list`.
- [x] **Verificar antes de gravar**: um `GET /v1/models` real com a chave; `401` → **não grava**.
      Transforma "digitei a chave errada" de falha misteriosa no primeiro prompt em erro localizado,
      e já aquece o catálogo do `/models`.
- [x] **Nada de `--connect <chave>`**: argv aparece em `/proc/<pid>/cmdline` (qualquer usuário lê) e
      no histórico do shell. `auth login` pergunta no terminal — é o que o opencode faz.

### 3.6 Onboarding de primeiro uso

- [x] Se **nenhum provedor tem credencial resolvível** (nem store, nem env, nem config apontando
      para variável existente) e a entrada é TTY → o harness abre o `/connect` como processo default.
- [x] Se já há credencial resolvível, **não** há onboarding (sem provider real o M12 já provou que
      `/models` funciona).
- [x] Se o operador **rejeita**: instrução de env var na stderr + **exit 1**, sem escrever nada em
      disco (o store continua vazio → o onboarding volta na próxima execução).
- [x] **Sem TTY ou com `--run`**: onboarding nunca abre diálogo; imprime a instrução e sai 1. Não
      pode bloquear esperando input num pipe.
- [x] O onboarding roda **antes** de `Validate` falhar, no startup do `main`, antes do loop.

### 3.7 Modelo por provedor

- [x] `/models` persiste o modelo escolhido em `state.json`, por provedor.
- [x] Trocar para provedor **nunca visto** → sem modelo salvo; orienta `/models`.
- [x] Trocar para provedor **já usado** → tenta o último modelo salvo.
- [x] Se o provider **recusar** o modelo salvo (404 de catálogo, erro de modelo), **não escolhe
      outro**: avisa e orienta `/models`.

> Regra de spec: *nunca há fallback automático de modelo.* Auto-selecionar seria o harness decidindo
> sozinho com qual modelo falar — decisão que é do operador. Mesmo princípio do veto do `Approver`.

### 3.8 Preparo para `go install`

- [x] `go install github.com/antonioghizidev/ancientcode@latest` compila e roda a partir do
      módulo (verificado com `GOPATH` limpo, **fora** da árvore do repositório).
- [x] Nenhum caminho de runtime assume estar dentro da árvore do repo.
- [x] Primeiro uso sem config: as mensagens apontam para caminhos absolutos do home.

## 4. Requisitos de segurança deste milestone

Derivados de `docs/SPEC.md` §5.5 e §5.6. Este milestone **relaxa um invariante** e o relaxamento é
delimitado, não difuso:

| Antes (invariante mantido) | Agora |
|---|---|
| Config **nunca** contém valor de chave — só `api_key_env` | **Mantido.** `ancientcode.yaml` continua sem valor de chave. |
| Segredos só de env var / secret manager | **Relaxado e nomeado:** o valor pode existir em **um** arquivo `0600`, escrito **só** por comando explícito do operador. |

- [x] **S1. `0600` no arquivo, `0700` no diretório**, verificado na escrita; teste falha se a
      permissão for afrouxada.
- [x] **S2. Escrita atômica** (`tmp` + `rename`), para que uma falha no meio não deixe credencial
      truncada nem arquivo legível por outro usuário.
- [x] **S3. Segredo nunca sai do store por erro, log, audit ou system prompt.** `Get` com chave
      ausente devolve **só** o nome do provedor.
- [x] **S4. Input mascarado de verdade**: eco `*`, sem scrollback, termios com `ECHO` off. Eco
      visível em TUI é chave no histórico do terminal.
- [x] **S5. Segredo nunca em argv** — nem no store nem em flag (§3.5).
- [x] **S6. `/connect` reporta a fonte real**; nunca "salvo" genérico quando a chave em uso é outra.
- [x] **S7. `auth.json` e `state.json` nunca entram no repositório**; ambos no `.gitignore`, ambos
      fora de `code_root` (o modelo não os lê como arquivo do projeto).
- [x] **S8. Schema estrito**: campo desconhecido no store → erro fail-closed, não "ignora e segue".
- [x] **S9. Rejeitar o onboarding não escreve nada** em disco.
- [x] **S10. Sem TTY, o onboarding não abre diálogo** e não trava.

## 5. Testes obrigatórios

### Funcionais

- [x] `/connect` grava chave e fixa o provedor; `/provider` mostra a origem correta.
- [x] `SwitchProvider` para provedor nunca usado orienta `/models`.
- [x] `SwitchProvider` para provedor já usado tenta o último modelo salvo.
- [x] `/models` persiste o modelo por provedor (dois provedores, dois modelos, ambos sobrevivem).
- [x] Onboarding: sem credencial + TTY → abre; com credencial no env → **não** abre.
- [x] Rejeitar o onboarding → exit 1, instrução de env var na stderr, store vazio.
- [x] `auth login/list/logout` no CLI; `logout` apaga chave **e** pin.
- [x] `/connect --use-env` solta o pin e mantém o valor.
- [x] `go install` do módulo compila e roda fora da árvore.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [x] **S1a.** Arquivo criado com `0600`; afrouxar a permissão → **teste falha**.
- [x] **S2a.** `Save` com erro no meio não deixa arquivo legível por outro usuário.
- [x] **S3a.** Nenhum erro de `Get`/`Load` contém o valor da chave (injetar chave e varrer a
      mensagem).
- [x] **S4a.** Input mascarado não escreve a chave no eco (pty de teste).
- [x] **S5a.** Nenhum caminho aceita chave por argv; `--connect <chave>` **não existe**.
- [x] **S6a.** `/connect` com chave no env **e** pin existente reporta a origem `store`, nunca
      "salvo" genérico.
- [x] **S7a.** `auth.json`/`state.json` ausentes do `git ls-files` e do `code_root`.
- [x] **S8a.** Campo desconhecido no store → erro, não silêncio.
- [x] **S9a.** Rejeitar o onboarding não cria nenhum arquivo.
- [x] **S10a.** Sem TTY, onboarding não lê stdin (não trava) e sai 1.
- [x] **Regressão de precedência:** com `NVIDIA_API_KEY` no ambiente e sem `/connect`, o harness
      continua usando a env var (**zero regressão** para CI e para quem já usa env).
- [x] **Regressão do config:** `ancientcode.yaml` com `api_key_env` continua funcionando sem store.

## 6. Fora de escopo (decidido, não esquecido)

- **Keyring do sistema** (`secret-tool`/libsecret): permitido pela regra (sem CGO; é executar um
  binário, como `docker`/`apt`), mas num server minimal não existe, e um caminho secundário nunca
  testado no alvo é pior que nenhum. Reavaliar se algum dia o alvo mudar.
- **Senha mestra cifrando o store**: troca `.env` por "lembre uma senha" — pior.
- **OAuth / device code**: nenhum dos três provedores atuais exige.
- **`/sessions`**: é do backlog `/new`+`/compact`+`/autocompact` (spec do M13 §10), não deste
  milestone. Mas fica registrado o risco: sem ele, trocar de sessão perde o acesso à anterior.

## 7. Riscos e o que este milestone **não** resolve

- **`0600` não é um limite de segurança contra o agente.** `shell` roda como `run_user`, que é um
  usuário sem privilégio — quem conseguisse ler a chave já é o próprio operador. A defesa real é o
  **gate humano**. Fica escrito em `docs/SPEC.md` §5.5 para ninguém achar que o `0600` protege mais
  do que protege.
- **`/models --check`** (camada 3 do M12) continua por implementar; aqui só usamos a verificação de
  credencial do `GET /v1/models`, que é mais barata e não gasta tokens.
- **`go install` é preparatório, não fecha o primeiro uso**: o usuário ainda precisa configurar
  provedor e chave no primeiro uso — o que o `/connect` entrega. O que este milestone garante é que
  instalar pelo GitHub **funciona** e que a partir daí o caminho é o `/connect`.

## 8. Critérios de aceite (DoD)

- [x] Implementação conforme escopo
- [x] **Todos os testes do projeto passam** (`go test -count=1 ./...`)
- [x] `gofmt`/`go vet`/`go build` limpos
- [x] `go test -race -count=1 ./...` verde no WSL (sequencial por pacote)
- [x] **Todos os testes de segurança de §5 presentes e passando** (e que falhariam se a proteção
      fosse removida)
- [x] `docs/DECISOES.md` atualizado (D-65 store, D-66 precedência/pin, D-67 onboarding,
      D-68 modelo-por-provedor)
- [x] `docs/SPEC.md` §5.5 atualizado com o relaxamento nomeado e o limite do `0600`
- [x] Nota de sessão em `docs/`
- [x] Artefato `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1` compilado
- [x] `go install` verificado fora da árvore do repositório
- [x] Smoke no Inspiron: `/connect` real com NIM, troca de provedor, modelo persistido
- [x] Status deste arquivo e do `docs/SPEC.md` atualizados