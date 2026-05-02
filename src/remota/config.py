# tunnel-client/config.py
# ==========================================
# Configurações do Cliente Túnel (VM Remota)
# ==========================================

import os

# 🔌 Conexão ao servidor DMZ
HOST = "172.29.0.203"  # IP da VM DMZ
PORT = 8080            # Porta do protocolo binário

# 📦 Protocolo Binário
HEADER_FMT = ">HBH"  # Big-endian: uint16, uint8, uint16

# Tipos de mensagem (devem coincidir com o servidor Go)
MSG_DATA         = 0
MSG_HEARTBEAT    = 1
MSG_CHANNEL_OPEN = 2
MSG_CHANNEL_CLOSE = 3

# 🔐 Proxy SSH
PROXY_CHANNEL_ID = 99              # Canal dedicado para SSH forwarding
SSH_TARGET = "127.0.0.1:22"        # Destino local do proxy

# 🌐 Web Proxy via Túnel
WEB_PROXY_CHANNEL = 100              # Canal dedicado para proxy HTTP
WEB_SERVER_PORT = 80                 # Porta do Apache/nginx na Remota

# ⏱️ Comportamento
HEARTBEAT_INTERVAL = 10            # Segundos entre heartbeats
RECONNECT_BACKOFF_INITIAL = 1.0    # Segundos para primeira reconexão
RECONNECT_BACKOFF_MAX = 30.0       # Limite máximo de backoff

# 🐛 Debug/Logging
DEBUG = False                       # False = modo produção (menos logs, sem tráfego de teste)
LOG_LEVEL = "INFO"                 # "DEBUG", "INFO", "WARNING", "ERROR"
LOG_FORMAT = "%(asctime)s [%(levelname)s] %(message)s"

# 📁 Logging para ficheiro (rotação automática)
LOG_FILE = os.path.expanduser("~/projeto-tunel/remota.log")  # Caminho do ficheiro de log
LOG_FILE_MAX_BYTES = 10 * 1024 * 1024  # 10 MB por ficheiro
LOG_FILE_BACKUP_COUNT = 5            # Manter 5 ficheiros de backup (remota.log.1, .2, etc.)
LOG_FILE_FORMAT = "%(asctime)s [%(levelname)s] [%(funcName)s:%(lineno)d] %(message)s"  # Mais detalhado para ficheiro

# 🔐 Autenticação
AUTH_TOKEN = "8f4e3a2b1c7d9e5f6a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f"  # ⚠️ MESMO TOKEN DO LADO DMZ
MSG_AUTH = 4                             # Novo tipo de mensagem
