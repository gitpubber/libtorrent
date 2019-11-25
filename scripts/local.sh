#!/bin/bash
#
# set build to use shared ~/.go/pkg and ~/.go/src folders
#

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

ln -sf "$GOPATH/pkg" "$OUT"
ln -sf "$GOPATH/src" "$OUT"
