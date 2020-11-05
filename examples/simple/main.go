package main

import (
	"github.com/zzztttkkk/suna/rbac"
	"github.com/zzztttkkk/suna/sqls"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/savsgio/gotils"
	"github.com/valyala/fasthttp"
	"github.com/zzztttkkk/suna"
	"github.com/zzztttkkk/suna/auth"
	"github.com/zzztttkkk/suna/config"
	"github.com/zzztttkkk/suna/middleware"
	"github.com/zzztttkkk/suna/output"
	"github.com/zzztttkkk/suna/router"
	"github.com/zzztttkkk/suna/validator"
)

var RedisUrl = "redis://127.0.0.1:6379"
var SqlUrl = "postgres://postgres:123456@localhost/suna_examples_simple?sslmode=disable"
var MysqlUrl = "root:123456@/suna_examples_simple"

type User struct {
	id int64
}

func (user *User) GetId() int64 {
	return user.id
}

var authenticator = auth.AuthenticatorFunc(
	func(ctx *fasthttp.RequestCtx) (auth.User, bool) {
		if gotils.B2S(ctx.FormValue("token")) == "123456" {
			return &User{id: 1}, true
		}
		return nil, false
	},
)

func grantRbacPermsToUser1() {
	ctx := &fasthttp.RequestCtx{}
	req := fasthttp.AcquireRequest()
	req.SetRequestURI("/?token=123456")
	ctx.Init(req, nil, nil)

	txCtx, committer := sqls.TxByUser(ctx)
	defer committer()

	if rbac.SubjectHasRole(txCtx, 1, "root") {
		return
	}

	_ = rbac.NewRole(txCtx, "root", "root")
	for _, perm := range rbac.Permissions(txCtx) {
		if !strings.HasPrefix(perm.Name, "rbac.") {
			continue
		}
		_ = rbac.RoleAddPerm(txCtx, "root", perm.Name)
	}
	_ = rbac.SubjectAddRole(txCtx, 1, "root")
}

func main() {
	conf := config.Default()
	conf.Redis.Mode = "singleton"
	conf.Redis.Nodes = append(conf.Redis.Nodes, RedisUrl)
	conf.Sql.Driver = "postgres"
	conf.Sql.Leader = SqlUrl
	conf.Sql.Logging = true

	conf.Done()

	suna.Init(&suna.InitOption{Config: conf, Authenticator: authenticator})

	root := router.New(nil)
	root.NotFound = output.NotFound
	root.MethodNotAllowed = output.MethodNotAllowed
	root.CompressionOptions.Enable = true

	root.Use(
		middleware.NewTimeoutAndAccessLoggingMiddleware(
			"${UserId:06d} ${Remote} ${Method} ${Path} "+
				"${StatusCode} ${StatusText} IsTimeout:${IsTimeout} ${TimeSpent:03d}ms\nErrorStack:\n${ErrStack}\n",
			time.Second, 0, "", output.Recover,
		),
		middleware.NewRateLimiter(
			10,
			time.Second, 10,
			func(ctx *fasthttp.RequestCtx) string { return ctx.RemoteAddr().String() },
		),
	)

	var emptyRegexp = regexp.MustCompile(`\s+`)
	var emptyBytes = []byte("")

	validator.RegisterFunc(
		"username",
		func(data []byte) ([]byte, bool) {
			v := emptyRegexp.ReplaceAll(data, emptyBytes)
			return v, len(v) > 3
		},
		"remove all space characters and make sure the length is greater than 3",
	)

	type Form struct {
		Ignore string `validator:"-"`
		Name   string `validator:"L<3-20>;F<username>;D<null>;I<username>"`
	}

	root.GETWithDoc(
		"/hello",
		func(ctx *fasthttp.RequestCtx) {
			form := Form{}
			if !validator.Validate(ctx, &form) {
				return
			}
			output.MsgOK(ctx, form.Name)
		},
		validator.MakeDoc(Form{}, "print hello."),
	)
	root.GET("/doc", root.DocHandler())

	loader := router.NewLoader()
	loader.AddChild("/rbac", rbac.Loader())

	go func() {
		time.Sleep(time.Second)
		grantRbacPermsToUser1()
	}()

	loader.RunAsHttpServer(root, conf.Http.Address, conf.Http.TLS.Cert, conf.Http.TLS.Key)
}
