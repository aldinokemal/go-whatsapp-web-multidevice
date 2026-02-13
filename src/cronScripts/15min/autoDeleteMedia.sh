#!/bin/sh
TARGET_DIR="/app/storages"

if [ ! -d "$TARGET_DIR" ]; then
  echo "Diretório não encontrado: $TARGET_DIR"
  exit 1
fi

find "$TARGET_DIR" -type f -name "*.jpeg" -mmin +30 -exec rm -f {} \;

echo "Arquivos .jpeg removidos de: $TARGET_DIR"
