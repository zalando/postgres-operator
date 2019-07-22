#!/bin/sh
set -e -x

team_repo="$GOPATH/src/github.com/zalando/"
project_dir="$team_repo/postgres-operator"

mkdir -p "$team_repo"

ln -s "$PWD" "$project_dir"
cd "$project_dir"

make deps clean docker push
