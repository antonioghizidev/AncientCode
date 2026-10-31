#!/bin/sh
# Portao de release: a versao fixada no install.sh tem de ser a tag estavel que
# esta saindo.
#
# O link estavel do README (raw/.../HEAD/install.sh) instala a versao FIXADA, de
# proposito: rodar o instalador amanha tem de dar a mesma build de hoje. O preco
# e' que a fixacao tem de ser atualizada a cada release. A v0.16.1 saiu fixando
# v0.16.0, e a pagina da release mandava instalar a versao anterior (D-95).
#
# Uso: sh scripts/check-release-version.sh <tag>
set -eu

TAG="${1:-}"
if [ -z "$TAG" ]; then
	echo "uso: $0 <tag>" >&2
	exit 2
fi

# Prerelease nao entra no canal estavel: o link fixo continua apontando para a
# ultima estavel, entao nao ha o que conferir.
case "$TAG" in
*-*)
	echo "prerelease ($TAG): a versao fixada continua na ultima estavel"
	exit 0
	;;
esac

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PIN=$(sed -n 's/.*ANCIENTCODE_DEFAULT_VERSION:-\([^}"]*\)}.*/\1/p' "$ROOT/install.sh" | head -1)
if [ -z "$PIN" ]; then
	echo "erro: nao achei ANCIENTCODE_DEFAULT_VERSION em install.sh" >&2
	exit 2
fi
if [ "$PIN" != "$TAG" ]; then
	echo "erro: install.sh fixa $PIN, mas a release e' $TAG." >&2
	echo "      atualize ANCIENTCODE_DEFAULT_VERSION em install.sh e tagueie de novo:" >&2
	echo "      o link estavel do README instala a versao fixada." >&2
	exit 1
fi
echo "install.sh fixa $TAG (ok)"
