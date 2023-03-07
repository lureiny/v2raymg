# V2raymg

基于v2ray/xray提供的api对v2ray/xray进行管理, 支持单机和集群模式

## 功能列表

> <font color="red">使用者需要对v2ray/xray的配置有最基本的了解</font>

### 集群模式

- 支持通过中心节点发现其他节点
- 支持不通过中心节点, 通过级联的方式感知全部节点
- 集群中任一节点都可以作为入口节点管理集群内任意节点
- 集群内节点自感知其他节点状态, 可以自动剔除无效节点, 无效是指无法连接
- 支持设置gateway模式, 即仅转发请求, 不使用proxy相关功能, 支持动态设置gateway模式, 可以用来屏蔽某个节点的proxy

### 用户管理

- 增、删、改、查
- 过期管理
- 流量查询

### 订阅

- 支持多种客户端

### proxy管理

- 增加、删除、复制、迁移inbound
- inbound自动迁移
- proxy升级
- xray(已完成测试) + v2ray(暂未完成全部功能测试)

## 使用方法

`v2raymg server --conf {config file}`, 暂时未支持日志文件与后台运行, 因此建议使用输出重定向: `nohup ./v2raymg server >> v2raymg.log &`

`--conf {config file}`可以省略, 此时会使用默认配置文件`/usr/local/etc/v2raymg/config.json`

### api 接口

为了方便通过浏览器操作, 所有的接口都响应GET请求

```
/sub
	获取订阅
	/sub?target={target}&user={user}&pwd={pwd}&tags={tags}
	target: 目标节点
	user: user name
	pwd: password
	tags: inbound的tag列表, 使用","分隔
	
/help/{relativePath}
	返回指定路径的help信息, 当relativePath为空时返回全部help信息
	
/bound
	inbound操作接口, 支持添加, 删除, 迁移, 复制inbound, inbound间复制用户, 获取inbound
	通用参数列表:
	target: 目标node的名称
	token: 用于验证操作权限
	type: 操作类型, 可选值有addInbound, deleteInbound, transferInbound, copyInbound, copyUser, getInbound
	各个接口参数说明: 
	1. 添加inbound
	/bound?type=addInbound&boundRawString={boundRawString}&token={token}
	boundRawString, json中inbound配置base64编码后的字符串
	2. 删除inbound
	/bound?type=deleteInbound&src_tag={src_tag}&token={token}
	src_tag, 要删除inbound的tag
	3. 迁移inbound
	迁移inbound仅切换端口
	/bound?type=transferInbound&src_tag={src_tag}&new_port={new_port}&token={token}
	src_tag, 要迁移inbound的tag
	new_port, 新的端口
	4. 复制inbound
	/bound?type=copyInbound&src_tag={src_tag}&new_port={new_port}&dstTag={dst_tag}&dst_protocol={dst_protocol}&is_copy_user={is_copy_user}&token={token}
	src_tag, 被复制inbound的tag
	new_port, 新的端口
	dst_tag, 新inbound的tag
	dst_protocol, 新的协议类型, 仅支持vmess, vless, trojan
	is_copy_user, 是否同时复制用户, "is_copy_user == 1"时为复制, 默认复制
	5. inbound间复制用户
	/bound?type=copyUser&src_tag={src_tag}&dst_tag={dst_tag}&token={token}
	src_tag, 被复制inbound的tag
	dst_tag, 新的tag
	6. 获取inbound详细配置
	/bound?type=getInbound&src_tag={src_tag}&token={token}
	src_tag, 想要获取inbound的tag
	
/user
	user操作接口, 支持添加, 删除, 更新user信息, 重置用户proxy的密钥信息, 获取用户列表
	通用参数列表:
	target: 目标node的名称
	tags: 操作的inbound的tag, 使用","分隔
	type: 操作类型
	token: 用于验证操作权限
	各个接口参数说明:
	1. 添加用户
	/user?type=1&user={user}&pwd={pwd}&expire={expire}&target={target}&token={token}
	user: 用户名
	pwd: password
	expire: 过期时间, 过期时间的时间戳, 例如2022-11-27 12:00:00过期, 则expire=1669521600
	2. 更新用户信息
	/user?type=2&user={user}&pwd={pwd}&expire={expire}&target={target}&token={token}
	user: 用户名
	pwd: password
	expire: 过期时间, 过期时间的时间戳, 例如2022-11-27 12:00:00过期, 则expire=1669521600
	3. 删除用户
	/user?type=3&target={target}&user={user}&token={token}
	user: 用户名
	4. 重置用户
	/user?target={target}&type=4&user={user}&token={token}
	user: 用户名
	5. 获取用户列表
	/user?type=5&target={target}&token={token}
	
/adaptive
	对每一个指定tag的inbound, 从配置的port库中随机选择一个, 更新指定tag的端口
	请求示例: /adaptive?tags=tag1,tag2&target=target&token={token}
	参数列表: 
	token: 用于验证操作权限
	tags: 需要操作的inbound tag, 使用","分割
	target: 目标node的名称
	
/adaptiveOp
	修改端口区间
	请求示例: /adaptiveOp?type=add&target=target1&tags=tag1,tag2&ports=10000&token={token}
	参数列表: 
	token: 用于验证操作权限
	type: 操作类型, 可选值为add, del, 默认值为add
	target: 目标node的名称
	tags: 需要操作的inbound tag, 使用","分割
	ports: 添加/删除的端口, 支持单个port及端口范围(10000-10004)
	
/node
	/node?token={token}
	获取当前集群内的全部节点
	参数列表:
	token: 用于验证操作权限
	
/stat
	获取指定节点的统计信息, 需要proxy配置中开启统计
	/stat?target={target}&reset={reset}&pattern={pattern}&token={token}
	参数列表:
	target: 目标node名称
	token: 用于验证操作权限
	reset: 是否重置统计数据
	pattern: 查询参数, 默认情况下查询全部统计信息, 包含inbound与用户信息
	
/tag
	获取目标节点的所有inbound tag
	/tag?target={target}&token={token}
	参数列表:
	target: 目标node
	token: 用于验证操作权限
	
/update
	更新目标节点的proxy版本
	/update?target={target}&version_tag={version_tag}&token={token}
	参数列表:
	target: 目标node
	token: 用于验证操作权限
	version_tag: github上目标tag, 默认为最新版。v2ray: https://github.com/v2fly/v2ray-core/releases, xray: https://github.com/XTLS/Xray-core/releases
```

