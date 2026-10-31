# M16 — Distribuição instalável: release com checksum e instalador verificado

**Status**: 🟡 em andamento (branch `m16-distribuicao`) — spec escrita, implementação em curso.
**Depende de**: M14, M15
**Sessão de referência**: `docs/session-2026-10-10-m16-distribuicao.md`

## 1. Objetivo

Entregar o AncientCode como **artefato instalável por terceiros**, a partir de instruções no
GitHub, sem exigir toolchain Go na máquina de destino.

O binário de hoje só existe para quem tem o repositório e um compilador. O alvo real — servidor
Linux minimal, Core 2 Duo sem AVX, sem Go instalado — não tem nenhum dos dois. A spec fecha essa
distância com três peças: **release versionada**, **instalador que verifica**, e **versão que
diz qual é**.

## 2. O que está errado hoje

| hoje | consequência |
|---|---|
| `const version = "0.15.0-m15"` em `main.go:66` | a versão é digitada à mão e mente: `--version` pode prometer uma build que não é a que está rodando |
| nenhuma tag, nenhuma release | não há "instale a v0.15.0"; há "compile você mesmo" |
| nenhum `.github/` | nada é compilado, testado ou assinado automaticamente |
| nenhum checksum | um binário baixado é indistinguível de um binário adulterado |

O item 1 é o que importa para segurança: **`--version` é a afirmação que o usuário faz ao
reportar um bug**. Se ela for escrita à mão e não injetada pela build, ela não é evidência de
nada.

## 3. Escopo

### 3.1 Versão injetada pela build

`version` deixa de ser `const` e passa a ser `var`, com default explícito para build local:

```go
// Injetado pela build de release: -X main.version=<tag>.
var version = "dev"
```

Regra: **`dev` é o valor de build local**, e nada mais pode produzir `dev`. Uma release sempre
carrega a tag. A spec obriga que o valor injetado seja **verificável contra a tag do artefato**,
porque um `-X` errado é pior que uma constante errada — parece autoritativo.

### 3.2 Release por tag

`.github/workflows/release.yml`, disparado por push de tag `v*`:

1. matriz de build: `linux/amd64` com `GOAMD64=v1` (**o único alvo, por contrato da
   constituição §7**);
2. `CGO_ENABLED=0`, `-trimpath`, `-ldflags="-s -w -X main.version=$TAG"`;
3. tarball `ancientcode_<versão>_linux_amd64.tar.gz` contendo o binário, o README e a licença;
4. `SHA256SUMS` assinado é **não** escopo deste milestone (ver §6);
5. verificação **obrigatória**: a suíte precisa passar antes de o artefato ser produzido.

### 3.3 `install.sh`

Executado por `curl … | sh`. Contrato:

- detecta a plataforma e **recusa** o que não for `linux/amd64`, em vez de tentar;
- baixa o tarball e o `SHA256SUMS` da **mesma versão**;
- **verifica o checksum antes de extrair** — checksum ausente é erro, não aviso;
- instala em `~/.local/bin` (ou `$PREFIX`), sem `sudo` por default;
- falha fechado em qualquer etapa: binário ausente, checksum divergente, sem espaço.

### 3.4 Instruções no README

Duas vias, com o critério de escolha explícito:

```sh
curl -fsSL https://raw.githubusercontent.com/antonioghizidev/ancientcode/<TAG>/install.sh | sh
# ou, se já tem Go 1.27+:
go install github.com/antonioghizidev/ancientcode/cmd/ancientcode@<TAG>
```

`go install` é secundário **e tem uma pegadinha** que a spec precisa nomear: ele compila na
máquina do usuário com o `GOAMD64` **default do Go**, que é `v1` — mas só porque o default
acerta. Um `GOAMD64` exportado no shell do usuário quebra a compatibilidade silenciosamente.
O caminho principal não sofre disso: o binário já vem compilado.

## 4. Casos de segurança obrigatórios

Cada um com teste que **falha se a proteção for removida**.

