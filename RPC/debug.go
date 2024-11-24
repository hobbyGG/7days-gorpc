package geerpc

import (
	"fmt"
	"net/http"
	"text/template"
)

type debugHTTP struct {
	*Server
}

type debugService struct {
	Name   string
	Method map[string]*methodType
}

func (server *debugHTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var debug = template.Must(template.ParseFiles("E:\\Work\\CodeForStudy\\7days-rpc\\static\\debug.html"))
	var services []debugService
	server.serviceMap.Range(func(namei, svci interface{}) bool {
		svc := svci.(*service)
		services = append(services, debugService{
			Name:   namei.(string),
			Method: svc.method,
		})
		return true
	})
	err := debug.Execute(w, services)
	if err != nil {
		_, _ = fmt.Fprintln(w, "rpc: error executing template:", err.Error())
	}
}
