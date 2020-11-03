package rbac

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"
	"github.com/zzztttkkk/suna/output"
	"github.com/zzztttkkk/suna/sqls"
	"github.com/zzztttkkk/suna/utils"
)

type _SubjectOpT struct {
	roles *sqls.Operator
	lru   *utils.Lru
}

var SubjectOperator = &_SubjectOpT{
	roles: &sqls.Operator{},
}

func init() {
	dig.Provide(
		func(_ _DigRoleTableInited) _DigUserTableInited {
			SubjectOperator.roles.Init(subjectWithRoleT{})
			SubjectOperator.lru = utils.NewLru(cfg.Cache.Lru.UserSize)
			return _DigUserTableInited(0)
		},
	)
}

func (op *_SubjectOpT) changeRole(ctx context.Context, subjectId int64, roleName string, mt modifyType) error {
	OP := op.roles

	roleId := _RoleOperator.GetIdByName(ctx, roleName)
	if roleId < 1 {
		return output.HttpErrors[fasthttp.StatusNotFound]
	}
	defer LogOperator.Create(
		ctx,
		"user.changeRole",
		utils.M{"user": subjectId, "role": fmt.Sprintf("%d:%s", roleId, roleName), "modify": mt.String()},
	)
	defer op.lru.Remove(strconv.FormatInt(subjectId, 16))

	cond := sqls.STR("role=? and subject=?", roleId, subjectId)
	var _id int64
	OP.ExecSelect(ctx, &_id, sqls.Select("role").Where(cond))
	if _id < 1 {
		if mt == _Del {
			return nil
		}
		OP.ExecInsert(ctx, sqls.Insert("subject, role, created").Values(subjectId, roleId, time.Now().Unix()))
		return nil
	}

	if mt == _Add {
		return nil
	}

	OP.ExecDelete(ctx, sqls.Delete().Where(cond).Limit(1))
	return nil
}

func (op *_SubjectOpT) getRoles(ctx context.Context, userId int64) []int64 {
	v, ok := op.lru.Get(strconv.FormatInt(userId, 16))
	if ok {
		return v.([]int64)
	}

	OP := op.roles
	lst := make([]int64, 0, 10)
	OP.ExecSelect(
		ctx,
		&lst,
		sqls.Select("role").Distinct().
			Where("subject=? and role>0 and status>=0 and deleted=0", userId).OrderBy("role"),
	)
	op.lru.Add(strconv.FormatInt(userId, 16), lst)
	return lst
}

func (op *_SubjectOpT) hasRole(ctx context.Context, userId int64, roleName string) bool {
	roles := op.getRoles(ctx, userId)
	tRid := _RoleOperator.GetIdByName(ctx, roleName)
	if tRid < 0 {
		return false
	}

	for _, rid := range roles {
		if rid == tRid {
			return true
		}
	}

	g.RLock()
	defer g.RUnlock()
	for _, rid := range roles {
		m := roleInheritMap[rid]
		if len(m) > 0 && m[tRid] {
			return true
		}
	}
	return false
}
