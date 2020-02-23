#!/bin/bash
#
# pack original *.go sources into jar files before 'maven publish'
#

mod() {
  zip -u libtorrent-sources.jar -r build/pkg/mod -y -x \*/.git/\* -x \*/pkg/mod/cache/\*

  zip -d libtorrent-sources.jar libtorrent/\*
}

work() {
  zip -u libtorrent-sources.jar -r src/ -y -x \*/.git/\* -x src/gitlab.com/axet/libtorrent

  zip -d libtorrent-sources.jar libtorrent/\*

  zip -u libtorrent-sources.jar -r src/gitlab.com/axet/libtorrent/*.go
}

case $1 in
  mod) mod ;;
  work) work ;;
esac
