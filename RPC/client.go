package geerpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hobbyGG/7days-gorpc/RPC/codec"
)

type Call struct {
	Seq           uint64
	ServiceMethod string      //service.mathod
	Args          interface{} //参数
	Reply         interface{} // Call的响应
	Error         error       // 传递时设置为空，如果有错误将会由服务端填写
	Done          chan *Call  //为了支持异步调用
}

func (call *Call) done() {
	call.Done <- call
}

// 客户端
type Client struct {
	cc       codec.Codec
	opt      *Option
	sending  sync.Mutex // 发送上锁
	header   codec.Header
	mu       sync.Mutex // 对客户端操作时上锁
	seq      uint64
	pending  map[uint64]*Call //悬挂未处理完的请求
	closing  bool             // 用户关闭
	shutdown bool             // 服务端要关闭
}

var _ io.Closer = (*Client)(nil)

var Errshutdown = errors.New("connection is shut down")

func (client *Client) Close() error {
	if err := client.cc.Close(); err != nil {
		client.mu.Lock()
		defer client.mu.Unlock()

		// 已经关闭，不可重复关闭
		if client.closing {
			return Errshutdown
		}
	}
	client.closing = true
	return client.cc.Close()
}

func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	// 保持连接的条件是未关闭且为断开
	return !client.shutdown && !client.closing
}

func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	// 这里如果是true说明已经注册过且被关闭了
	if client.closing || client.shutdown {
		return 0, Errshutdown
	}
	call.Seq = client.seq
	// 核心就是将call挂在请求队列
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()

	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			// 接收服务端响应
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body" + err.Error())
			}
			call.done()
		}
	}
	client.terminateCalls(err)
}
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	// 协商协议
	f := codec.NewCoidecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalide codec type %s", opt.CodecType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error:", err)
		_ = conn.Close()
		return nil, err
	}
	// 协商成功，调用构造函数
	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		seq:     uint64(1),
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

func parseOptions(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

// 以指定网络接入RPC服务器
func Dial(network, address string, opts ...*Option) (client *Client, err error) {
	return dialTimeout(NewClient, network, address, opts...)
}

func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()

	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	if err := client.cc.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *Client) Go(ServiceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: ServiceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

func (client *Client) Call(ctx context.Context, ServiceMethod string, args, reply interface{}) error {
	call := client.Go(ServiceMethod, args, reply, make(chan *Call, 1))
	select {
	// 将ctx与call的时间进行比较，ctx先done了则算处理完该请求
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed:" + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}

type clientResult struct {
	client *Client
	err    error
}

type newClientFunc func(conn net.Conn, opt *Option) (client *Client, err error)

func dialTimeout(f newClientFunc, network, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		log.Println("dial fail, parse option fail")
		return nil, err
	}
	// 添加超时连接处理
	conn, err := net.DialTimeout(network, address, opt.ConnectionTimeout)
	if err != nil {
		return nil, err
	}
	// 如果客户端为nil就关闭连接
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	// 使用子协程创建客户端，以此实现select的超时功能
	ch := make(chan clientResult)
	go func() {
		// 创建客户端
		client, err := f(conn, opt)
		ch <- clientResult{client: client, err: err}
	}()
	if opt.ConnectionTimeout == 0 {
		// 如果为1就是不设超时限制，直接返回创建的客户端即可
		result := <-ch
		return result.client, result.err
	}
	select {
	case <-time.After(opt.ConnectionTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout except within %s", opt.ConnectionTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

func NewHTTPClient(conn net.Conn, opt *Option) (*Client, error) {
	// "CONNECT %s HTTP/1.0\n\n"：这是一个格式化字符串，%s 是一个占位符，它会被 defaultRPCPath 的值替换。\n\n 表示两个换行符，这是 HTTP 协议中请求头和请求体之间的分隔符。
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))

	// 获取指定method的响应
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err
}

func DialHTTP(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

// rpcAddr is a general format (protocol@addr) to represent a rpc server
// 用法如http@localhost/_geerpc/
func XDial(rpcAddr string, opts ...*Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocal@addr", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return DialHTTP("tcp", addr, opts...)
	default:
		return Dial(protocol, addr, opts...)
	}
}
