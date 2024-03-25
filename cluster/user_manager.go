package cluster

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/common/util"
	gc "github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/proxy/manager"
	"github.com/lureiny/v2raymg/proxy/sub"
	"github.com/lureiny/v2raymg/proxy/sub/expand"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

var defaultTags = []string{}

// 全局的管理全局的user
type UserManager struct {
	users        map[string]*proto.User // key = name, value = User
	lock         sync.RWMutex
	proxyManager *manager.ProxyManager
}

func NewUserManager() *UserManager {
	um := &UserManager{}
	return um
}

func (um *UserManager) Init(proxyManager *manager.ProxyManager) {
	um.proxyManager = proxyManager
	um.LoadUser()
	// 不指定tag同时default_tag配置为空, 则获取全部tag的订阅
	defaultTags = gc.GetStringSlice(common.ConfigProxyDefaultTags)

	localDefaultTags := []string{}
	if len(defaultTags) == 0 {
		localDefaultTags = um.proxyManager.GetTags()
	} else {
		localDefaultTags = defaultTags
	}

	// 添加用户到默认tag下
	for name, user := range um.users {
		if len(user.Tags) == 0 {
			for _, tag := range localDefaultTags {
				u := &manager.User{
					Tag:   tag,
					Email: name,
				}
				// 添加到默认tag的inbound中
				err := um.proxyManager.AddUser(u)
				if err != nil {
					logger.Error(
						"Err=Add user to default inbound err > %v|User=%s|Tag=%s",
						err,
						u.Email,
						u.Tag,
					)
				} else {
					user.Tags = append(user.Tags, tag)
				}
			}
		}
	}
}

// 从配置文件中加载用户列表, 对于不存在的用户添加到默认的inbound下
func (um *UserManager) LoadUser() {
	um.users = map[string]*proto.User{}
	um.lock = sync.RWMutex{}
	usersLocal := gc.GetStringMapString(common.ConfigUsers)
	um.lock.Lock()
	defer um.lock.Unlock()

	userTagMap := um.proxyManager.GetUsersTag()
	for k, v := range usersLocal {
		l := strings.Split(v, "|")
		passwd := ""
		var expireTime int64 = 0
		if len(l) != 2 {
			passwd = v
		} else {
			e, err := strconv.ParseInt(l[1], 10, 64)
			if err != nil {
				passwd = v
				expireTime = 0
			} else {
				passwd = l[0]
				expireTime = e
			}
		}
		um.users[k] = &proto.User{
			Name:       k,
			Passwd:     passwd,
			ExpireTime: expireTime,
			Tags:       userTagMap[k],
		}
	}
}

func proxyUserOp(user *proto.User, opType string, proxyManager *manager.ProxyManager) (succTags, faileTags []string, err error) {
	err = nil

	if len(user.Tags) == 0 {
		user.Tags = []string{gc.GetString(common.ConfigProxyDefaultTags)}
	}

	for _, tag := range user.Tags {
		bUser, err := manager.NewUser(user.Name, tag)
		if err != nil {
			logger.Error(
				"Err=%v|User=%s|Tag=%s",
				err,
				user.Name,
				tag,
			)
			faileTags = append(faileTags, tag)
			continue
		}
		switch strings.ToLower(opType) {
		case "add":
			err = proxyManager.AddUser(bUser)
		case "delete":
			err = proxyManager.RemoveUser(bUser)
		case "reset":
			err = proxyManager.ResetUser(bUser)
		default:
			err = fmt.Errorf("unsupport proxy user op[%s]", opType)
		}
		if tag == "" {
			continue
		}
		if err != nil {
			logger.Error(
				"Err=%v|User=%s|Tag=%s",
				err,
				user.Name,
				tag,
			)
			faileTags = append(faileTags, tag)
			continue
		}
		succTags = append(succTags, tag)
	}
	if len(faileTags) > 0 {
		err = fmt.Errorf("succ tag list: [%s], fail tag list: [%s]", strings.Join(succTags, ", "), strings.Join(faileTags, ", "))
	}
	return
}

