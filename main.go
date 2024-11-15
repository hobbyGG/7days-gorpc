package main

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

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
	Write(*Header, interface{}) error //写入
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

func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
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

func main() {

	err = client.Call("Arith.Multiply", args, &reply)

}
