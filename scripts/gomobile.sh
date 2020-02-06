#!/bin/bash
#
# gomobile build.
#
# supports for older android 2.3.3. requires ndk-16b (16.1.4479499), and properly applied gomobile.patch
#
# sdkmanager --install "ndk;16.1.4479499"
#

set -e

mod() {
  export GOPATH=$PWD/build
  export GOBIN=$GOPATH/bin/
  export PATH=$GOBIN:$PATH
  export ANDROID_HOME=$HOME/Android/Sdk
  export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/16.1.4479499/
  go get -d golang.org/x/mobile/cmd/gomobile
  chmod a+rw -R build
  patch -p1 < scripts/gomobile.diff -d build/pkg/mod/golang.org/x/mobile@*/
  go get golang.org/x/mobile/cmd/gomobile
  gomobile init
  gomobile bind
}

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

export LIB=$DIR/..
export GOPATH=$LIB/../libtorrent-build
export GOBIN=$GOPATH/bin/
export PATH=$GOBIN:$PATH
export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/16.1.4479499/

cp -nv $DIR/*linux-android* $ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-*/bin/

mkdir -p $GOPATH
cd $GOPATH

[ ! -e $GOPATH/src/golang.org/x/mobile/ ] && go get -d golang.org/x/mobile/cmd/gomobile && patch -p1 < $DIR/gomobile.diff -d $GOPATH/src/golang.org/x/mobile/ && go get golang.org/x/mobile/cmd/gomobile

[ ! -e "$GOPATH/pkg/gomobile" ] && gomobile init

[ ! -e ./src/github.com/anacrolix/torrent ] && git clone -b dev https://gitlab.com/axet/torrent src/github.com/anacrolix/torrent

[ ! -e ./src/gitlab.com/axet/libtorrent ] && mkdir -p ./src/gitlab.com/axet/ && ln -sf $LIB src/gitlab.com/axet/libtorrent

go get -tags disable_libutp -d gitlab.com/axet/libtorrent
