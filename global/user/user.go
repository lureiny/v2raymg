package user

import (
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/common/util"
	"github.com/lureiny/v2raymg/global/proxy"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

var globalUserManager *cluster.UserManager = cluster.NewUserManager()

// InitUserManager ...
func InitUserManager() error {
	globalUserManager.Init(proxy.GetProxyManager())
	return nil
}

// GetUserManager ...
func GetUserManager() *cluster.UserManager {
	return globalUserManager
}

// Add add user
func Add(user *proto.User) error {
	return globalUserManager.Add(user)
}

// Delete delete user
func Delete(user *proto.User) error {
	return globalUserManager.Delete(user)
}

// Update udpate passwd, expire time
func Update(user *proto.User) error {
	return globalUserManager.Update(user)
}

// Reset reset user uuid, user need to update sub info
func Reset(user *proto.User) error {
	return globalUserManager.Reset(user)
}

// HaveUser have user?
func HaveUser(user *proto.User) bool {
	return globalUserManager.HaveUser(user)
}

// ListUsers get user list, only user name
func ListUsers() []string {
	return globalUserManager.ListUsers()
}

// ClearInvalideUser ...
func ClearInvalideUser() {
	globalUserManager.ClearInvalideUser()
}

// Get ...
func Get(userName string) *proto.User {
	return globalUserManager.Get(userName)
}

// GetUserList ...
func GetUserList() map[string]*proto.User {
	return globalUserManager.GetUserList()
}

// FlushUser ...
func FlushUser() {
	globalUserManager.FlushUser()
}

// GetUserSub ...
func GetUserSub(user *proto.User, excludeProtocols *util.StringList, useSNI bool) ([]string, error) {
	return globalUserManager.GetUserSub(user, excludeProtocols, useSNI)
}
