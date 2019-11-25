#!/bin/bash
#
# deploy .aar into maven central
#

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

source ${DIR}/install.sh

mvn gpg:sign-and-deploy-file "${ARGS[@]}"
