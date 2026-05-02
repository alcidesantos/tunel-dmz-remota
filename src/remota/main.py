import asyncio
import struct
import logging
import logging.handlers
import os
import sys
import socket
import time
import random
import signal

# 🔧 Importa todas as configurações
from config import (
    HOST, PORT,
    HEADER_FMT,
    MSG_DATA, MSG_HEARTBEAT, MSG_CHANNEL_OPEN, MSG_CHANNEL_CLOSE,
    PROXY_CHANNEL_ID, SSH_TARGET,
    HEARTBEAT_INTERVAL,
    RECONNECT_BACKOFF_INITIAL, RECONNECT_BACKOFF_MAX,
    DEBUG, LOG_LEVEL, LOG_FORMAT,
    LOG_FILE, LOG_FILE_MAX_BYTES, LOG_FILE_BACKUP_COUNT, LOG_FILE_FORMAT,
    WEB_PROXY_CHANNEL, WEB_SERVER_PORT,
    AUTH_TOKEN, MSG_AUTH
)

def setup_logging():
    """Configura logging duplo: console + ficheiro com rotação."""
    logger = logging.getLogger()
    logger.setLevel(getattr(logging, LOG_LEVEL))
    logger.handlers = []
    
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(getattr(logging, LOG_LEVEL))
    console_handler.setFormatter(logging.Formatter(LOG_FORMAT))
    logger.addHandler(console_handler)
    
    os.makedirs(os.path.dirname(LOG_FILE), exist_ok=True)
    file_handler = logging.handlers.RotatingFileHandler(
        LOG_FILE, maxBytes=LOG_FILE_MAX_BYTES,
        backupCount=LOG_FILE_BACKUP_COUNT, encoding='utf-8'
    )
    file_handler.setLevel(getattr(logging, LOG_LEVEL))
    file_handler.setFormatter(logging.Formatter(LOG_FILE_FORMAT))
    logger.addHandler(file_handler)
    return logger

logger = setup_logging()
HEADER_SIZE = struct.calcsize(HEADER_FMT)
last_frame_time = time.time()  # 🔹 Para heartbeat adaptativo

async def handle_web_proxy(tunnel_writer, ch_id, request_data):
    """Encaminha request HTTP para Apache local e envia resposta pelo túnel."""
    try:
        # 🔍 LOG DE ACESSO HTTP (ENTRADA)
        # Decodifica a primeira linha (ex: GET / HTTP/1.1)
        req_line = request_data.decode('utf-8', errors='ignore').split('\r\n', 1)[0]
        logger.info(f"🌐 [REMOTA-ACCESS] HTTP recebido: {req_line} -> Apache:{WEB_SERVER_PORT}")

        reader, writer = await asyncio.open_connection("127.0.0.1", WEB_SERVER_PORT)
        writer.write(request_data)
        await writer.drain()
        
        response = b""  # ✅ CORREÇÃO: sem espaços em b""
        while True:
            chunk = await reader.read(4096)
            if not chunk: break
            response += chunk
            if b"\r\n\r\n" in response:
                try:
                    headers_end = response.find(b"\r\n\r\n")
                    headers = response[:headers_end].decode('utf-8', errors='ignore').lower()
                    if "content-length:" in headers:
                        length_str = headers.split("content-length:")[1].split("\r\n")[0].strip()
                        content_length = int(length_str)
                        if len(response) >= headers_end + 4 + content_length: break
                    else:
                        await asyncio.sleep(0.05)
                        break
                except Exception:
                    break
        
        header = struct.pack(HEADER_FMT, ch_id, MSG_DATA, len(response))
        tunnel_writer.write(header + response)
        await tunnel_writer.drain()
        
        # 🔍 LOG DE ACESSO HTTP (SAÍDA)
        logger.info(f"📤 [REMOTA-ACCESS] HTTP Resposta {len(response)}B enviada Ch:{ch_id}")
        
        writer.close()
        await writer.wait_closed()
        
    except ConnectionRefusedError:
        logger.error(f"❌ [Proxy] Apache não responde em 127.0.0.1:{WEB_SERVER_PORT}")
        err_resp = f"HTTP/1.1 502 Bad Gateway\r\n\r\nApache offline".encode()
        tunnel_writer.write(struct.pack(HEADER_FMT, ch_id, MSG_DATA, len(err_resp)) + err_resp)
        await tunnel_writer.drain()
    except Exception as e:
        logger.error(f"❌ [Proxy] Erro ao processar request Ch{ch_id}: {e}")
        err_resp = f"HTTP/1.1 500 Internal Error\r\n\r\nProxy Error: {e}".encode()
        tunnel_writer.write(struct.pack(HEADER_FMT, ch_id, MSG_DATA, len(err_resp)) + err_resp)
        await tunnel_writer.drain()

