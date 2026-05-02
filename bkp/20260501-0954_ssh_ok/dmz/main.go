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

const (
	HeaderSize = 5
	MaxPayload = 65535
)

type MessageType byte

const (
	MsgData        MessageType = 0
	MsgHeartbeat   MessageType = 1
	MsgChannelOpen MessageType = 2
	MsgChannelClose MessageType = 3
)

var (
	activeClientConn net.Conn
	connMu           sync.Mutex
	proxyConns       = make(map[uint16]net.Conn) // Canal -> Ligação SSH externa
	proxyMu          sync.Mutex
)

func main() {
	listener, err := net.Listen("tcp", "0.0.0.0:8080")
	if err != nil {
		fmt.Println("❌ Erro ao ouvir 8080:", err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Println("🌐 [DMZ] Servidor binário ativo em 0.0.0.0:8080")

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
	ln, err := net.Listen("tcp", "0.0.0.0:2222")
	if err != nil {
		fmt.Println("❌ Erro ao ouvir 2222:", err)
		return
	}
	fmt.Println("🔑 [DMZ] Proxy SSH ativo em :2222 -> Remota:22")

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
	const proxyCh = uint16(99)

	// Regista esta ligação no mapa global
	proxyMu.Lock()
	proxyConns[proxyCh] = extConn
	proxyMu.Unlock()
	defer func() {
		proxyMu.Lock()
		delete(proxyConns, proxyCh)
		proxyMu.Unlock()
	}()

	// Pede à Remota para abrir a ponte SSH
	target := []byte("127.0.0.1:22")
	sendFramed(proxyCh, MsgChannelOpen, target)

	// Lê do cliente SSH externo e encaminha para o túnel
	buf := make([]byte, 4096)
	for {
		n, err := extConn.Read(buf)
		if err != nil {
			fmt.Println("🔌 [DMZ] SSH externo desconectou")
			return
		}
		sendFramed(proxyCh, MsgData, buf[:n])
	}
}

func handleClient(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, HeaderSize)
	
	// Desativa timeout para suportar sessões SSH longas/idle
	conn.SetReadDeadline(time.Time{})

	for {
		if _, err := io.ReadFull(conn, buf); err != nil {
			if err != io.EOF {
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

		if length > MaxPayload { continue }

		payload := make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil { return }

		// 🔹 ROTEAMENTO: Se for canal 99, encaminha direto para o SSH externo
		if channelID == 99 {
			proxyMu.Lock()
			ext := proxyConns[99]
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
			fmt.Printf("📥 [DMZ] Ch:%d | Data:%q\n", channelID, string(payload))
			ack := []byte(fmt.Sprintf("ACK:%s", string(payload)))
			sendFramed(channelID, MsgData, ack)
		case MsgHeartbeat:
			// Silencioso
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
