# Sessão 2026-10-02 — M14: `/connect`, credenciais e modelo por provedor

**Branch**: `m14-connect-credenciais` · **Base**: `main` em `7ba8769` (PR #9, spec + decisões)

## Objetivo

Tirar o operador do `.env` no caminho principal. `/connect` na TUI e no REPL,
`ancientcode auth login|logout|list` na CLI, chave verificada antes de ser guardada,
modelo lembrado por provedor. Preparo para `go install`.

## Entregue

| Commit | O quê |
|---|---|
| `internal/creds` | store `auth.json` `0600`, `tmp`+`fsync`+`rename`, schema estrito, pin por provedor |
| `internal/state` | `state.json` separado, último modelo por provedor, regex estrita de model ID |
| resolução com pin | store fixada > env > store solta; erro nomeia as **duas** fontes (D-66) |
| verificação | `GET /models` real; `401` distinguido e **sem guarda**; http só em loopback; corpo limitado |
| REPL + CLI | `/connect`, `auth login\|logout\|list`, input mascarado, onboarding de primeiro uso (D-67) |
| **TUI** | painel de dois estágios reaproveitando o padrão do picker do M12 |
| modelo por provedor | `/models` persiste; `/provider` restaura; **nunca há fallback automático** (D-68) |
| dois `fix` | isatty real + caminho absoluto; `state.write` de fato atômico |
| docs | SPEC §5.5.1 (o relaxamento e seu limite), D-69 |

## Três invariantes que valem mais que o resto

1. **A chave nunca entra em argv.** `readSecret` no termios, `*` no eco, sem scrollback. No TUI
   a chave nunca sai do fluxo de runes e a projeção **só conhece a máscara** — por construção,
   um refuturo não tem como renderizá-la.
2. **`/connect` fixa o provedor (D-66).** Sem o pin, o harness colaria uma chave e continuaria
   usando a do ambiente em silêncio: a tela diria "salvo" e todo request sairia com a chave
   antiga. Mesma classe do gate invisível (M11) e do painel que não limpava (M12).
3. **`0600` não protege contra o agente.** `shell` roda como `run_user`, sem privilégio, no mesmo
   usuário. O `0600` protege contra **outro usuário do mesmo host** — e é isso que ele compra.
   A defesa real continua sendo o gate humano. Dizer o contrário seria reassurance falsa.

## Smoke no Inspiron: um defeito que nenhum teste unitário podia achar

O smoke não confirmou o M14 — **achou um buraco nele**.

O teste 1 digitou uma chave **deliberadamente errada** e recebeu:

```
provider nvidia: chave guardada e verificada (80 modelos)
```

Ela foi guardada. A verificação **não verificava**. Medição contra os serviços reais:

| Endpoint | sem chave | chave inválida | chave válida |
|---|---|---|---|
| `nvidia/v1/models` | **200** | **200** | 200 |
| `openrouter/api/v1/models` | **200** | **200** | 200 |
| `openrouter/api/v1/key` | 401 | 401 | **200** |
| `nvidia/v1/chat/completions` | 401 | **401** | passa da auth |

As respostas de `/models` são **byte a byte idênticas** (7639 e 774392 bytes). `GET /models` é
catálogo público nos dois provedores: prova vivacidade, nunca identidade.

**Por que a suíte estava verde**: meu mock devolvia 401 — *mais rigoroso que a realidade*. Um mock
mais estrito que o sistema esconde exatamente esta classe de defeito, porque o teste só passa se
o código for pior que o provider.

Corrigido (D-2026-10-02-70): estratégia por provedor (`/key` no OpenRouter, sonda de chat de 1
token na NVIDIA), e uma **terceira resposta** — `ErrInconclusive` — para quando o provider não dá
resposta definitiva. `401/403` = recusada; `429/5xx/rede` = não sabemos. Antes, as duas eram
colapsadas, e dizer "sua chave está errada" quando o provider estava com rate limit manda o
operador caçar o problema errado.

### Três medições que custaram idas ao hardware

1. **O catálogo da NVIDIA começa por modelos aposentados.** De 80, só **1 dos 20 primeiros**
   respondeu 200. O único vivo estava na **posição 11**. Um limite de 3 recusaria uma chave válida
   quase sempre; 12 erraria por um. Hoje são 24.
2. **Uma entrada pendurada consome tudo.** `deepseek-v4.1-flash` gastou 75s enquanto os vizinhos
   respondiam em 0.5s. Devolver no primeiro timeout transformava o problema de um vizinho num
   veredito sobre a chave. Timeout agora **pula** e a sonda continua.
3. **Orçamento por request ≠ orçamento total.** Com um único limite de 90s, a sonda morria na
   terceira tentativa. Agora são 75s por request e 6min no total.

### O que passou no hardware

- Binário roda no **Core 2 Duo** — `GOAMD64=v1` confirmado. Meu aviso de AVX era falso alarme:
  o runtime do Go tem assembly vetorial atrás de feature detection.
- `auth.json` `0600`, diretório `0700`.
- **A chave nunca apareceu na saída** — o driver de pty a procura explicitamente como 2ª barreira.
- `/provider` → `chave: store fixada` (S6).
- **Turno real respondeu com o ambiente limpo** (`NVIDIA_API_KEY` removida): prova de que o store
  alimenta as requests. Sem ela o turno funcionaria igual com ou sem store.
- Chave errada → `403`, recusada, **nada gravado**.
- Chave real → verificada e guardada, `key_len=70`, pin ativo.
- Onboarding **não** disparou havia chave no ambiente (D-67); sem TTY, nada gravado (S9).

## Não validado

- `/connect` no painel da **TUI** (precisa de pty respondendo DSR).
- Memória de modelo (`/models` persistindo, `/provider` restaurando).
- `--use-env` soltando o pin.

## Três lições de processo, todas nesta milestone

1. **A âncora de uma mutação que não casa precisa abortar o script.** Um `python` que falha no
   assert deixa o arquivo intacto, o teste roda sobre o código bom e sai verde.
2. **Um teste que aceita "algum erro" não cobre a propriedade.** Mutação G (remove a checagem de
   transporte) ficou verde porque o pedido a um host inexistente falhava por DNS. Passou a exigir o
   erro **de transporte**.
3. **Verifique a identidade do artefato antes de concluir qualquer coisa sobre ele.** Reconstruí o
   binário no WSL, não recopiei para o Windows, e testei o arquivo velho — mesmo nome, mesma
   versão. Quase escrevi "o fix não funciona". Agora o script de deploy compara **md5** em três
   pontos.

E uma sobre o `-race`: a corrida de dados que ele pegou estava **no meu teste**, não no produto —
o handler da request pendurada continua vivo enquanto o seguinte já começa, e ambos escreviam no
mesmo slice.

## Defeitos encontrados no caminho

### `state.write` não era atômico (o mais sério)

O comentário prometia `tmp + fsync + rename`; o código abria `s.path` com `O_TRUNC` e escrevia
por cima. Um processo morto no meio deixava `state.json` **truncado**, e truncado faz `GetModel`
errar para todo modelo já escolhido — o harness perdia a memória de modelo sem o operador ter
feito nada. É exatamente o que o S2 proíbe. Corrigido para o mesmo `tmp`+`rename` do `creds`.

### `hasStdinTerminal` mentia para `/dev/null`

Usava `ModeCharDevice`, e `/dev/null` **é** char device. Achado pelo gate de `go install`
(binário fora da árvore, stdin em `</dev/null`): o onboarding anunciava "vou pedir a chave" e só
depois falhava com "stdin is not a terminal". Agora é a sondagem `TCGETS`, com `fd` parametrizado
para o teste apontar para `/dev/null` sem trocar o fd 0 do binário de teste.

### `CurrentProvider` e `CurrentAPIKeyEnv` ficavam stale

Liam `cfg.Provider`, que vem do YAML e nunca muda. Depois do primeiro `/provider`, `/connect`
alvo errado e a mensagem mandava exportar a variável errada. Agora o loop guarda o provider ativo.

### Filtro de colagem incompleto no painel

Descartar só o `ESC` deixava a cauda imprimível (`[A`, `[2J`) dentro da chave: o operador veria
menos asteriscos do que colou e a chave falharia na verificação sem explicação. Agora descarta a
sequência CSI inteira.

## Dois erros meus, ambos sobre testes

**Quatro testes meus estavam errados, não o código.** `Esc` no stage 2 volta ao stage 1 por
design; com `agent=nil` o destaque começa em `litellm`; backspace de `"nvidi"`→`"nvid"` ainda
isola `nvidia`; e um teste esqueceu de `m.connect = p`. Escrevi a expectativa do jeito que
eu imaginava, não do jeito que o código se comporta.

**Uma mutação "verde" era mentira.** A mutação do fallback automático ficou verde porque eu
formulei uma mudança que não mudava nada. Só quando formulei a mutação honesta (o pin global
pular a checagem de env) ela ficou vermelha. Quase aceitei uma proteção como coberta.

Depois disso o harness de mutação passou a **abortar quando a âncora não casa** — um `python` que
falha no assert deixa o arquivo intacto, o teste roda sobre o código bom e sai verde.

## Mutações de segurança

Todas vermelhas, baseline verde, em cópia isolada em `/tmp` (em `/mnt/d` uma edição de WSL seguida
de restauração já restoration inconsistente):

- **TUI (7)**: projeção devolve a chave crua · guarda antes de verificar · `Ctrl+C` não fecha o
  busy · `Esc` fecha em vez de voltar · resultado para de nomear a origem · filtro de C0/DEL ·
  sequência CSI não filtrada.
- **Modelo (7)**: fallback automático após recusa · harness escolhe para provider nunca visto ·
  `CurrentProvider` volta ao YAML · env volta ao YAML · `/models` não lembra · restauração lê o
  provider errado · falha ao persistir desfaz o switch.

## Verificação

- Host: `go vet ./...`, `go test -count=1 ./...`, `gofmt` — limpos.
- **WSL**: `-race -p 1` verde em todos os pacotes; build `CGO_ENABLED=0 GOAMD64=v1` ok.
- `go install` do módulo com `GOPATH` limpo, **fora** da árvore: compila, roda, onboarding sem
  TTY sai 1 com instrução e **não grava nada**.
- `auth.json`/`state.json` fora de `git ls-files` (S7a, teste que lê o git de verdade).

## Não validado

- `/models --check` (camada 3 do M12) continua dívida.

_(O smoke foi executado; ver a seção de hardware acima.)_

## Rodada 2 do smoke: mais quatro defeitos, todos achados no hardware

O smoke não terminou na primeira ida. Cada rodada seguinte achou algo novo — o
argumento a favor de smoke real e não só de testes.

### 1. `/connect --use-env` não fazia nada

O argumento chega ao handler como o resto inteiro (`"nvidia --use-env"`), e
`connectTarget` o validava verbatim — recusando **a própria opção do comando**.
Sem mensagem de erro visível: o pin simplesmente continuava ativo. Eu tinha
anotado esse bug no começo da sessão e não corrigi; o hardware encontrou.

### 2. A memória de modelo era só de escrita

`/model X` gravava `state.json` corretamente — e a sessão seguinte começava no
modelo do config. A única forma de voltar a X era trocar de provider e voltar.
Um store que nada lê não é persistência. Instalar a memória agora **restaura** o
modelo do provider ativo.

### 3. O banner dizia o modelo do YAML, não o ativo

Depois de (2) corrigido, o banner ainda anunciava
`model=nvidia/nemotron-nano-3-30b-a3b` numa sessão que rodava
`google/diffusiongemma-26b-a4b-it`. Todo o resto do código pergunta ao loop o que
está em uso; `cfg.Model` era o único lugar que reportava o YAML. A primeira linha
de orientação do operador contradizia a sessão.

### 4. A TUI passava o nome da variável de ambiente como base URL

`LookupProviderDefault` devolve `(baseURL, apiKeyEnv, ok)` e eu li invertido. O
sintoma no painel era `base_url scheme "" must be http(s)`. **Nenhum teste
falhou**: todos os anteriores paravam antes do dispatch.

## O que a rodada 2 validou no hardware

| Verificação | Resultado |
|---|---|
| `/models` persiste em `state.json`, `0600` | ok |
| modelo lembrado volta em sessão nova | ok |
| banner anuncia `modelo restaurado de state.json` | ok |
| provider nunca visto orienta `/models` | ok |
| `--use-env` solta o pin e **mantém** a chave | ok |
| TUI: `/connect` abre, filtra, `Enter` avança | ok |
| TUI: chave mascarada — **326 asteriscos, 0 caracteres** | ok |
| TUI: backspace reduz a máscara | ok |
| TUI: chave errada → `403`, recusada, nada guardado | ok |
| TUI: chave errada **não substitui** a guardada (hash idêntico) | ok |
| TUI: `Esc` fecha sem alterar a credencial | ok |
| TUI: chave real → guardada, verificada, origem nomeada | ok |
| `auth logout` apaga chave **e** pin | ok |

### Um achado colateral

Com o store vazio e TTY presente, o **onboarding** dispara antes da TUI e consome
a entrada. É o D-67 como especificado — mas significa que um driver de smoke
precisa semear o store antes de testar a TUI, senão está testando o onboarding.

## Lição que mudou o processo (D-69)

Os testes POSIX de `state` e `creds` têm `Skip` para Windows. **Eles não rodavam no host** — e eu
reportei "verde" para esses pacotes várias vezes sem que significasse nada para S1/S2. O race no
WSL foi o que reprovou e expôs a escrita não-atômica.

*Um teste pulado é um teste que não existe.* Gate sobre permissão, atomicidade ou filesystem só é
válido onde o teste POSIX roda, e a saída precisa distinguir `ok` de `SKIP`.
