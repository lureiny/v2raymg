# V2raymg

基于v2ray提供的api对v2ray进行管理, 支持单机和集群模式

## 功能列表

### 集群模式

- 支持通过中心节点发现其他节点
- 支持不通过中心节点, 通过级联的方式感知全部节点
- 集群中任一节点都可以作为入口节点管理集群内任意节点
- 集群内节点自感知其他节点状态, 可以自动剔除无效节点, 无效是指无法连接 

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
## 配置文件说明

```json
{
  "cluster": {
    "center_node": {     // 中心节点相关配置
      "host": "localhost",  
      "port": 0          // 为0时不会使用中心节点
    },
    "token": "",         // 集群内节点间验证用的token, 可以为空, 但不建议设置为空
    "name": "test",      // 集群名字, 相同集群的节点需要具有相同集群名称
    "nodes": [            // 集群内其他节点信息, 不使用中心节点时可以使用此种方式搭建集群, 只要集群中不存在孤岛节点, 集群内的节点即可全部互相感知
      {
        "name": "node1",  // 节点名称, 不可以重名
        "port": 10000,
        "host": "127.0.0.1"
      }
    ]
  },
  "proxy": {             
    "config_file": "/usr/local/etc/xray/config.json",             // xray/v2ray配置文件路径
    "default_tags": ["vless"],                                    // 默认操作的inbound  tag, 为空时会在全部外部inbound上操作
    "host": "127.0.0.1",                                          // 本地的ip/host, 生成订阅时需要用到
    "port": 443,                                                  // proxy的端口, 不填时会使用proxy config中的监听端口, 不支持port range
    "exec": "",                                                   // 可执行文件路径, 填写后v2raymg会接管进程的运行, 同时可以实现升级管理
    "adaptive": {                                                 // 自适应配置
      "ports": [10000,"20000-21000"],                             // 端口范围, 自动更换时会从该端口范围内随机选择一个
      "tags": ["vless"],                                          // 需要自动更换端口的inbound tag
      "cron": "* 5 * * *"                                         // 自动更换端口任务的执行周期规则, 采用cron进行调度, 默认为"* 5 * * *"
    }
  },
  "server": {                                                     
    "http": {                                                     
      "port": 23155,                                              // http服务监听端口
      "token": "iiiii"                                            // 访问http服务时的token, 每个节点的token可以不同
    },
    "listen": "0.0.0.0",                                          // http与rpc服务监听地址
    "name": "end_node1",                                          // 本地节点名称
    "rpc": { 
      "only_gateway": false,                                      // 为true时表示当前节点仅负责转发, 不负责proxy管理等工作
      "port": 23156,                                              // 本地监听的rpc端口
      "type": "end"                                               // 节点类型, center|end
    }
  },
  "users": {
    "user1": "passwd1|0"                                          // 用户列表 key = {user name}, value = {passwrod}|{expire time}, expire time为过期时间的时间戳, 0时表示不过期,
  }
}
```