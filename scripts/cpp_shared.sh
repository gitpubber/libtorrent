#!/bin/bash

set -e

mkdir -p jni/arm64-v8a jni/armeabi-v7a jni/x86 jni/x86_64

cp $NDK/sources/cxx-stl/llvm-libc++/libs/armeabi-v7a/libc++_shared.so jni/armeabi-v7a
cp $NDK/sources/cxx-stl/llvm-libc++/libs/x86/libc++_shared.so jni/x86
cp $NDK/sources/cxx-stl/llvm-libc++/libs/arm64-v8a/libc++_shared.so jni/arm64-v8a
cp $NDK/sources/cxx-stl/llvm-libc++/libs/x86_64/libc++_shared.so jni/x86_64

zip -u libtorrent.aar jni/*/*

rm -rf jni