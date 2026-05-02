// tunnel-server/webproxy.go
package main

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// Registo de proxies à espera de resposta: channelID -> callback
var (
	proxyHandlers   = make(map[uint16]chan []byte)
	proxyHandlersMu sync.Mutex
)

// Regista um handler para receber respostas de um canal
func registerProxyHandler(chID uint16) chan []byte {
	proxyHandlersMu.Lock()
	defer proxyHandlersMu.Unlock()
	respChan := make(chan []byte, 1)
	proxyHandlers[chID] = respChan
	return respChan
}

// Remove handler após uso
func unregisterProxyHandler(chID uint16) {
	proxyHandlersMu.Lock()
	defer proxyHandlersMu.Unlock()
	delete(proxyHandlers, chID)
}

// Envia resposta para o handler registado (chamado por handleClient)
func dispatchProxyResponse(chID uint16, payload []byte) bool {
	proxyHandlersMu.Lock()
	defer proxyHandlersMu.Unlock()
	if respChan, ok := proxyHandlers[chID]; ok {
		select {
		case respChan <- payload:
			return true
		default:
			return false
		}
	}
	return false
}

func startWebProxyListener() {
	addr := fmt.Sprintf("%s:%d", ListenAddress, WebProxyListenPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("⚠️ [DMZ] Web proxy falhou: %v\n", err)
		return
	}
	fmt.Printf("🌐 [DMZ] Web proxy ativo: http://%s -> Túnel -> Remota:80\n", addr)

	for {
		clientConn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleWebProxy(clientConn)
	}
}

func handleWebProxy(clientConn net.Conn) {
	defer clientConn.Close()

	// Lê request HTTP do browser (com timeout)
	clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	request := make([]byte, 65535)
	n, err := clientConn.Read(request)
	clientConn.SetReadDeadline(time.Time{})
	if err != nil || n == 0 {
		return
	}
	request = request[:n]
	fmt.Printf("📥 [WebProxy] Request lido: %dB\n", len(request))

	// Regista handler para receber resposta do Ch:100
	respChan := registerProxyHandler(WebProxyChannel)
	defer unregisterProxyHandler(WebProxyChannel)

	// Verifica estado do túnel ANTES de enviar
	connMu.Lock()
	conn := activeClientConn
	connMu.Unlock()

	if conn == nil {
		fmt.Printf("❌ [WebProxy] activeClientConn é NIL - túnel indisponível\n")
		clientConn.Write([]byte("HTTP/1.1 503 Service Unavailable\r\nConnection: close\r\n\r\nTunnel offline"))
		return
	}
	fmt.Printf("✅ [WebProxy] activeClientConn válido, a enviar pelo túnel...\n")

	// Envia request pelo túnel
	sendFramed(WebProxyChannel, MsgData, request)
	fmt.Printf("📤 [WebProxy] sendFramed() chamado para Ch:%d\n", WebProxyChannel)

	// Aguarda resposta via channel (NÃO lê diretamente do socket!)
	select {
	case response := <-respChan:
		fmt.Printf("✅ [WebProxy] Resposta recebida (%dB), a enviar ao browser\n", len(response))
		clientConn.Write(response)
	case <-time.After(8 * time.Second):
		fmt.Printf("⚠️ [WebProxy] Timeout à espera de resposta\n")
		clientConn.Write([]byte("HTTP/1.1 504 Gateway Timeout\r\nConnection: close\r\n\r\nProxy timeout"))
	}
}
