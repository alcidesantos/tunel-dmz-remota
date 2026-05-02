package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// 🔧 Variáveis globais
var (
	activeClientConn net.Conn
	connMu           sync.Mutex
	proxyConns       = make(map[uint16]net.Conn)
	proxyMu          sync.Mutex
	wg               sync.WaitGroup
	ctx              context.Context
	cancel           context.CancelFunc
)

func main() {
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	addr := fmt.Sprintf("%s:%d", ListenAddress, ServerPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("❌ Erro ao ouvir %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Printf("🌐 [DMZ] Servidor binário ativo em %s\n", addr)

	go startSSHProxyListener()
	go startWebProxyListener()

	// 🔹 Handler de sinais otimizado
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println("\n🛑 [DMZ] Encerramento solicitado...")
		
		// 1. Fecha o listener → desbloqueia Accept()
		listener.Close()
		
		// 2. Fecha a ligação ativa → desbloqueia io.ReadFull() no handleClient
		connMu.Lock()
		if activeClientConn != nil {
			activeClientConn.Close()
		}
		connMu.Unlock()
		
		// 3. Cancela contexto para sair do loop principal
		cancel()
	}()

	// 🔹 Loop principal
	for {
		conn, err := listener.Accept()
		if err != nil {
			// Se foi fechado intencionalmente pelo signal handler
			if ctx.Err() != nil {
				fmt.Println("🏁 [DMZ] Listener fechado. A aguardar goroutines...")
				wg.Wait()
				fmt.Println("✅ [DMZ] Encerramento completo.")
				return
			}
			fmt.Println("⚠️ Erro accept:", err)
			continue
		}

		// 🔹 TCP Keepalive
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(5 * time.Second)
		}

		fmt.Printf("🔗 [DMZ] Ligação de: %v\n", conn.RemoteAddr())
		
		connMu.Lock()
		activeClientConn = conn
		connMu.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			handleClient(conn)
		}()
	}
}

func startSSHProxyListener() {
	addr := fmt.Sprintf("%s:%d", ListenAddress, ProxySSHPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("❌ Erro ao ouvir SSH %s: %v\n", addr, err)
		return
	}
	fmt.Printf("🔑 [DMZ] Proxy SSH ativo em %s -> %s\n", addr, SSHTarget)

	for {
		extConn, err := ln.Accept()
		if err != nil {
			continue
		}
		if tcpConn, ok := extConn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(5 * time.Second)
		}

		fmt.Printf("🔐 [DMZ-ACCESS] SSH de %v -> Ch:%d (%s)\n", extConn.RemoteAddr(), ProxyChannelID, SSHTarget)
		fmt.Println("📡 [DMZ] Ligação SSH externa recebida")

		wg.Add(1)
		go func() {
			defer wg.Done()
			handleSSHBridge(extConn)
		}()
	}
}

func handleSSHBridge(extConn net.Conn) {
	defer extConn.Close()
	proxyMu.Lock()
	proxyConns[ProxyChannelID] = extConn
	proxyMu.Unlock()
	defer func() {
		proxyMu.Lock()
		delete(proxyConns, ProxyChannelID)
		proxyMu.Unlock()
	}()

	target := []byte(SSHTarget)
	sendFramed(ProxyChannelID, MsgChannelOpen, target)
	fmt.Printf("🔄 [DMZ] Bridge SSH Ch:%d ativo - a encaminhar dados para %s\n", ProxyChannelID, SSHTarget)

	buf := make([]byte, 4096)
	for {
		n, err := extConn.Read(buf)
		if err != nil {
			if err == io.EOF {
				fmt.Println("🔌 [DMZ-ACCESS] Sessão SSH terminada (cliente desligou)")
			} else {
				fmt.Printf("🔌 [DMZ-ACCESS] Sessão SSH terminada com erro: %v\n", err)
			}
			return
		}
		sendFramed(ProxyChannelID, MsgData, buf[:n])
	}
}

