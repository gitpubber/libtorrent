#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/.."

GOPATH="$OUT"

go_get() {
  F="$1"
  T="$2"
  
  TT="$OUT/src/$T"
  
  if [ ! -e "$TT" ]; then
    git clone "https://$F" "$TT" || return 1
  fi
  
  return 0
}

go_get "gitlab.com/axet/torrent" "github.com/anacrolix/torrent" || exit 1

go get -u gitlab.com/axet/libtorrent || exit 1

go get -u golang.org/x/mobile/cmd/gomobile || exit 1
