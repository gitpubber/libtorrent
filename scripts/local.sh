#!/bin/bash

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DIR="$DIR/.."

ln -sf "$GOPATH/pkg" "$DIR"
ln -sf "$GOPATH/src" "$DIR"
ln -sf $DIR/scripts/build.gradle "$DIR"
