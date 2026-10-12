#!/bin/sh
# AncientCode - instalador.
#
# Contrato (spec M16):
#   - so linux/amd64; qualquer outra plataforma e recusada, naotentada;
#   - artefato e SHA256SUMS vem da MESMA versao resolvida (S4);
#   - o checksum e conferido ANTES de extrair, e ausencia de checksum e erro (S2, S5);
#   - checksum divergente e recusa, nunca aviso (S1);
#   - sem sudo por default.
#
# Uso:
#   curl -fsSL https://raw.githubusercontent.com/antonioghizidev/ancientcode/<TAG>/install.sh | sh
#
# Variaveis de ambiente (todas opcionais):
#   ANCIENTCODE_VERSION   versao a instalar; padrao "latest"
#   ANCIENTCODE_PREFIX    onde instalar; padrao "$HOME/.local/bin"
#   ANCIENTCODE_RELEASE_BASE  base dos assets (usado por teste)

set -eu

REPO="antonioghizidev/ancientcode"
RELEASES="https://github.com/$REPO/releases"
BASE="${ANCIENTCODE_RELEASE_BASE:-$RELEASES/download}"
RAWBASE="${ANCIENTCODE_RAW_BASE:-https://raw.githubusercontent.com/$REPO}"

# A versao fixada e o DEFAULT de proposito: "latest" e convenience, nao
# reprodutibilidade: se o usuario rodar o instalador amanha sem querer, ele
# recebe outra build. Quem quiser a ultima tem que pedir (ANCIENTCODE_VERSION).
DEFAULT_VERSION="${ANCIENTCODE_DEFAULT_VERSION:-v0.16.0}"
WANT_VERSION="${ANCIENTCODE_VERSION:-$DEFAULT_VERSION}"
PREFIX="${ANCIENTCODE_PREFIX:-$HOME/.local/bin}"

die() {
	echo "erro: $*" >&2
	echo "nada foi instalado." >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "falta '$1' no PATH"
}

need tar
need sha256sum

# --- plataforma (S3) ---------------------------------------------------------
# Recusar aqui e deliberate: um binario amd64 instalado num aarch64 falha tarde
# e com mensagem confusa, e compilar no lugar seria pior - exigiria toolchain
# numa maquina que nao a tem por definicao.
# Os overrides existem para o teste poder exercitar a recusa sem trocar a
# arquitetura da maquina; em uso normal nao ha como defines-los.
os_name=$(uname -s 2>/dev/null || echo "?")
os_arch=$(uname -m 2>/dev/null || echo "?")
os_name="${ANCIENTCODE_FAKE_OS:-$os_name}"
os_arch="${ANCIENTCODE_FAKE_ARCH:-$os_arch}"
[ "$os_name" = "Linux" ] || die "so linux/amd64; esta maquina e $os_name"
case "$os_arch" in
x86_64 | amd64) ;;
*) die "so linux/amd64; esta maquina e $os_arch" ;;
esac

# --- downloader --------------------------------------------------------------
# curl ou wget, escolhidos uma vez. Ausencia dos dois e recusa (fail-closed):
# um instalador que "tenta depois" e um instalador que pode falhar aberto.
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
	fetch_head_url() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
	fetch_head_url() { wget -q --server-response --spider "$1" 2>&1 | sed -n 's/.*Location: *//p' | tr -d '\r' | tail -1; }
else
	die "precisa de curl ou wget"
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/ancientcode.XXXXXX") || die "mktemp falhou"
trap 'rm -rf "$WORK"' EXIT INT TERM

