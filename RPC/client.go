package geerpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/hobbyGG/7days-gorpc/RPC/codec"
)

type Call struct {
	Seq           int
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
	seq      int
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
	client.pending[uint64(call.Seq)] = call
	client.seq++
	return uint64(call.Seq), nil
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
		call := client.removeCall(uint64(h.Seq))
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
		seq:     1,
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
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(network, address)
	defer func() {
		if client == nil {
			_ = conn.Close()
		}
	}()
	return NewClient(conn, opt)
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
	client.seq = call.Seq
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

func (client *Client) Call(ServiceMethod string, args, reply interface{}) error {
	call := <-client.Go(ServiceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}