func checkUserTag(user *proto.User, proxyManager *manager.ProxyManager) {
	if len(user.Tags) == 0 {
		if len(defaultTags) == 0 {
			user.Tags = proxyManager.GetTags()
		} else {
			user.Tags = defaultTags
		}
	}
}

func (um *UserManager) Add(user *proto.User) error {
	if user.Name == "" {
		return fmt.Errorf("Empty user")
	} else if !um.HaveUser(user) && user.Passwd == "" {
		// 第一次添加
		return fmt.Errorf("Empty passwd")
	}

	checkUserTag(user, um.proxyManager)
	// 先尝试添加到proxy
	succTags, _, err := proxyUserOp(user, "add", um.proxyManager)
	if len(succTags) == 0 && err != nil {
		return err
	}
	// 添加到配置文件中
	um.lock.Lock()
	if _, ok := (um.users)[user.Name]; !ok {
		user.Tags = succTags
		um.users[user.Name] = user
	} else {
		um.users[user.Name].Tags = append(um.users[user.Name].Tags, succTags...)
	}
	um.lock.Unlock()
	um.FlushUser()

	return err
}

// Delete这里只是标记删除, 实际的清除逻辑会在5秒内生效
func (um *UserManager) Delete(user *proto.User) error {
	if user.Name == "" {
		return fmt.Errorf("Empty user name")
	}
	um.lock.RLock()
	if localUser, ok := um.users[user.Name]; !ok {
		return fmt.Errorf("user[%s] is not exist", user.Name)
	} else if len(user.Tags) == 0 {
		user.Tags = localUser.Tags
	}
	um.lock.RUnlock()
	succTags, _, err := proxyUserOp(user, "delete", um.proxyManager)
	if len(succTags) == 0 && err != nil {
		return err
	}

	// 从配置文件中删除
	um.lock.Lock()
	if u, ok := (um.users)[user.Name]; ok {
		// 删除对应tag
		oldTags := util.StringList(u.Tags)
		newTags := oldTags.Filter(
			func(t string) bool {
				for _, succTag := range succTags {
					if succTag == t {
						return false
					}
				}
				return true
			})
		// 如果全部tag都被清除, 则在将作为无效用户清除
		u.Tags = newTags
		// 通过设置expire time让程序自动清除
		if len(u.Tags) == 0 {
			u.ExpireTime = 1
		}
	} else {
		logger.Warn("user[%s] is not exist", user.Name)
	}
	um.lock.Unlock()
	um.FlushUser()

	return err
}

// 更新用户passwd
func (um *UserManager) Update(user *proto.User) error {
	if user.Passwd == "" {
		return fmt.Errorf("empty passwd")
	}
	var err error = nil
	// 只更新存在的用户
	um.lock.Lock()
	if u, ok := (um.users)[user.Name]; ok {
		// TODO: 需要细化变更类型, 否则每次都需要传递passwd
		u.ExpireTime = user.GetExpireTime()
		u.Passwd = user.GetPasswd()
	} else {
		err = fmt.Errorf("user[%s] is not exist", user.Name)
	}
	um.lock.Unlock()
	um.FlushUser()
	return err
}

// 重置用户proxy的uuid
func (um *UserManager) Reset(user *proto.User) error {
	checkUserTag(user, um.proxyManager)
	_, _, err := proxyUserOp(user, "reset", um.proxyManager)
	return err
}

func (um *UserManager) HaveUser(user *proto.User) bool {
	_, ok := um.users[user.Name]
	return ok
}

func (um *UserManager) ListUsers() []string {
	users := []string{}
	for key := range um.users {
		users = append(users, key)
	}
	return users
}

