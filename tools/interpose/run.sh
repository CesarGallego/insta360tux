#!/bin/sh
# Lanza la copia instrumentada con el interceptor. Log: work/logs/xu.log (INSTA360_XU_LOG).
cd "$(dirname "$0")/../.."
export INSTA360_XU_LOG="${INSTA360_XU_LOG:-$PWD/work/logs/xu.log}"
export DYLD_INSERT_LIBRARIES="$PWD/work/xulog.dylib"
exec work/LinkController.app/Contents/MacOS/Webcam-desktop "$@"
