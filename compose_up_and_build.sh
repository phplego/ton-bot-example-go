#!/bin/bash

set -x
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bot

if [ $? -ne 0 ]; then
    echo "Build failed"
    exit 1
fi

docker compose up -d --build
docker compose restart