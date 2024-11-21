package geerpc

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"

	"github.com/hobbyGG/7days-gorpc/RPC/codec"
)

const MagicNumber = 0x3bef5c

// RPC服务
type Server struct {
	serviceMap sync.Map
}

func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defined" + s.name)
	}
	return nil
}

// 公共接口
func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

func NewServer() *Server {
	return &Server{}
}

// 传入"service.method"
func (server *Server) findService(ServiceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(ServiceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed:" + ServiceMethod)
		return
	}
	serviceName, methodName := ServiceMethod[:dot], ServiceMethod[dot+1:]
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service" + serviceName)
		return
	}
	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method" + methodName)
	}
	return
}

type Option struct {
	MagicNumber int        // 协商的标识字符
	CodecType   codec.Type // 协商使用的编码类型
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType, // 默认使用0x3bef5c对应的gob类型
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
	f := codec.NewCoidecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}

	// 解析出编码方式，即解析完协商数据，之后交给serveCodec处理
	// 对header与body的处理与响应由serveCodec处理
	server.serveCodec(f(conn))
}

var invalidRequest = struct{}{}

func (server *Server) serveCodec(cc codec.Codec) {

	// 实现conn传来的header与body并做出响应
	sending := new(sync.Mutex) //互斥锁,如果不加这个锁，当多报文携带多个请求时，由于请求时并发处理的，会出现数数据粘滞的情况
	wg := new(sync.WaitGroup)
	for {
		wg.Add(1)
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
		go server.handleRequest(cc, req, sending, wg)
	}
	wg.Wait()
	_ = cc.Close()
}

// 服务端接收的请求结构体
type request struct {
	h            *codec.Header //一个header信息
	argv, replyv reflect.Value // 客户端传来的参数以及我们要响应的数据
	mtype        *methodType
	svc          *service
}

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read head error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// 获取header和参数
func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	// 获取服务与需要的方法
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()
	// 确保argvi是指针，因为readBody需要传入指针
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()
	err := req.svc.call(req.mtype, req.argv, req.replyv)
	if err != nil {
		req.h.Error = err.Error()
		server.sendResponse(cc, req.h, invalidRequest, sending)
		return
	}
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}
