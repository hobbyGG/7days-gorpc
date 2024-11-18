package main

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	geerpc "github.com/hobbyGG/7days-gorpc/RPC"
)

// 启用一个默认配置的服务器
func startServer(addr chan string) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		// 这里属于初始化操作，可以直接fatal打断
		log.Fatal("network error", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	geerpc.Accept(l)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	// 为服务器分配一个空闲端口
	go startServer(addr)

	// 客户端访问对应的socket
	client, _ := geerpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("geerpc req %d", i)
			var reply string
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error", err)
			}
			log.Println("reply:", reply)
		}(i)
	}
	wg.Wait()
}
