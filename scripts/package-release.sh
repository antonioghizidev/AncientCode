#!/bin/sh
# Empacota o artefato de release.
#
# Existe para que o workflow e os testes usem o MESMO empacotador. A primeira
# versao do M16 tinha o workflow gerando o tarball com um diretorio de topo
# (tar -czf ... ancientcode_<tag>_linux_amd64) e o teste gerando um tarball
# plano (tar -czf ... ancientcode). O teste passava; o install.sh, que procura
# <extraido>/ancientcode, recusava toda release real. Dois empacotadores, um
# verde e um quebrado - por isso agora ha so um.
#
# Uso:
#   scripts/package-release.sh <dist-dir> <tag>
#
# Entrada esperada: <dist-dir>/ancientcode_<tag>_linux_amd64/ com o binario e
# os arquivos que vao para o usuario.
# Saida: <dist-dir>/ancientcode_<tag>_linux_amd64.tar.gz e <dist-dir>/SHA256SUMS.
set -eu

if [ "$#" -ne 2 ]; then
	echo "uso: $0 <dist-dir> <tag>" >&2
	exit 2
fi

DIST=$1
TAG=$2

case "$TAG" in
v*) ;;
*)
	echo "erro: a tag precisa comecar com v: $TAG" >&2
	exit 2
	;;
esac

DIR="$DIST/ancientcode_${TAG}_linux_amd64"
ASSET="ancientcode_${TAG}_linux_amd64.tar.gz"

[ -f "$DIR/ancientcode" ] || {
	echo "erro: falta $DIR/ancientcode" >&2
	exit 1
}

# O -C no diretorio e' o ponto: o tarball NAO tem diretorio de topo, entao o
# install.sh acha o binario em <extraido>/ancientcode. Se alguem "consertar"
# isto para incluir o diretorio, os testes de instalacao quebram - de proposito.
tar -czf "$DIST/$ASSET" -C "$DIR" .

# O checksum e' do TARBALL, entao vem depois do tar. A ordem nao e' detalhe:
# invertida, o SHA256SUMS descreve um arquivo que ninguem tem.
(
	cd "$DIST"
	sha256sum "$ASSET" > SHA256SUMS
)
