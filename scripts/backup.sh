#!/bin/bash
set -euo pipefail

SRC_DIR="$HOME/projeto-tunel/src"
BKP_DIR="$HOME/projeto-tunel/bkp/$(date +%Y%m%d-%H%M)"

# Garante que a pasta de origem existe antes de copiar
if [ ! -d "$SRC_DIR" ]; then
    echo "❌ Erro: $SRC_DIR não existe."
    exit 1
fi

# Cria a estrutura de destino
mkdir -p "$BKP_DIR"

# Copia preservando permissões, ownership e timestamps
cp -a "$SRC_DIR/." "$BKP_DIR/"

echo "✅ Backup criado em: $BKP_DIR"
