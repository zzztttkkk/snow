package mware

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	"github.com/go-redis/redis/v7"
	"github.com/rs/xid"
	"github.com/valyala/fasthttp"
	"github.com/zzztttkkk/snow/mware/internal"
	"github.com/zzztttkkk/snow/router"
	"github.com/zzztttkkk/snow/secret"
	"github.com/zzztttkkk/snow/utils"
	"sync"
	"time"
)

var (
	SessionInCookie = "session"
	SessionInHeader = "session"
	SessionExpire   = time.Hour
)

var sessionKeyPrefix = "snow"

type _SessionT struct {
	id    []byte
	valid bool
	key   string
}

func (s *_SessionT) String() string {
	return utils.B2s(s.id)
}

func (s *_SessionT) fromBytes(bytesV []byte) {
	for i, v := range bytesV {
		s.id[i] = v
	}
	s.key = fmt.Sprintf("%s:session:%s", sessionKeyPrefix, s.String())
}

var sessionPool = sync.Pool{New: func() interface{} { return &_SessionT{id: make([]byte, 20, 20)} }}

func acquireSession() *_SessionT {
	return sessionPool.Get().(*_SessionT)
}

func releaseSession(s *_SessionT) {
	s.valid = false
	s.key = ""
	sessionPool.Put(s)
}

var redisClient redis.Cmdable

func (s *_SessionT) Set(key string, val interface{}) {
	bs, err := json.Marshal(val)
	if err != nil {
		panic(err)
	}
	if err = redisClient.HSet(s.key, key, bs).Err(); err != nil {
		panic(err)
	}
}

func (s *_SessionT) Get(key string, dst interface{}) bool {
	bs, err := redisClient.HGet(s.key, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return false
		}
		panic(err)
	}
	return json.Unmarshal(bs, dst) == nil
}

func (s *_SessionT) Del(keys ...string) {
	redisClient.HDel(s.key, keys...)
}

func makeUSidKey(uid int64) string {
	return fmt.Sprintf("sonw:sessionid:%d", uid)
}

func AuthToken(uid int64, seconds int, ext map[string]interface{}) string {
	v := jwt.MapClaims{
		"uid":  uid,
		"exp":  time.Now().Unix() + int64(seconds),
		"unix": time.Now().Unix(),
	}
	if len(ext) > 0 {
		v["ext"] = ext
	}
	return secret.JwtEncode(v)
}

var authTokenInHeader string
var authTokenInCookie string

func readUid(ctx *fasthttp.RequestCtx) int64 {
	var token string
	if len(authTokenInHeader) > 0 {
		bytesV := ctx.Request.Header.Peek(authTokenInHeader)
		if len(bytesV) > 0 {
			token = utils.B2s(bytesV)
		}
	}

	if len(token) < 1 && len(authTokenInCookie) > 1 {
		bytesV := ctx.Request.Header.Cookie(authTokenInCookie)
		if len(bytesV) > 0 {
			token = utils.B2s(bytesV)
		}
	}

	if len(token) < 1 {
		return -1
	}

	v, err := secret.JwtDecode(token)
	if err != nil {
		return -1
	}

	m, ok := v.(jwt.MapClaims)
	if !ok {
		return -1
	}

	uid, ok := m["uid"].(int64)
	if !ok {
		return -1
	}
	ctx.SetUserValue(internal.Uid, uid)

	lastLogin, ok := m["unix"].(int64)
	if !ok {
		return -1
	}
	ctx.SetUserValue(internal.LastLogin, lastLogin)

	ext, ok := m["ext"].(map[string]interface{})
	if ok {
		ctx.SetUserValue(internal.TokenExt, ext)
	}
	return uid
}

type UserFetcher func(ctx context.Context, uid int64) User

var userFetcher UserFetcher

const (
	sessionExistsFlagKey = "\\."
)

func SessionHandler(ctx *fasthttp.RequestCtx) {
	var session = acquireSession()
	defer releaseSession(session)

	var uid int64
	var sidKey string

	// session by uid
	uid = readUid(ctx)
	if uid > 0 {
		sidKey = makeUSidKey(uid)
		sid, _ := redisClient.Get(sidKey).Bytes()
		if len(sid) == 20 {
			session.fromBytes(sid)
			if session.valid {
				redisClient.Set(sidKey, sid, SessionExpire)
				var sUid int64
				if !session.Get("uid", &sUid) || sUid != uid {
					// delete dirty data
					redisClient.Del(session.key, sidKey)
					session.valid = false
				} else {
					ctx.SetUserValue(internal.UserKey, userFetcher(ctx, uid))
				}
			}
		}
	}

	// session by request
	if !session.valid {
		bytesV := ctx.Request.Header.Cookie(SessionInCookie)
		if len(bytesV) == 20 {
			session.fromBytes(bytesV)
		}

		if !session.valid {
			bytesV = ctx.Request.Header.Peek(SessionInHeader)
			if len(bytesV) == 20 {
				session.fromBytes(bytesV)
			}
		}
	}

	// check this sessionId is generated by our server
	if session.valid {
		var x bool
		if !session.Get(sessionExistsFlagKey, &x) {
			session.valid = false
		}
	}

	// generate new sessionId
	if !session.valid {
		session.fromBytes(utils.S2b(xid.New().String()))
		session.Set(sessionExistsFlagKey, true)
	}

	redisClient.Expire(session.key, SessionExpire)
	ctx.SetUserValue(internal.SessionKey, session)

	ck := fasthttp.AcquireCookie()
	defer fasthttp.ReleaseCookie(ck)
	ck.SetKey(SessionInCookie)
	ck.SetValue(session.String())
	ck.SetPath("/")
	ck.SetMaxAge(int(SessionExpire / time.Second))
	ctx.Response.Header.SetCookie(ck)

	router.Next(ctx)
}
