package geerpc

import (
	"fmt"
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func TestService(t *testing.T) {
	t.Run("test Service", func(t *testing.T) {
		var foo Foo
		s := newService(&foo)
		_assert(t, len(s.method) == 1, "wrong service Method, expect 1, but got %d", len(s.method))
		mType := s.method["Sum"]
		_assert(t, mType != nil, "wrong Method, Sum shouldn't nil")
	})

	t.Run("test call", func(t *testing.T) {
		var foo Foo
		s := newService(&foo)
		mType := s.method["Sum"]

		argv := mType.newArgv()
		reply := mType.newReplyv()
		argv.Set(reflect.ValueOf(Args{Num1: 1, Num2: 3}))
		err := s.call(mType, argv, reply)
		_assert(t, err == nil && *reply.Interface().(*int) == 4 && mType.NumCalls() == 1, "failed to call Foo.Sum")
	})
}

func _assert(t *testing.T, condition bool, msg string, v ...interface{}) {
	t.Helper()
	if !condition {
		panic(fmt.Sprintf("assertion failed:"+msg, v...))
	}
}
