# 🔐 Túnel Seguro DMZ ↔ Remota

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)
[![Python](https://img.shields.io/badge/Python-3.10+-3776AB?logo=python)](https://www.python.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Sistema de tunneling binário de alta resiliência para acesso remoto seguro a máquinas em redes isoladas, com proxies **SSH** e **HTTP** multiplexados sobre uma única ligação TCP. Desenvolvido para ambientes académicos/protótipos com foco em estabilidade, observabilidade e segurança perimetral.

---

## 🏗️ Arquitetura

[SUPORTE/Cliente] ←SSH/HTTP→ [DMZ:8080/2222/8888] ←Túnel Binário→ [REMOTA:22/80]
↑
Auth por Token + Keepalive + Heartbeat Adaptativo


- **DMZ (Go):** Servidor binário que expõe portos de acesso, autentica ligações, multiplexa tráfego e gere o ciclo de vida do túnel.
- **Remota (Python):** Cliente assíncrono que estabelece a ligação de saída, cria pontes locais para `sshd` e `apache/nginx`, e gere reconexão inteligente.

---

## ✨ Funcionalidades

| Módulo | Estado | Notas |
|--------|--------|-------|
| 🔑 Proxy SSH (Ch:99) | ✅ | Buffer anti-race, logs de início/fim de sessão |
| 🌐 Proxy HTTP (Ch:100) | ✅ | Parsing de `Content-Length`, fallback 502/504 |
| 🔐 Autenticação | ✅ | Handshake inicial por Pre-Shared Token |
| 📡 TCP Keepalive | ✅ | 5s/3 probes → deteta quedas de NAT/firewall em ~20s |
| 💓 Heartbeat | ✅ | Adaptativo (2s–15s) conforme estabilidade da rede |
| 🛑 Graceful Shutdown | ✅ | `<1s`, zero sockets `TIME_WAIT` ou processos zumbi |
| 🔄 Reconexão | ✅ | Timeout 10s + backoff exponencial + jitter 30% |
| 📊 Logging | ✅ | Estruturado, com rotação automática (Go + Python) |

---

## 📁 Estrutura do Repositório
```
├── src/
│ ├── dmz/ # Servidor Go (DMZ)
│ │ ├── main.go
│ │ ├── config.go
│ │ ├── webproxy.go
│ │ └── go.mod
│ └── remota/ # Cliente Python (Remota)
│   ├── main.py
│   └── config.py
├── scripts/ # Backup & validação
├── .gitignore
└── README.md
```

---

## 🚀 Deploy Rápido

#### 1️⃣ DMZ (Go)
```bash
cd src/dmz
go build -o tunnel-server
export TUNNEL_AUTH_TOKEN="tunnel-2026-secure-token"  # ⚠️ Altera para um token forte
sudo ./tunnel-server  # ou via systemd
```
#### 2️⃣ Remota (Python)
```
cd src/remota
export TUNNEL_AUTH_TOKEN="tunnel-2026-secure-token"  # ⚠️ Mesma string da DMZ
python3 main.py
```
✅ Nota: Ambas as partes usam apenas a stdlib. Sem dependências externas.

## 🔧 Configuração

| Ficheiro             | Parâmetros Chave                        | Descrição                                         |
| ---------------------| ----------------------------------------|---------------------------------------------------|
| src/dmz/config.go    | ListenAddress, ServerPort, AuthToken    |IP/porto de escuta, token de auth, portos de proxy |
| src/remota/config.py | HOST, PORT, AUTH_TOKEN, WEB_SERVER_PORT |IP da DMZ, porto do túnel, destino do proxy HTTP   |

## 🧪 Validação Imediata
#### Testar SSH através do túnel
```
ssh utilizador@<DMZ_IP> -p 2222
```
#### Testar HTTP através do túnel
```
curl -H "Connection: close" -s http://<DMZ_IP>:8888
```
#### Verificar logs
```
journalctl -u tunnel-dmz.service -f  # DMZ
tail -f ~/projeto-tunel/remota.log   # Remota
```
## 🛡️ Segurança & Limitações
**🔐 Autenticação:** Handshake por token partilhado (MSG_AUTH). Previne ligações não autorizadas e relay abuse.
**🌐 Confidencialidade:** O tráfego SSH viaja cifrado nativamente. O HTTP e controlo viajam em texto puro no túnel. Para produção, recomenda-se TLS 1.3 ou migração para mTLS/WireGuard.
**🚧 Escopo Académico:** O projeto prioriza resiliência de transporte e gestão de ciclo de vida. A arquitetura está preparada para evolução para Session Broker + Gateway Local (APP_S) conforme descrito no relatório técnico.

## 📈 Evolução Futura
**Session Broker Dinâmico:** Alocação ephemeral de portos/canais por sessão.
**Zero-Trust Access:** DMZ deixa de expor portos publicamente; acesso mediado por validação JWT/mTLS.
**Client-Side Gateway (APP_S):** Posto de SUPORTE instala um proxy local que estabelece túnel seguro para a DMZ e abre portos efémeros locais.
**Encriptação de Transporte:** Integração de TLS 1.3 ou ChaCha20-Poly1305 a nível de aplicação.
## 📄 Licença
Distribuído sob a licença MIT. Ver LICENSE para detalhes.
💡 Desenvolvido para fins académicos e prototipagem. Testa em ambiente controlado antes de deploy em produção.
