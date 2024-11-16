package main

import (
	"bufio"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int  // 协商的标识字符
	CodecType   Type // 协商使用的编码类型
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   GobType, // 默认使用0x3bef5c对应的gob类型
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json" // 不实现
)

// 将Type与NewCodecFunc构造函数进行哈希映射
var NewCoidecFuncMap map[Type]NewCodecFunc

// 将类型与构造方法进行链接
func init() {
	NewCoidecFuncMap = make(map[Type]NewCodecFunc)
	NewCoidecFuncMap[GobType] = NewGobCodec
}

type Header struct {
	ServiceMethod string
	Seq           int
	Error         string
}

type Codec interface {
	io.Closer                         //这是一个关闭接口
	ReadHeader(*Header) error         //需要读取头部报
	ReadBody(interface{}) error       // 需要读取主体信息
	Write(*Header, interface{}) error //写入conn，即传输数据
}

// 对于jsonCodec实现也是类似的
type GobCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer //用于暂存要写入的数据
	dec  *gob.Decoder
	enc  *gob.Encoder
}

// 这一行代码是初始化了一个空Codec实例，作用是用于检查你的这个结构体是否满足了抽象出的接口
var _ Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

// 读取传来的gob流Header数据
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		// 这里为什么不处理错误了
		// 这里是执行写入数据，不用接收错误
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()

	// gob的encoder维护着一个buf，编码后的数据流都会在这个buf里
	if err := c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header", err)
		return err
	}
	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}
	return nil
}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}

// RPC服务
type Server struct{}

func NewServer() *Server {
	return &Server{}
}

// 默认处理
var DefaultServer = NewServer()

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Panicln("rpc server: accept error:", err)
			return
		}
		go server.ServeConn(conn)
	}
}

// io.rwc是一个通用io接口，conn实现了这个io接口的rwc方法所以可以直接传入
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()

	// 需要实现的主要功能是解析option的内容，获取到header和body的编解码格式
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error:", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalide magic number %x", opt.MagicNumber)
		return
	}
	f := NewCoidecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}

	// 解析出编码方式，即解析完协商数据，之后交给serveCodec处理
	// 对header与body的处理与响应由serveCodec处理
	server.serveCodec(f(conn))
}

var invalidRequest = struct{}{}

func (server *Server) serveCodec(cc Codec) {

	// 实现conn传来的header与body并做出响应
	sending := new(sync.Mutex) //互斥锁,如果不加这个锁，当多报文携带多个请求时，由于请求时并发处理的，会出现数数据粘滞的情况
	wg := new(sync.WaitGroup)
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				// 请求出现错误时因为连接被关闭，接收到的报文有问题，这时可以结束服务
				break
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		// 正常获取请求，这里加锁是防止由于并发导致的提前退出的情况，使用waitgroup是常见的处理并发同步的手法
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg)
	}
	wg.Wait()
	_ = cc.Close()
}

// 服务端接收的请求结构体
type request struct {
	h            *Header       //一个header信息
	argv, replyv reflect.Value // 客户端传来的参数以及我们要响应的数据
}

func (server *Server) readRequestHeader(cc Codec) (*Header, error) {
	var h Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read head error:", err)
		}
		return nil, err
	}
	return &h, nil
}

// 获取header和参数
func (server *Server) readRequest(cc Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}

	// 目前我们不知道请求参数的类型
	// 为了实现最简单的功能，我们假设是string
	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

func (server *Server) sendResponse(cc Codec, h *Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

func (server *Server) handleRequest(cc Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	// 首先实现一个打印argv功能，并返回hello消息
	// 调用注册RPC方法得到正确会回应
	defer wg.Done() //完成了请求额处理就消去一个waitgroup
	// 打印argv
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("geerpc resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

func startServer(addr chan string) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	Accept(l)
}

func main() {
	addr := make(chan string)
	go startServer(addr)

	conn, _ := net.Dial("tcp", <-addr)
	defer func() {
		_ = conn.Close()
	}()

	time.Sleep(time.Second)

	_ = json.NewEncoder(conn).Encode(DefaultOption)
	cc := NewGobCodec(conn)

	for i := 0; i < 5; i++ {
		h := &Header{
			ServiceMethod: "Foo.Sum",
			Seq:           i,
		}
		_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))
		_ = cc.ReadHeader(h)
		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