## 编译方法

使用make进行构建, 构建后的文件存放在`bin`目录下

```shell
make v2ray #  构建v2ray版本
make xray  #  构建xray版本
```

## 配置文件说明

```json
{
  "cluster": {
    "center_node": { // 中心节点相关配置
      "host": "localhost",
      "port": 0 // 为0时不会使用中心节点
    },
    "token": "", // 集群内节点间验证用的token, 可以为空, 但不建议设置为空
    "name": "test", // 集群名字, 相同集群的节点需要具有相同集群名称
    "nodes": [ // 集群内其他节点信息, 不使用中心节点时可以使用此种方式搭建集群, 只要集群中不存在孤岛节点, 集群内的节点即可全部互相感知
      {
        "name": "node1", // 节点名称, 不可以重名
        "port": 10000,
        "host": "127.0.0.1"
      }
    ]
  },
  "proxy": {
    "config_file": "/usr/local/etc/xray/config.json", // xray/v2ray配置文件路径
    "default_tags": [
      "vless"
    ], // 默认操作的inbound  tag, 为空时会在全部外部inbound上操作
    "host": "127.0.0.1", // 本地的ip/host, 生成订阅时需要用到
    "port": 443, // proxy的端口, 不填时会使用proxy config中的监听端口, 不支持port range
    "version": "", // v2ray/xray server端版本, 默认为最新版
    "adaptive": { // 自适应配置
      "ports": [
        10000,
        "20000-21000"
      ], // 端口范围, 自动更换时会从该端口范围内随机选择一个
      "tags": [
        "vless"
      ], // 需要自动更换端口的inbound tag
      "cron": "0 5 * * *" // 自动更换端口任务的执行周期规则, 采用cron进行调度, 默认为"0 5 * * *"
    }
  },
  "server": {
    "http": {
      "port": 23155, // http服务监听端口
      "token": "iiiii", // 访问http服务时的token, 每个节点的token可以不同
      "support_prometheus": true
    },
    "listen": "0.0.0.0", // http与rpc服务监听地址
    "name": "end_node1", // 本地节点名称
    "rpc": {
      "only_gateway": false, // 为true时表示当前节点仅负责转发, 不负责proxy管理等工作
      "port": 23156, // 本地监听的rpc端口
      "type": "end" // 节点类型, center|end
    }
  },
  "cert": {
    "email": "test@gmail.com",
    "secrets": { // dns api访问tokens, 参见https://go-acme.github.io/lego/dns/
      "key": "key",
      "secret": "secert" 
    },
    "path": "/root/acme_test/", // cert存储路径
    "dns_provider": "alidns" // dns服务名称, 参见https://go-acme.github.io/lego/dns/
  },
  "users": {
    "user1": "passwd1|0" // 用户列表 key = {user name}, value = {passwrod}|{expire time}, expire time为过期时间的时间戳, 0时表示不过期,
  }
}
```