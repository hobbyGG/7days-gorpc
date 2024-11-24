package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type ServerItem struct {
	Addr  string    // 服务的连接地址
	start time.Time // 服务开始时间
}

// 只存储对于服务的一些log
func NewServerItem(addr string, start time.Time) *ServerItem {
	return &ServerItem{
		Addr:  addr,
		start: start,
	}
}

type Registry struct {
	timeout time.Duration            // 超时处理
	mu      sync.Mutex               // 多个进程互斥访问
	servers map[string](*ServerItem) // 注册的服务列表
}

const (
	defaultPath    = "/registry"
	defaultTimeout = time.Minute * 5
)

func New(timeout time.Duration) *Registry {
	return &Registry{
		timeout: timeout,
		servers: make(map[string]*ServerItem),
	}
}

var DefaultRegistry = New(defaultTimeout)

type RS interface {
	putServer(addr string)  // 注册服务
	aliveServers() []string // 返回服务实例列表
}

var _ RS = (*Registry)(nil)

func (r *Registry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.servers[addr]
	if !ok {
		r.servers[addr] = NewServerItem(addr, time.Now())
	} else {
		s.start = time.Now()
	}
}

// 心跳检测
func (r *Registry) aliveServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 记录存活的服务实例
	var alive []string
	for addr, s := range r.servers {
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	// 由于map是乱序，所以要进行排序（还有一种根据value的排序）
	sort.Strings(alive)
	return alive
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		w.Header().Set("X-rpc-servers", strings.Join(r.aliveServers(), ","))
	case "POST":
		addr := req.Header.Get("X-rpc-servers")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *Registry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path:", registryPath)
}

func HandleHTTP() {
	DefaultRegistry.HandleHTTP(defaultPath)
}

func Heartbeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		duration = defaultTimeout - time.Duration(1)*time.Minute
	}
	var err error
	go func() {
		// 等一个时间间隔
		t := time.NewTicker(duration)
		for err == nil {
			// 这里会进行一个duration的等待
			<-t.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

func sendHeartbeat(registry, addr string) error {
	log.Println(addr, "send heart beat to registry", registry)
	httpClient := &http.Client{}
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-rpc-servers", addr)
	if _, err := httpClient.Do(req); err != nil {
		log.Println("rpc server: heart beat err:", err)
		return err
	}
	return nil
}
