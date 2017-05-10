#!/bin/bash
#
# https://groups.google.com/forum/#!topic/go-mobile/ZstjAiIFrWY
#

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

GOPATH="$OUT"

"$DIR/update.sh" || exit 1

if [ ! -e "$GOPATH/pkg/gomobile" ]; then
  gomobile init || exit 1
fi

gomobile bind -o "$OUT/libtorrent.aar" gitlab.com/axet/libtorrent || exit 1
