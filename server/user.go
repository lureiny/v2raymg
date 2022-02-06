package server

import (
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/bound"
	"github.com/lureiny/v2raymg/config"
	"github.com/spf13/viper"
)

var Users struct {
	users *map[string]string
	lock  sync.RWMutex
}

func UpdatetUsers() {
	usersLocal := viper.GetStringMapString("users")
	Users.lock.Lock()
	defer Users.lock.Unlock()

	Users.users = &usersLocal
}

func InitUserService(t, apiHost, proxyHost, configFile, defaultTag string, apiPort, proxyPort int) {
	RestfulServer.GET("/user", func(c *gin.Context) {
		token := c.DefaultQuery("token", "")
		email := c.Query("user")
		pwd := c.Query("pwd")
		tag := c.DefaultQuery("tag", defaultTag)
		opType := c.DefaultQuery("type", "")

		if token != t {
			c.String(200, "error token")
			return
		}

		if pwd == "" || email == "" {
			c.String(200, "Empty pwd or user")
			return
		}

		switch opType {
		case "1": // 增加用户
			if err := addUser(apiHost, configFile, tag, email, pwd, apiPort); err != nil {
				config.Error.Println(err.Error())
				c.String(200, "")
				return
			}
		case "2": // 更新用户pwd
			updateUser(email, pwd)
		case "3": // 删除用户
			if err := removeUser(apiHost, configFile, tag, email, pwd, apiPort); err != nil {
				config.Error.Println(err.Error())
				c.String(200, "")
				return
			}

		case "4": // reset id
			if err := addUser(apiHost, configFile, tag, email, pwd, apiPort); err != nil {
				config.Error.Println(err.Error())
				c.String(200, "")
				return
			}
			if err := removeUser(apiHost, configFile, tag, email, pwd, apiPort); err != nil {
				config.Error.Println(err.Error())
				c.String(200, "")
				return
			}
		default:
			c.String(200, "Unsupport option")
			return
		}
		c.String(200, "Succ")
	})
}

func addUser(apiHost, configFile, tag, email, pwd string, apiPort int) error {
	go func(email, pwd string) {
		Users.lock.Lock()
		defer Users.lock.Unlock()
		if _, ok := (*Users.users)[email]; !ok {
			(*Users.users)[email] = pwd
			viper.Set("users", *Users.users)
			viper.WriteConfig()
		}
	}(email, pwd)

	runtimeConfig := &config.RuntimeConfig{
		Host:       apiHost,
		Port:       apiPort,
		ConfigFile: configFile,
	}

	p, err := bound.GetProtocol(tag, configFile)
	if err != nil {
		return err
	}

	user, err := bound.NewUser(email, tag, bound.Protocol(p))

	if err != nil {
		return err
	}

	err = bound.AddUser(runtimeConfig, user)
	if err != nil {
		return err
	}

	return nil
}

func updateUser(email, pwd string) {
	Users.lock.Lock()
	defer Users.lock.Unlock()
	(*Users.users)[email] = pwd
	viper.Set("users", *Users.users)
	viper.WriteConfig()
}

func removeUser(apiHost, configFile, tag, email, pwd string, apiPort int) error {
	go func(email string) {
		Users.lock.Lock()
		defer Users.lock.Unlock()
		delete(*Users.users, email)
		viper.Set("users", *Users.users)
		viper.WriteConfig()
	}(email)

	runtimeConfig := &config.RuntimeConfig{
		Host:       apiHost,
		Port:       apiPort,
		ConfigFile: configFile,
	}

	p, err := bound.GetProtocol(tag, configFile)
	if err != nil {
		return err
	}

	user, err := bound.NewUser(email, tag, bound.Protocol(p))
	if err != nil {
		return err
	}

	if err := bound.RemoveUser(runtimeConfig, user); err != nil {
		return err
	}

	return nil
}
