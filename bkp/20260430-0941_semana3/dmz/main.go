package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
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

type Channel struct {
	ID   uint16
	Open bool
}

func main() {
	listener, err := net.Listen("tcp", "0.0.0.0:8080")
	if err != nil {
		fmt.Println(" Erro ao ouvir:", err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Println("🌐 [DMZ] Servidor multiplexado ativo em 0.0.0.0:8080")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("️ Erro accept:", err)
			continue
		}
		fmt.Println("🔗 [DMZ] Ligação de:", conn.RemoteAddr())
		go handleClient(conn)
	}
}

func handleClient(conn net.Conn) {
	defer conn.Close()
	channels := make(map[uint16]*Channel)
	var mu sync.Mutex
	buf := make([]byte, HeaderSize)

	for {
		if _, err := io.ReadFull(conn, buf); err != nil {
			if err != io.EOF {
				fmt.Println("️ Erro leitura header:", err)
			}
			return
		}

		channelID := binary.BigEndian.Uint16(buf[0:2])
		msgType := MessageType(buf[2])
		length := binary.BigEndian.Uint16(buf[3:5])

		if length > MaxPayload {
			fmt.Println("🚫 [DMZ] Payload demasiado grande")
			return
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil {
			fmt.Println("⚠️ Erro leitura payload:", err)
			return
		}

		mu.Lock()
		switch msgType {
		case MsgChannelOpen:
			channels[channelID] = &Channel{ID: channelID, Open: true}
			fmt.Printf(" [DMZ] Canal %d aberto\n", channelID)
			
		case MsgData:
			if ch, ok := channels[channelID]; ok && ch.Open {
				fmt.Printf("📥 [DMZ] Ch:%d | Data:%q\n", channelID, string(payload))
				// Resposta específica por canal
				ack := []byte(fmt.Sprintf("ACK_CH%d:%s", channelID, string(payload)))
				sendMessage(conn, channelID, MsgData, ack)
			} else {
				fmt.Printf("️ [DMZ] Dados rejeitados: canal %d fechado ou inexistente\n", channelID)
			}

		case MsgChannelClose:
			if _, ok := channels[channelID]; ok {
				delete(channels, channelID)
				fmt.Printf(" [DMZ] Canal %d fechado\n", channelID)
			}

		case MsgHeartbeat:
			// Ignora ou responde conforme política
		}
		mu.Unlock()
	}
}

func sendMessage(conn net.Conn, channelID uint16, msgType MessageType, payload []byte) {
	header := make([]byte, HeaderSize)
	binary.BigEndian.PutUint16(header[0:2], channelID)
	header[2] = byte(msgType)
	binary.BigEndian.PutUint16(header[3:5], uint16(len(payload)))
	conn.Write(header)
	conn.Write(payload)
}
