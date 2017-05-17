#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DIR="$DIR/../"

ARGS=(-DpomFile=pom.xml -Dfile=libtorrent.aar \
  -DrepositoryId=sonatype-nexus-staging \
  -Durl=https://oss.sonatype.org/service/local/staging/deploy/maven2/)

mvn gpg:sign-and-deploy-file "${ARGS[@]}"

mvn install:install-file "${ARGS[@]}"
