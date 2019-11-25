#!/bin/bash

set -e

go_get() {
  local F="$1"
  local T="$2"
  local TT="$OUT/src/$T"
  [ -e "$TT" ] || git clone "https://$F" "$TT"
}

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

GOPATH="$OUT"

go_get "gitlab.com/axet/torrent" "github.com/anacrolix/torrent"

go get -u gitlab.com/axet/libtorrent

go get -u golang.org/x/mobile/cmd/gomobile

[ -e "$GOPATH/pkg/gomobile" ] || gomobile init
