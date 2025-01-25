#!/bin/bash

export $(grep -v '^#' .env | xargs)

go build -o ./bot

if [ $? -ne 0 ]; then
    echo "Build failed"
    exit 1
fi

./bot