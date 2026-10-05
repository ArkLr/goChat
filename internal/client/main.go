// internal/client/main.go

package main

import (
	"fmt"
	"net"
)

func main() {
	// Connect to the server
	conn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer conn.Close()

	data := []byte("Hello, TCP!")
	_, err = conn.Write(data)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
}
