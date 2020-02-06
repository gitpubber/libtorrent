#!/bin/bash
#
# pack original *.go sources into jar files before 'maven publish'
#

zip -u libtorrent-sources.jar -r src/ -x \*/.git/\* -y -x src/gitlab.com/axet/libtorrent

zip -d libtorrent-sources.jar libtorrent/\*

zip -u libtorrent-sources.jar -r src/gitlab.com/axet/libtorrent/*.go
