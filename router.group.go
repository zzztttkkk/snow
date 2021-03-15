package sha

import (
	"fmt"
	"net/http"
	"strings"
)

type _MuxGroup struct {
	_MiddlewareNode

	prefix string
	parent *_MuxGroup
	mux    *Mux
}

func (m *_MuxGroup) Websocket(path string, handlerFunc WebSocketHandlerFunc, opt *HandlerOptions) {
	m.HTTPWithOptions(opt, "get", path, wshToHandler(handlerFunc))
}

func (m *_MuxGroup) FileSystem(opt *HandlerOptions, method, path string, fs http.FileSystem, autoIndex bool) {
	if !strings.HasSuffix(path, "/{filepath:*}") {
		panic(fmt.Errorf("sha.mux: path must endswith `/{filepath:*}`"))
	}
	m.HTTPWithOptions(opt, method, path, makeFileSystemHandler(fs, autoIndex))
}

func (m *_MuxGroup) FileContent(opt *HandlerOptions, method, path, filepath string) {
	if strings.Contains(path, "{") {
		panic(fmt.Errorf("sha.mux: path can not contains `{.*}`"))
	}
	m.HTTPWithOptions(opt, method, path, makeFileContentHandler(filepath))
}

func (m *_MuxGroup) HTTP(method, path string, handler RequestHandler) {
	m.HTTPWithOptions(nil, method, path, handler)
}

var _ Router = (*_MuxGroup)(nil)

func (m *_MuxGroup) HTTPWithOptions(opt *HandlerOptions, method, path string, handler RequestHandler) {
	m.add(nil, method, m.prefix+path, handler, opt)
}

func (m *_MuxGroup) add(childMiddlewares []Middleware, method, path string, handler RequestHandler, opt *HandlerOptions) {
	var ms []Middleware
	ms = append(ms, m.local...)
	ms = append(ms, childMiddlewares...)

	if m.parent != nil {
		m.parent.add(ms, method, path, handler, opt)
		return
	}

	if len(ms) != 0 {
		if opt == nil {
			opt = &HandlerOptions{
				Middlewares: ms,
			}
		} else {
			nopt := &HandlerOptions{}
			nopt.Document = opt.Document
			nopt.Middlewares = append(nopt.Middlewares, ms...)
			nopt.Middlewares = append(nopt.Middlewares, opt.Middlewares...)
			opt = nopt
		}
	}

	m.mux.HTTPWithOptions(opt, method, path, handler)
}

func (m *_MuxGroup) NewGroup(prefix string) Router {
	return &_MuxGroup{
		prefix: prefix,
		parent: m,
		mux:    m.mux,
	}
}
