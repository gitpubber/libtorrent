#!/bin/bash
#
# https://groups.google.com/forum/#!topic/go-mobile/ZstjAiIFrWY
#

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

GOPATH="$OUT"

"$DIR/update.sh"

if [ ! -e "$GOPATH/pkg/gomobile" ]; then
  gomobile init
fi

gomobile bind -o "$OUT/libtorrent.aar" "$@" gitlab.com/axet/libtorrent
