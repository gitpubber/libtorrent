#!/bin/bash

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

ln -sf "$GOPATH/pkg" "$OUT"
ln -sf "$GOPATH/src" "$OUT"
