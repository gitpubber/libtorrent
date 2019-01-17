#!/bin/bash
#
# https://groups.google.com/forum/#!topic/go-mobile/ZstjAiIFrWY
#

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

"$DIR/build.sh" -ldflags -s "$@"
