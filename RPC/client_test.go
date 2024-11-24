package geerpc

import (
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClient_dailTimeout(t *testing.T) {
	t.Parallel()
	l, _ := net.Listen("tcp", ":0")

	f := func(conn net.Conn, opt *Option) (client *Client, err error) {
		_ = conn.Close()
		time.Sleep(2 * time.Second)
		return nil, err
	}

	t.Run("timeout", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", l.Addr().String(), &Option{ConnectionTimeout: time.Second})
		_assert(t, err != nil && strings.Contains(err.Error(), "connect timeout"), "expect a timeout error")
	})
	t.Run("0", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", l.Addr().String(), &Option{ConnectionTimeout: 0})
		_assert(t, err == nil, "0 means no limit")
	})
}

func TestXDial(t *testing.T) {
	if runtime.GOOS == "linux" {
		ch := make(chan struct{})
		addr := "/tmp/geerpc.sock"
		go func() {
			_ = os.Remove(addr)
			l, err := net.Listen("unix", addr)
			if err != nil {
				return
			}
			ch <- struct{}{}
			Accept(l)
		}()
		select {
		case <-time.After(time.Second * 5):
			t.Fatal("failed to listen unix socket")
		case <-ch:
			_, err := XDial("unix@" + addr)
			_assert(t, err == nil, "failed to connect unix socket")
		}
	}
}
