#!/bin/bash
#
# gomobile build.
#
# supports for older android 2.3.3. requires ndk-16b (16.1.4479499), and properly applied gomobile.patch
#
# sdkmanager --install "ndk;16.1.4479499"
#

set -e

export GOPATH=$PWD
export GOBIN=$GOPATH/pkg/bin/
export PATH=$GOBIN:$PATH
export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/16.1.4479499/

rm -rf pkg/*

go get golang.org/x/mobile/cmd/gomobile
[ -e "$GOPATH/pkg/gomobile" ] || ANDROID_HOME= gomobile init

[ -e ./src/github.com/anacrolix/torrent ] || git clone https://gitlab.com/axet/torrent src/github.com/anacrolix/torrent

[ -e ./src/gitlab.com/axet/libtorrent ] || ( mkdir -p ./src/gitlab.com/axet/ && ln -sf ../../../ src/gitlab.com/axet/libtorrent )

go get -d gitlab.com/axet/libtorrent