async def read_loop(reader, writer):
    """Lê mensagens do servidor + suporta proxy SSH e proxy HTTP."""
    active_proxies = {}
    pending_ssh_data = []  # 🔹 Buffer para dados Ch:99 antes da ponte estar pronta
    
    try:
        while True:
            header = await reader.readexactly(HEADER_SIZE)
            ch_id, msg_type, length = struct.unpack(HEADER_FMT, header)
            payload = await reader.readexactly(length)

            # 🔹 Caso especial: Proxy HTTP via túnel
            if ch_id == WEB_PROXY_CHANNEL and msg_type == MSG_DATA:
                asyncio.create_task(handle_web_proxy(writer, ch_id, payload))
                continue

            # 🔹 Caso especial: Proxy SSH (Ch:99)
            if ch_id == PROXY_CHANNEL_ID and msg_type == MSG_DATA:
                if PROXY_CHANNEL_ID in active_proxies:
                    # Ponte já pronta: envia dados + flush de buffer pendente
                    _, local_w = active_proxies[ch_id]
                    for buf in pending_ssh_data:
                        local_w.write(buf)
                        await local_w.drain()
                    pending_ssh_data.clear()
                    
                    local_w.write(payload)
                    await local_w.drain()
                else:
                    # Ponte ainda a inicializar: guarda em buffer
                    pending_ssh_data.append(payload)
                    if DEBUG:
                        logger.debug("⏳ [SSH] Dados recebidos antes da ponte. Em buffer...")
                continue

            # 🔹 Pedido de abertura de proxy SSH
            if msg_type == MSG_CHANNEL_OPEN and ch_id == PROXY_CHANNEL_ID and payload.decode() == SSH_TARGET:
                logger.info(f"🔗 Proxy SSH solicitado para {SSH_TARGET}")
                try:
                    local_r, local_w = await asyncio.open_connection("127.0.0.1", 22)
                    active_proxies[ch_id] = (local_r, local_w)

                    logger.info(f"🔐 [REMOTA-ACCESS] SSH solicitado (Ch{ch_id}) -> {SSH_TARGET}")

                    asyncio.create_task(bridge(local_r, local_w, writer, ch_id))
                except ConnectionRefusedError:
                    logger.error("❌ SSH daemon não está a correr na porta 22")
                except Exception as e:
                    logger.error(f"❌ Falha ao criar ponte SSH: {e}")
                continue

            # 🔹 Tratamento padrão
            if ch_id in active_proxies:
                _, local_w = active_proxies[ch_id]
                local_w.write(payload)
                await local_w.drain()
            elif msg_type == MSG_HEARTBEAT:
                if DEBUG: logger.debug("💓 Heartbeat recebido")
            elif msg_type == MSG_DATA:
                if DEBUG or ch_id not in [1, 2]:
                    logger.info(f"📩 Ch:{ch_id} | {payload.decode(errors='replace')}")
            elif msg_type == MSG_CHANNEL_CLOSE:
                if ch_id in active_proxies:
                    active_proxies[ch_id][1].close()
                    del active_proxies[ch_id]
                logger.info(f"🔒 Canal {ch_id} fechado")

    except asyncio.IncompleteReadError:
        logger.warning("🔌 Ligação fechada pelo servidor")
    except ConnectionResetError:
        logger.warning("🔌 Ligação reiniciada pelo servidor (reset)")
    except Exception as e:
        logger.error(f"❌ Erro na leitura: {e}")

async def adaptive_heartbeat_loop(writer):
    """Heartbeat com intervalo adaptativo baseado em estabilidade de receção."""
    MIN_INTERVAL, MAX_INTERVAL, cur = 2.0, 15.0, HEARTBEAT_INTERVAL
    try:
        while not writer.is_closing():
            await asyncio.sleep(cur)
            if writer.is_closing(): break
            
            ts = int(time.time()) & 0xFFFFFFFF
            payload = struct.pack(">I", ts)
            header = struct.pack(HEADER_FMT, 0, MSG_HEARTBEAT, len(payload))
            writer.write(header + payload)
            await writer.drain()
            
            elapsed = time.time() - last_frame_time
            if elapsed > cur * 1.5:
                cur = max(MIN_INTERVAL, cur * 0.8)  # Rede instável → aumenta frequência
            else:
                cur = min(MAX_INTERVAL, cur * 1.1)  # Rede estável → poupa banda
                
            if DEBUG:
                logger.debug(f"💓 Heartbeat | Interval: {cur:.1f}s | Last frame: {elapsed:.1f}s ago")
    except Exception as e:
        logger.debug(f"⚠️ Heartbeat falhou: {e}")

