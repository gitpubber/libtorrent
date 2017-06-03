#!/bin/bash

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

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

go_get "gitlab.com/axet/torrent" "github.com/anacrolix/torrent"

go get -u gitlab.com/axet/libtorrent

go get -u golang.org/x/mobile/cmd/gomobile
