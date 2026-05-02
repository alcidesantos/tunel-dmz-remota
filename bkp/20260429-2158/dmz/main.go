package main

import (
    "bufio"
    "fmt"
    "net"
    "os"
)

func main() {
    port := ":8080"
    fmt.Printf("🚀 [DMZ] A ouvir em %s...\n", port)

    // 1. Ouvir a ligação TCP
    listener, err := net.Listen("tcp", port)
    if err != nil {
        fmt.Println("Erro ao ouvir:", err)
        os.Exit(1)
    }
    defer listener.Close()

    for {
        // 2. Aceitar ligação
        conn, err := listener.Accept()
        if err != nil {
            fmt.Println("Erro ao aceitar:", err)
            continue
        }

        fmt.Println("✅ [DMZ] Ligação recebida de:", conn.RemoteAddr())

        // 3. Ler dados (Goroutine para lidar com este cliente)
        go handleConnection(conn)
    }
}

func handleConnection(conn net.Conn) {
    defer conn.Close()
    scanner := bufio.NewScanner(conn)

    // Lê linha a linha
    for scanner.Scan() {
        msg := scanner.Text()
        fmt.Printf("📩 [DMZ] Recebido: %s\n", msg)
        
        // Responde ao cliente (ECHO)
        conn.Write([]byte("ACK: " + msg + "\n"))
    }
}
