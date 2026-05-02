package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// 🔧 Todas as configurações vêm de config.go
// (HeaderSize, MaxPayload, MessageType consts, ProxyChannelID, SSHTarget, etc.)

var (
	activeClientConn net.Conn
	connMu           sync.Mutex
	proxyConns       = make(map[uint16]net.Conn) // Canal -> Ligação SSH externa
	proxyMu          sync.Mutex
)

func main() {
	// 🔧 Usa constantes de config
	addr := fmt.Sprintf("%s:%d", ListenAddress, ServerPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("❌ Erro ao ouvir %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Printf("🌐 [DMZ] Servidor binário ativo em %s\n", addr)

	go startSSHProxyListener()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("⚠️ Erro accept:", err)
			continue
		}
		fmt.Println(" [DMZ] Ligação de:", conn.RemoteAddr())
		
		connMu.Lock()
		activeClientConn = conn
		connMu.Unlock()

		go handleClient(conn)
	}
}

func startSSHProxyListener() {
	addr := fmt.Sprintf("%s:%d", ListenAddress, ProxySSHPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("❌ Erro ao ouvir %s: %v\n", addr, err)
		return
	}
	fmt.Printf("🔑 [DMZ] Proxy SSH ativo em %s -> %s\n", addr, SSHTarget)

	for {
		extConn, err := ln.Accept()
		if err != nil {
			fmt.Println("⚠️ Erro accept SSH:", err)
			continue
		}
		fmt.Println("📡 [DMZ] Ligação SSH externa recebida")
		go handleSSHBridge(extConn)
	}
}

func handleSSHBridge(extConn net.Conn) {
	defer extConn.Close()

	// Regista esta ligação no mapa global
	proxyMu.Lock()
	proxyConns[ProxyChannelID] = extConn
	proxyMu.Unlock()
	defer func() {
		proxyMu.Lock()
		delete(proxyConns, ProxyChannelID)
		proxyMu.Unlock()
	}()

	// Pede à Remota para abrir a ponte SSH
	target := []byte(SSHTarget)
	sendFramed(ProxyChannelID, MsgChannelOpen, target)

	// Lê do cliente SSH externo e encaminha para o túnel
	buf := make([]byte, 4096)
	for {
		n, err := extConn.Read(buf)
		if err != nil {
			if LogDebug {
				fmt.Println("🔌 [DMZ] SSH externo desconectou")
			}
			return
		}
		sendFramed(ProxyChannelID, MsgData, buf[:n])
	}
}

func handleClient(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, HeaderSize)
	
	// 🔧 Timeout configurável
	if !ReadDeadlineEnabled {
		conn.SetReadDeadline(time.Time{}) // Desativa para sessões SSH longas
	} else {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}

	for {
		// Renova deadline se estiver ativo
		if ReadDeadlineEnabled {
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}

		if _, err := io.ReadFull(conn, buf); err != nil {
			if err != io.EOF {
				if LogDebug {
					fmt.Println("⚠️ [DMZ] Leitura falhou:", err)
				}
			}
			connMu.Lock()
			if activeClientConn == conn {
				activeClientConn = nil
			}
			connMu.Unlock()
			return
		}

		channelID := binary.BigEndian.Uint16(buf[0:2])
		msgType := MessageType(buf[2])
		length := binary.BigEndian.Uint16(buf[3:5])

		if length > MaxPayload {
			if LogDebug {
				fmt.Println("🚫 [DMZ] Payload demasiado grande")
			}
			continue
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}

		// 🔹 ROTEAMENTO: Se for canal de proxy, encaminha direto
		if channelID == ProxyChannelID {
			proxyMu.Lock()
			ext := proxyConns[ProxyChannelID]
			proxyMu.Unlock()
			if ext != nil {
				ext.Write(payload)
			}
			continue
		}

		// 🔹 Tratamento padrão para canais de teste/heartbeat
		switch msgType {
		case MsgChannelOpen:
			fmt.Printf("🔓 [DMZ] Canal %d aberto\n", channelID)
		case MsgData:
			if LogDebug {
				fmt.Printf("📥 [DMZ] Ch:%d | Data:%q\n", channelID, string(payload))
			}
			ack := []byte(fmt.Sprintf("ACK:%s", string(payload)))
			sendFramed(channelID, MsgData, ack)
		case MsgHeartbeat:
			if LogDebug {
				fmt.Printf("💓 [DMZ] Heartbeat recebido (canal %d)\n", channelID)
			}
		case MsgChannelClose:
			fmt.Printf("🔒 [DMZ] Canal %d fechado\n", channelID)
		}
	}
}

func sendFramed(chID uint16, msgType MessageType, payload []byte) {
	connMu.Lock()
	defer connMu.Unlock()
	if activeClientConn == nil {
		return
	}

	header := make([]byte, HeaderSize)
	binary.BigEndian.PutUint16(header[0:2], chID)
	header[2] = byte(msgType)
	binary.BigEndian.PutUint16(header[3:5], uint16(len(payload)))
	
	activeClientConn.Write(header)
	activeClientConn.Write(payload)
}