func handleClient(conn net.Conn) {
	defer conn.Close()

	// 🔐 FASE 1: Handshake de Autenticação (antes de aceitar tráfego)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	authBuf := make([]byte, HeaderSize)
	if _, err := io.ReadFull(conn, authBuf); err != nil {
		if LogDebug { fmt.Println("🔐 [DMZ] Timeout/erro no handshake auth") }
		return
	}

	// ✅ Usa "=" em vez de ":=" para evitar conflitos de declaração
	_ = binary.BigEndian.Uint16(authBuf[0:2])
	authType := MessageType(authBuf[2])
	authLen := binary.BigEndian.Uint16(authBuf[3:5])

	if authType != MsgAuth || authLen == 0 || authLen > 64 {
		if LogDebug { fmt.Println("🔐 [DMZ] Handshake auth inválido (tipo/len)") }
		return
	}

	token := make([]byte, authLen)
	if _, err := io.ReadFull(conn, token); err != nil {
		return
	}

	// Remove deadline para sessões longas (SSH/HTTP)
	conn.SetReadDeadline(time.Time{})

	if string(token) != AuthToken {
		if LogDebug { fmt.Println("🔐 [DMZ] Token inválido. Ligação rejeitada.") }
		return
	}
	fmt.Println("🔐 [DMZ] Cliente autenticado com sucesso.")

	// 🔐 FASE 2: Loop principal (teu código original segue daqui para baixo)
	buf := make([]byte, HeaderSize)
	if !ReadDeadlineEnabled {
		conn.SetReadDeadline(time.Time{})
	} else {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}

	for {
		if ReadDeadlineEnabled {
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}

		if _, err := io.ReadFull(conn, buf); err != nil {
			if err != io.EOF && LogDebug {
				fmt.Println("⚠️ [DMZ] Leitura falhou:", err)
			}
			connMu.Lock()
			if activeClientConn == conn { activeClientConn = nil }
			connMu.Unlock()
			return
		}

		channelID := binary.BigEndian.Uint16(buf[0:2])
		msgType := MessageType(buf[2])
		length := binary.BigEndian.Uint16(buf[3:5])

		if length > MaxPayload {
			if LogDebug { fmt.Println("🚫 [DMZ] Payload demasiado grande") }
			continue
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil { return }

		if channelID == WebProxyChannel && msgType == MsgData {
			if dispatchProxyResponse(channelID, payload) {
				fmt.Printf("📤 [DMZ] WebProxy Ch:%d dispatched (%dB)\n", channelID, len(payload))
			} else {
				fmt.Printf("⚠️ [DMZ] WebProxy Ch:%d sem handler registado\n", channelID)
			}
			continue
		}

		if channelID == ProxyChannelID {
			proxyMu.Lock()
			ext := proxyConns[ProxyChannelID]
			proxyMu.Unlock()
			if ext != nil { ext.Write(payload) }
			continue
		}

		switch msgType {
		case MsgChannelOpen:
			fmt.Printf("🔓 [DMZ] Canal %d aberto\n", channelID)
		case MsgData:
			if LogDebug { fmt.Printf("📥 [DMZ] Ch:%d | Data:%q\n", channelID, string(payload)) }
			ack := []byte(fmt.Sprintf("ACK:%s", string(payload)))
			sendFramed(channelID, MsgData, ack)
		case MsgHeartbeat:
			if LogDebug { fmt.Printf("💓 [DMZ] Heartbeat recebido (canal %d)\n", channelID) }
		case MsgChannelClose:
			fmt.Printf("🔒 [DMZ] Canal %d fechado\n", channelID)
		}
	}
}

func sendFramed(chID uint16, msgType MessageType, payload []byte) {
	connMu.Lock()
	defer connMu.Unlock()
	if activeClientConn == nil { return }

	header := make([]byte, HeaderSize)
	binary.BigEndian.PutUint16(header[0:2], chID)
	header[2] = byte(msgType)
	binary.BigEndian.PutUint16(header[3:5], uint16(len(payload)))

	activeClientConn.Write(header)
	activeClientConn.Write(payload)
}
