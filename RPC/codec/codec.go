package codec

import (
	"io"
)

type Type string

// 这里我们使用conn初始化一个解析类型
type NewCodecFunc func(io.ReadWriteCloser) Codec

// 将Type与NewCodecFunc构造函数进行哈希映射
var NewCoidecFuncMap map[Type]NewCodecFunc

// 将类型与构造方法进行链接
func init() {
	NewCoidecFuncMap = make(map[Type]NewCodecFunc)
	NewCoidecFuncMap[GobType] = NewGobCodec
}

// 用于协商信息
type Header struct {
	ServiceMethod string
	Seq           uint64
	Error         string
}

type Codec interface {
	io.Closer                         //这是一个关闭接口
	ReadHeader(*Header) error         //需要读取头部报
	ReadBody(interface{}) error       // 需要读取主体信息
	Write(*Header, interface{}) error //写入conn，即传输数据
}
