#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
OUT="$DIR/../"

ARGS=(-DpomFile="$DIR/pom.xml" -Dfile="$OUT/libtorrent.aar" \
  -DrepositoryId=sonatype-nexus-staging \
  -Durl=https://oss.sonatype.org/service/local/staging/deploy/maven2/)

mvn install:install-file "${ARGS[@]}"

mvn gpg:sign-and-deploy-file "${ARGS[@]}"