async def bridge(local_reader, local_writer, tunnel_writer, ch_id):
    """Encaminha dados do SSH local de volta para o túnel."""
    try:
        while True:
            data = await local_reader.read(4096)
            if not data:  
                logger.info(f"🔌 [REMOTA-ACCESS] Sessão SSH terminada (sshd local fechou leitura, Ch{ch_id})")
                break
            header = struct.pack(HEADER_FMT, ch_id, MSG_DATA, len(data))
            tunnel_writer.write(header + data)
            await tunnel_writer.drain()
    except Exception as e:
        logger.info(f"🔌 [REMOTA-ACCESS] Sessão SSH terminada (erro: {e})")
    finally:
        local_writer.close()
        logger.info(f"🧹 [REMOTA-ACCESS] Recursos SSH libertados (Ch{ch_id})")

async def run_client():
    backoff = RECONNECT_BACKOFF_INITIAL
    max_backoff = RECONNECT_BACKOFF_MAX
    reconnect_count = 0
    session_start = time.time()
    
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, lambda: asyncio.create_task(shutdown_handler(loop)))

    try:
        while True:
            try:
                logger.info(f"🔌 A ligar a {HOST}:{PORT}... (Tentativa #{reconnect_count+1})")
                reader, writer = await asyncio.wait_for(
                    asyncio.open_connection(HOST, PORT), timeout=10.0
                )

                # 🔐 Handshake de Autenticação
                token_payload = AUTH_TOKEN.encode()
                auth_header = struct.pack(HEADER_FMT, 0, MSG_AUTH, len(token_payload))
                writer.write(auth_header + token_payload)
                await writer.drain()

                # 🔹 TCP Keepalive (Linux)
                sock = writer.get_extra_info('socket')
                if sock:
                    sock.setsockopt(socket.SOL_SOCKET, socket.SO_KEEPALIVE, 1)
                    sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_KEEPIDLE, 5)
                    sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_KEEPINTVL, 5)
                    sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_KEEPCNT, 3)
                
                logger.info("✅ Ligação estabelecida!")
                reconnect_count = 0
                backoff = RECONNECT_BACKOFF_INITIAL
                
                read_task = asyncio.create_task(read_loop(reader, writer))
                hb_task = asyncio.create_task(adaptive_heartbeat_loop(writer))

                # Abre canal de proxy SSH
                payload = SSH_TARGET.encode()
                writer.write(struct.pack(HEADER_FMT, PROXY_CHANNEL_ID, MSG_CHANNEL_OPEN, len(payload)) + payload)
                await writer.drain()
                logger.info(f"📤 Proxy SSH solicitado (Ch{PROXY_CHANNEL_ID})")
                logger.info(f"🌐 Proxy HTTP pronto: Ch{WEB_PROXY_CHANNEL} -> localhost:{WEB_SERVER_PORT}")
                
                while True:
                    await asyncio.sleep(60)
                    
            except (ConnectionRefusedError, OSError, asyncio.TimeoutError) as e:
                reconnect_count += 1
                jitter = random.uniform(0, backoff * 0.3)
                delay = min(backoff + jitter, max_backoff)
                uptime = time.time() - session_start
                logger.warning(f"⚠️ Falha #{reconnect_count} (uptime: {uptime:.0f}s). Reconexão em {delay:.1f}s...")
                await asyncio.sleep(delay)
                backoff = min(backoff * 2, max_backoff)
                
    except asyncio.CancelledError:
        logger.info("👋 Interrompido. A fechar túnel...")
    except KeyboardInterrupt:
        pass
    finally:
        if 'writer' in locals() and not writer.is_closing():
            writer.close()
            try: await writer.wait_closed()
            except: pass
        logger.info("🛑 Cliente terminado")

async def shutdown_handler(loop):
    logger.info("🛑 [Cliente] Sinal recebido. A encerrar graciosamente...")
    for task in asyncio.all_tasks(loop):
        task.cancel()

if __name__ == "__main__":
    try:
        asyncio.run(run_client())
    except KeyboardInterrupt:
        logger.info("👋 Interrompido pelo utilizador.")
