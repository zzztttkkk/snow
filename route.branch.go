package suna

import (
	"fmt"
	"strings"
)

type _RouteBranch struct {
	_MiddlewareOrg
	allHandlers  map[string]map[string]RequestHandler
	root         *_Mux
	parentRouter *_RouteBranch
	prefix       string

	children map[string]*_RouteBranch
}

func (branch *_RouteBranch) AddHandler(method, path string, handler RequestHandler) {
	method = strings.ToUpper(method)
	m := branch.allHandlers[method]
	if m == nil {
		m = map[string]RequestHandler{}
		branch.allHandlers[method] = m
	}
	m[path] = handler
}

func (branch *_RouteBranch) AddBranch(prefix string, router Router) {
	v, ok := router.(*_RouteBranch)
	if !ok {
		panic(fmt.Errorf("suna.router: `%v` is not a branch", router))
	}
	branch.children[prefix] = v
	v.prefix = prefix
	v._MiddlewareOrg.parentMOrg = &branch._MiddlewareOrg
	v.parentRouter = branch
}

func (branch *_RouteBranch) sinking() {
	for _, v := range branch.children {
		v.sinking()
	}

	for a, b := range branch.allHandlers {
		for p, h := range b {
			branch.rising(a, p, branch.wrap(h))
		}
	}
}

func (branch *_RouteBranch) rising(method, path string, handler RequestHandler) {
	if branch.parentRouter != nil {
		branch.parentRouter.rising(method, branch.prefix+path, handler)
	} else {
		branch.root.doAddHandler(method, branch.prefix+path, handler)
	}
}

func NewBranch() Router {
	return &_RouteBranch{allHandlers: map[string]map[string]RequestHandler{}, children: map[string]*_RouteBranch{}}
}