// 清除无效用户, 全局级, 不区分tag, proxy manager中支持tag级的user管理
// 过期和无有效tag均为无效用户
func (um *UserManager) ClearInvalideUser() {
	currentTime := time.Now().Unix()
	expireUser := []*proto.User{}
	um.lock.RLock()
	for _, v := range um.users {
		if v.ExpireTime > 0 && v.ExpireTime < currentTime {
			expireUser = append(expireUser, v)
		}
	}
	um.lock.RUnlock()

	um.lock.Lock()
	for _, user := range expireUser {
		if len(user.Tags) > 0 {
			// 重新删除一遍, 尽量保证用户真正被删除
			_, _, err := proxyUserOp(user, "delete", um.proxyManager)
			if err != nil {
				logger.Error("clear expire err > %v\n", err)
			}
		}
		// 强制删除, 不论proxy中是否删除成功
		delete(um.users, user.Name)
		logger.Info("clear invalide user: %v", user)
	}
	um.lock.Unlock()
	um.FlushUser()
}

func (um *UserManager) Get(userName string) *proto.User {
	if user, ok := um.users[userName]; ok {
		return user
	}
	return nil
}

func (um *UserManager) GetUserList() map[string]*proto.User {
	um.lock.RLock()
	defer um.lock.RUnlock()
	return um.users
}

// flush user to config file
// flush不是实时的, 最多有一秒的延迟
func (um *UserManager) FlushUser() {
	userMap := map[string]string{}
	um.lock.RLock()
	for k, v := range um.users {
		userMap[k] = fmt.Sprintf("%s|%d", v.Passwd, v.ExpireTime)
	}
	um.lock.RUnlock()

	gc.Set("users", userMap)
}

// 获取用户订阅信息
func (um *UserManager) GetUserSub(user *proto.User, excludeProtocols *util.StringList, useSNI bool) ([]string, error) {
	if _, ok := um.users[user.Name]; !ok {
		return nil, fmt.Errorf("user[%s] is not exist", user.Name)
	}
	localUser := um.users[user.Name]
	if localUser.ExpireTime < time.Now().Unix() && localUser.ExpireTime > 0 {
		return nil, fmt.Errorf("expired user[%s], expired time is %d", user.Name, localUser.ExpireTime)
	}
	if localUser.Passwd != user.Passwd {
		return nil, fmt.Errorf("wrong passwd")
	}

	if len(user.Tags) == 0 {
		user.Tags = localUser.Tags
	}
	return getUserSubUri(user, excludeProtocols, useSNI, um)
}

func getSubUriHead(uri string) string {
	subs := strings.Split(uri, ":")
	return subs[0]
}

func getUserSubUri(user *proto.User, excludeProtocols *util.StringList, useSNI bool, um *UserManager) ([]string, error) {
	proxyHost := gc.GetString(common.ConfigProxyHost)
	proxyPort := gc.GetInt(common.ConfigProxyPort)

	// get sub不会返回错误, 只会打印日志
	uris := util.StringList{}
	for _, tag := range user.Tags {
		uri, err := sub.GetUserSubUri(user.Name, tag, proxyHost, globalLocalNode.Name, uint32(proxyPort), useSNI)
		if err != nil {
			logger.Error(
				"Err=%v|User=%s|Tag=%s",
				err,
				user.Name,
				tag,
			)
			continue
		}
		if !excludeProtocols.Contains(getSubUriHead(uri)) {
			uris = append(uris, uri)
		}
	}

	// get thrid sub
	thirdSubs, err := expand.GetSubFromThirdUrl()
	if err != nil {
		logger.Error("Err=Get Third subs fail > %v", err)
	}
	for _, u := range thirdSubs {
		if !excludeProtocols.Contains(getSubUriHead(u)) {
			uris = append(uris, u)
		}
	}

	// get hysteria sub
	if hysteriaUri := um.proxyManager.GetUserSub(user.Name, user.Passwd, globalLocalNode.Name); !excludeProtocols.Contains(getSubUriHead(hysteriaUri)) {
		uris = append(uris, hysteriaUri)
	}

	uris = uris.Filter(func(u string) bool {
		return len(u) > 0
	})
	return uris, nil
}

func IsUserComplete(user *proto.User, checkPasswd bool) bool {
	if checkPasswd {
		return user.GetName() != "" && user.GetPasswd() != ""
	} else {
		return user.GetName() != ""
	}
}
