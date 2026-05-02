package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
)

const (
	HeaderSize = 5
	MaxPayload = 65535
)

type MessageType byte

const (
	MsgData      MessageType = 0
	MsgHeartbeat MessageType = 1
	MsgChannelOpen MessageType = 2
	MsgChannelClose MessageType = 3
)

func main() {
	listener, err := net.Listen("tcp", "0.0.0.0:8080")
	if err != nil {
		fmt.Println("❌ Erro ao ouvir:", err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Println(" [DMZ] Servidor binário ativo em 0.0.0.0:8080")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("⚠️ Erro accept:", err)
			continue
		}
		fmt.Println("🔗 [DMZ] Ligação de:", conn.RemoteAddr())
		go handleClient(conn)
	}
}

func handleClient(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, HeaderSize)

	for {
		// 1. Ler header completo (bloqueante até receber 5 bytes)
		if _, err := io.ReadFull(conn, buf); err != nil {
			if err != io.EOF {
				fmt.Println("⚠️ Erro leitura header:", err)
			}
			return
		}

		// 2. Parse header
		channelID := binary.BigEndian.Uint16(buf[0:2])
		msgType := MessageType(buf[2])
		length := binary.BigEndian.Uint16(buf[3:5])

		if length > MaxPayload {
			fmt.Println("🚫 [DMZ] Payload demasiado grande")
			return
		}

		// 3. Ler payload
		payload := make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil {
			fmt.Println("⚠️ Erro leitura payload:", err)
			return
		}

		// 4. Processar mensagem
		fmt.Printf("📥 [DMZ] Ch:%d Type:%d Len:%d | Data:%q\n", channelID, msgType, length, string(payload))

		// 5. Responder com ACK (eco binário)
		respPayload := []byte("ACK:" + string(payload))
		respHeader := make([]byte, HeaderSize)
		binary.BigEndian.PutUint16(respHeader[0:2], channelID)
		respHeader[2] = byte(MsgData)
		binary.BigEndian.PutUint16(respHeader[3:5], uint16(len(respPayload)))

		conn.Write(respHeader)
		conn.Write(respPayload)
	}
}
