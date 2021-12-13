/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/v2fly/v2ray-core/v4/app/proxyman/command"
	"github.com/v2fly/v2ray-core/v4/infra/conf"
	"google.golang.org/grpc"
	"v2raymg.top/bound"
	"v2raymg.top/config"
	"v2raymg.top/fileIO"
)

// addCmd represents the add command
var (
	addCmd = &cobra.Command{
		Use:   "add",
		Short: "Add user to v2ray",
		Long:  ``,
		Run:   addUserLocal,
	}
)

func init() {
	// Required flags
	addCmd.Flags().StringVarP(&email, "email", "e", "", "Email of user.")
	addCmd.MarkFlagRequired("email")
	addCmd.Flags().StringVarP(&inBoundTag, "inboundTag", "t", "", "The inbound tag which adds user to.")
	addCmd.MarkFlagRequired("inboundTag")

	// Not necessary flags
	addCmd.Flags().StringVarP(&uuid, "uuid", "u", "", "UUID of vless or vmess.")
	addCmd.Flags().StringVarP(&protocol, "protocol", "p", "vmess", "The protocl of inbound.")
	addCmd.Flags().IntVarP(&alterID, "alterID", "a", 0, "The alter id of user.")
	addCmd.Flags().IntVarP(&level, "level", "l", 0, "The level of user.")
	addCmd.Flags().StringVarP(&configFile, "config", "c", "/usr/local/etc/v2ray/config.json", "The config file of v2ray.")
}

// AddUser 添加用户, 同时添加的运行中的程序以及配置文件中
func AddUser(runtimeConfig *RuntimeConfig, user *bound.User) error {
	err := addUserToRuntime(runtimeConfig, user)
	if err != nil {
		return err
	}

	if err := addUserToFile(user, runtimeConfig.ConfigFile); err != nil {
		return err
	}

	return nil
}

func addUserToRuntime(runtimeConfig *RuntimeConfig, user *bound.User) error {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", runtimeConfig.Host, runtimeConfig.Port), grpc.WithInsecure())
	if err != nil {
		return err
	}

	handlerClient := command.NewHandlerServiceClient(cmdConn)

	if err != nil {
		return err
	}

	err = bound.AddUser(handlerClient, user)
	if err != nil {
		return err
	}

	config.Info.Printf("Add user to runtime: [Email] %s, [UUID] %s to [Bound] %s", user.Email, user.UUID, user.InBoundTag)
	return nil
}

func addUserLocal(cmd *cobra.Command, args []string) {
	runtimeConfig := &RuntimeConfig{
		Host:       host,
		Port:       port,
		ConfigFile: configFile,
	}

	user, err := bound.NewUser(email, inBoundTag, bound.Protocol(protocol), bound.UUID(uuid))

	if err != nil {
		config.Error.Fatal(err)
	}

	err = AddUser(runtimeConfig, user)
	if err != nil {
		config.Error.Fatalf("Failed to add user > %v", err)
	}
}

func addUserToFile(user *bound.User, configFile string) error {
	c, err := fileIO.LoadConfig(configFile)
	if err != nil {
		return err
	}

	if err := addUserToConfig(c, user); err != nil {
		return err
	}

	if err := fileIO.DumpConfig(c, configFile); err != nil {
		return err
	}

	config.Info.Printf("Add user to config file: [Email] %s, [UUID] %s to [Bound] %s", user.Email, user.UUID, user.InBoundTag)
	return nil
}

func addUserToConfig(c *fileIO.V2rayConfig, user *bound.User) error {
	for index := range c.InboundConfigs {
		inBound := &(c.InboundConfigs[index])
		if inBound.Tag == user.InBoundTag {
			switch strings.ToLower(inBound.Protocol) {
			// 添加用户前应先检测是否已经存在
			case "vmess":
				return addVmessUser(inBound, user)
			case "vless":
				return addVlessUser(inBound, user)
			}
		}
	}

	return errors.New("No inbound which has tag: " + user.InBoundTag)
}

func addVmessUser(in *fileIO.InboundDetourConfig, user *bound.User) error {
	vmessConfig := new(conf.VMessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vmessConfig)
	if err != nil {
		return err
	}

	c := fileIO.V2rayInboundUser{Email: user.Email, ID: user.UUID}
	cb, err := json.Marshal(c)
	if err != nil {
		return err
	}

	vmessConfig.Users = append(vmessConfig.Users, cb)
	vmessConfigBytes, err := json.MarshalIndent(vmessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vmessConfigBytes)
	return nil
}

func addVlessUser(in *fileIO.InboundDetourConfig, user *bound.User) error {
	vlessConfig := new(conf.VLessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vlessConfig)
	if err != nil {
		return err
	}

	c := fileIO.V2rayInboundUser{Email: user.Email, ID: user.UUID}
	cb, err := json.Marshal(c)
	if err != nil {
		return err
	}

	vlessConfig.Clients = append(vlessConfig.Clients, cb)
	vlessConfigBytes, err := json.MarshalIndent(vlessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vlessConfigBytes)
	return nil
}