| # | Caso | Por que importa |
|---|---|---|
| S1 | checksum divergente → **recusa**, não instala | substitui o binário por um que o atacante escolheu |
| S2 | `SHA256SUMS` ausente → **recusa** | sem arquivo, a verificação é decorativa |
| S3 | plataforma não suportada → **recusa**, não tenta compilar nem instala | instalar em `aarch64` um binário `amd64` falha tarde e confuso |
| S4 | o checksum conferido é o do **artefato daquela versão**, não o de outra | Fixar a tag é o que impede o *downgrade* e o *mix-and-match* de artefatos |
| S5 | `install.sh` não executa nada antes de verificar o checksum | verificar depois de extrair já executou código do atacante |
| S6 | o binário publicado não contém instrução pós-SSE2 | a constituição §7 é um contrato; um artefato que quebra o Core 2 Duo é falha de distribuição |
| S7 | o valor de `version` injetado bate com a tag do release | `--version` como evidência |
| S8 | o caminho de **sucesso** instala o binário no prefixo | S1–S7 testam recusas; um instalador quebrado no sucesso continua verde em todos eles — foi assim que um tarball com diretório de topo chegou à primeira release |

## 5. O que este milestone **não** faz

- **Assinatura criptográfica** do `SHA256SUMS` (GPG/minisign/Sigstore). Um checksum servido pelo
  mesmo canal do artefato protege contra corrupção e truncamento, **não** contra um atacante que
  controle o canal. Fica registrado como dívida, com o porquê de ser adiado: exige um modelo de
  chaves que este projeto ainda não tem.
- **Multi-plataforma**: `linux/arm64`, macOS e Windows. Decidido com o usuário; ampliar depois
  é trivial, desde que a matriz do workflow seja a única coisa a mudar.
- **Auto-update.** Um binário que se atualiza sozinho é um binário que decide o que executa.

## 6. Dependência externa: repositório público (resolvido em 2026-10-30)

O repositório é **público**. Verificado sem autenticação: a página do repo responde 200,
`/releases/latest` redireciona para `/tag/v0.16.0` e o `install.sh` em
`raw.githubusercontent.com` responde 200.

O `main` já estava limpo — o par `usuário@IP` de LAN e o `/home/USUARIO/...` saíram com a
reescrita de histórico. O problema eram os `refs/pull/*/head`: 10 dos 12 refs ainda apontavam
para os commits antigos, e num repositório público esses refs são baixáveis por qualquer um.
Como as páginas de PR também mostram o diff antigo, apagar os refs não bastaria.

Decisão (D-92): **recriar o repositório** em vez de esperar o GitHub Support. O antigo foi
renomeado para `AncientCode-archived` e mantido privado; o novo recebeu o mesmo nome e apenas
`main` + a tag `v0.16.0` — nenhum ref de PR e nenhuma `v0.16.0-pre`.

## 7. Critérios de aceite

- [x] `--version` devolve a versão injetada pela build, e `dev` fora de release.
- [x] Workflow compila `GOAMD64=v1` e falha se a suíte não passar.
- [x] `install.sh` recusa checksum divergente (S1), checksum ausente (S2) e plataforma fora
      (S3), **antes** de executar qualquer coisa do artefato (S5).
- [x] `install.sh` fixa a tag: artefato e `SHA256SUMS` vêm da mesma versão (S4).
- [x] Artefato verificado sem instrução pós-SSE2 (S6).
- [x] Instalação bem-sucedida coberta por teste (S8), com o mesmo empacotador do workflow.
- [x] README traz as duas vias de instalação e nomeia a pegadinha do `GOAMD64`.
- [x] Suíte completa verde; `gofmt`/`vet`/`build` limpos.
- [x] DECISOES, nota de sessão e `specs/README.md` atualizados; `docs/SPEC.md` revisado (o
      contrato de build/`GOAMD64=v1` já estava correto, não precisou mudar).
- [x] **Dívidas registradas** (§5), não silenciosamente adiadas.
- [x] Release publicada e verificada a partir das URLs públicas: checksum confere, `GOAMD64=v1`
      no `go version -m`, e `install.sh` (versão fixa e `latest`) e `go install @v0.16.0`
      funcionam.