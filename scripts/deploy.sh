#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DIR="$DIR/../"

ARGS=(-DpomFile=pom.xml -Dfile="$DIR/libtorrent.aar" \
  -DrepositoryId=sonatype-nexus-staging \
  -Durl=https://oss.sonatype.org/service/local/staging/deploy/maven2/)

mvn install:install-file "${ARGS[@]}"

mvn gpg:sign-and-deploy-file "${ARGS[@]}"
