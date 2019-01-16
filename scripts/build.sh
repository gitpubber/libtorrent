#!/bin/bash
#
# https://groups.google.com/forum/#!topic/go-mobile/ZstjAiIFrWY
#

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

GOPATH="$OUT"

gomobile bind -tags disable_libutp -o "$OUT/libtorrent.aar" "$@" gitlab.com/axet/libtorrent

cat << EOF > "$OUT/build.gradle"
configurations.maybeCreate("default")
artifacts.add("default", file('libtorrent.aar'))
EOF
