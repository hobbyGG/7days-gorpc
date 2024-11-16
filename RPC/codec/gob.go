package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

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
