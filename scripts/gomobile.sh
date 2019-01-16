#!/bin/bash

export GOPATH=$PWD
export GOBIN=$GOPATH/pkg/bin/
export PATH=$GOBIN:$PATH

rm -rf pkg/*
go get golang.org/x/mobile/cmd/gomobile
gomobile init -ndk "$NDK"
