// tunnel-server/config.go
// ==========================================
// Configurações do Servidor Túnel (VM DMZ)
// ==========================================

package main

// 🔌 Portas de escuta
const (
	ServerPort  = 8080  // Porta do protocolo binário
	ProxySSHPort = 2222 // Porta para conexões SSH externas
)

// 🌐 Web Proxy via Túnel
const (
	WebProxyChannel    = 100  // Canal dedicado para proxy HTTP
	WebProxyListenPort = 8888 // Porta onde o proxy escuta na DMZ
)

// 📦 Protocolo Binário
const (
	HeaderSize = 5           // Tamanho do header: 2+1+2 bytes
	MaxPayload = 65535       // Tamanho máximo do payload em bytes
)

// Tipos de mensagem (devem coincidir com o cliente Python)
type MessageType byte

const (
	MsgData        MessageType = 0
	MsgHeartbeat   MessageType = 1
	MsgChannelOpen MessageType = 2
	MsgChannelClose MessageType = 3
)

// 🔐 Proxy SSH
const (
	ProxyChannelID = 99              // Canal dedicado para SSH forwarding
	SSHTarget      = "127.0.0.1:22"  // Destino local do proxy
)

// ⏱️ Comportamento
var (
	ReadDeadlineEnabled = false    // false = sessões SSH longas/idle suportadas
	LogDebug            = false    // false = modo produção (menos logs)
)

// 🌐 Rede
const (
	ListenAddress = "0.0.0.0" // Interface de escuta (todas)
)
