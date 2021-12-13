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

// removeCmd represents the remove command
var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a user from inbound.",
	Run:   removeUserLocal,
}

func init() {
	removeCmd.Flags().StringVarP(&email, "email", "e", "", "Email of user.")
	removeCmd.MarkFlagRequired("email")
	removeCmd.Flags().StringVarP(&inBoundTag, "inboundTag", "t", "", "The inbound tag which remove user from.")
	removeCmd.MarkFlagRequired("inboundTag")
	removeCmd.Flags().StringVarP(&configFile, "config", "c", "/usr/local/etc/v2ray/config.json", "The config file of v2ray.")
}

func RemoveUser(runtimeConfig *RuntimeConfig, user *bound.User) error {
	if err := removeUserFromRuntime(runtimeConfig, user); err != nil {
		return err
	}

	if err := removeUserFromFile(user, runtimeConfig.ConfigFile); err != nil {
		return err
	}

	return nil
}

func removeUserFromFile(user *bound.User, configFile string) error {
	c, err := fileIO.LoadConfig(configFile)
	if err != nil {
		return err
	}

	if err := removeUserFromConfig(c, user); err != nil {
		return err
	}

	if err := fileIO.DumpConfig(c, configFile); err != nil {
		return err
	}

	config.Info.Printf("Remove user from config file: [Email] %s from [Bound] %s", user.Email, user.InBoundTag)
	return nil
}

func removeUserFromConfig(c *fileIO.V2rayConfig, user *bound.User) error {
	for index := range c.InboundConfigs {
		inBound := &(c.InboundConfigs[index])
		if inBound.Tag == user.InBoundTag {
			switch strings.ToLower(inBound.Protocol) {
			// 添加用户前应先检测是否已经存在
			case "vmess":
				return removeVmessUser(inBound, user)
			case "vless":
				return removeVlessUser(inBound, user)
			}
		}
	}

	return errors.New("No inbound which has tag: " + user.InBoundTag)
}

func removeUserFromRuntime(runtimeConfig *RuntimeConfig, user *bound.User) error {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", runtimeConfig.Host, runtimeConfig.Port), grpc.WithInsecure())
	if err != nil {
		return err
	}

	handlerClient := command.NewHandlerServiceClient(cmdConn)

	if err != nil {
		return err
	}

	err = bound.RemoveUser(handlerClient, user)
	if err != nil {
		return err
	}
	config.Info.Printf("Remove User from runtime: [Email] %s from [Bound] %s", user.Email, user.InBoundTag)

	return nil
}

func removeUserLocal(cmd *cobra.Command, args []string) {
	runtimeConfig := &RuntimeConfig{
		Host:       host,
		Port:       port,
		ConfigFile: configFile,
	}

	user, err := bound.NewUser(email, inBoundTag, bound.Protocol(protocol), bound.UUID(uuid))
	if err != nil {
		config.Error.Fatal(err)
	}

	if err := RemoveUser(runtimeConfig, user); err != nil {
		config.Error.Fatal(err)
	}
}

func removeVmessUser(in *fileIO.InboundDetourConfig, user *bound.User) error {
	vmessConfig := new(conf.VMessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vmessConfig)
	if err != nil {
		return err
	}

	for index := range vmessConfig.Users {
		var vmessUser fileIO.V2rayInboundUser
		json.Unmarshal(vmessConfig.Users[index], &vmessUser)
		if vmessUser.Email == user.Email {
			vmessConfig.Users = append(vmessConfig.Users[:index], vmessConfig.Users[index+1:]...)
			break
		}
		if index == len(vmessConfig.Users)-1 {
			return errors.New("No User " + user.Email)
		}
	}

	vmessConfigBytes, err := json.MarshalIndent(vmessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vmessConfigBytes)
	return nil
}

func removeVlessUser(in *fileIO.InboundDetourConfig, user *bound.User) error {
	vlessConfig := new(conf.VLessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vlessConfig)
	if err != nil {
		return err
	}

	for index := range vlessConfig.Clients {
		var vlessUser fileIO.V2rayInboundUser
		json.Unmarshal(vlessConfig.Clients[index], &vlessUser)
		if vlessUser.Email == user.Email {
			vlessConfig.Clients = append(vlessConfig.Clients[:index], vlessConfig.Clients[index+1:]...)
			break
		}
		if index == len(vlessConfig.Clients)-1 {
			return errors.New("No User " + user.Email)
		}
	}

	vlessConfigBytes, err := json.MarshalIndent(vlessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vlessConfigBytes)
	return nil
}