# --- versao (S4) -------------------------------------------------------------
# 'latest' e' resolvido pelo REDIRECT de /releases/latest, que leva a
# /releases/tag/<TAG>. Nao se descobre a tag por um nome de asset: o nome do
# asset carrega a versao, entao procurar '..._latest_...' nunca encontraria
# nada. Era exatamente o bug que havia aqui.
#
# ATENCAO: o GitHub NAO aponta /releases/latest para prerelease. Se a ultima
# release for '-pre', este caminho devolve 404 e o instalador recusa, dizendo
# para pedir a versao explicita. E o comportamento certo: 'latest' que cai
# numa prerelease e' uma surpresa.
VERSION="$WANT_VERSION"
if [ "$VERSION" = "latest" ]; then
	redirected=$(fetch_head_url "$RELEASES/latest") || die "nao consegui consultar a ultima release (o repositorio esta privado, ou a ultima release e' prerelease?)"
	VERSION=$(basename "$redirected")
	case "$VERSION" in
	v*) ;;
	*) die "nao consegui determinar a versao mais recente (URL inesperada: $VERSION)" ;;
	esac
	echo "ultima release: $VERSION"
fi
case "$VERSION" in
v*) ;;
*) die "versao invalida: $VERSION" ;;
esac

ASSET="ancientcode_${VERSION}_linux_amd64.tar.gz"
echo "instalando AncientCode $VERSION"

# --- baixa -------------------------------------------------------------------
# ANCIENTCODE_LOCAL_ASSET existe para o teste apontar um artefato local em vez
# da internet. Em uso normal o caminho normal abaixo e o que roda.
if [ -n "${ANCIENTCODE_LOCAL_ASSET:-}" ]; then
	[ -f "$ANCIENTCODE_LOCAL_ASSET" ] || die "ANCIENTCODE_LOCAL_ASSET nao existe"
	cp "$ANCIENTCODE_LOCAL_ASSET" "$WORK/$ASSET" || die "nao consegui preparar o artefato"
else
	fetch "$BASE/$VERSION/$ASSET" "$WORK/$ASSET" || die "falha ao baixar $ASSET"
fi
fetch "$BASE/$VERSION/SHA256SUMS" "$WORK/SHA256SUMS" || die "falha ao baixar SHA256SUMS (S2: sem checksum nao ha instalacao)"

# --- verificacao (S1, S5) ----------------------------------------------------
# O checksum e lido do arquivo, nao de um valor esperado embutido aqui: o
# instalador nao pode carregar a resposta.
#
# A linha e filtrada pelo nome exato do artefato em vez de rodar
# `sha256sum -c` no arquivo inteiro. Um SHA256SUMS pode citar caminhos como
# `../../etc/passwd`; `sha256sum -c` leria o arquivo errado e o codigo de
# saida ainda poderia passar.
expected=$(awk -v f="$ASSET" '$2 == f || $2 == "*"f {print $1; exit}' "$WORK/SHA256SUMS")
[ -n "$expected" ] || die "SHA256SUMS nao contem uma entrada para $ASSET (S4)"

actual=$(sha256sum "$WORK/$ASSET" | awk '{print $1}')
[ "$actual" = "$expected" ] || die "checksum divergente para $ASSET (S1); o download foi recusado"

# --- extracao (so depois de verificar) ---------------------------------------
mkdir -p "$WORK/x"
tar -xzf "$WORK/$ASSET" -C "$WORK/x" || die "falha ao extrair o tarball"

# Nao confiar no conteudo do tarball para decidir o que instalar: um caminho
# absoluto ou com '..' escaparia do diretorio de trabalho.
BIN="$WORK/x/ancientcode"
[ -f "$BIN" ] || die "o tarball nao contem o binario ancientcode"

mkdir -p "$PREFIX"
TMPBIN="$PREFIX/.ancientcode.$$"
cp "$BIN" "$TMPBIN"
chmod 0755 "$TMPBIN"
mv -f "$TMPBIN" "$PREFIX/ancientcode"

echo "instalado em $PREFIX/ancientcode"
echo
echo "Para usar em qualquer terminal, acrescente ao seu ~/.profile ou ~/.bashrc:"
echo "    PATH=\"\$PATH:$PREFIX\""
echo
echo "Depois: copie ancientcode.example.yaml para ~/.ancientcode.yaml e exporte a chave."
