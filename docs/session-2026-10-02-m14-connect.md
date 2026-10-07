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

- **Smoke no Inspiron**: sem acesso SSH confirmado nesta sessão. `/connect` real com NIM,
  troca de provedor com restauração de modelo e `/models` entre dois provedores seguem pendentes
  de hardware.
- `/models --check` (camada 3 do M12) continua dívida.

## Lição que mudou o processo (D-69)

Os testes POSIX de `state` e `creds` têm `Skip` para Windows. **Eles não rodavam no host** — e eu
reportei "verde" para esses pacotes várias vezes sem que significasse nada para S1/S2. O race no
WSL foi o que reprovou e expôs a escrita não-atômica.

*Um teste pulado é um teste que não existe.* Gate sobre permissão, atomicidade ou filesystem só é
válido onde o teste POSIX roda, e a saída precisa distinguir `ok` de `SKIP`.
