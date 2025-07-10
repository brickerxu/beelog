# beelog

## config.toml

> 程序自动读取 ~/.config/beelog/config.toml

```toml

[server]
default-server = "server-name"
default-node-group = "group-name"

[[server.servers]]
# 自定义名称
name = "server-name"
host = "x.x.x.x"
port = 1011
user = "xxx"
key_path = "xxx"
# 可选
secret_code = "MFA code"


[[server.node-groups]]
# 自定义名称
group = "group-name"
nodes = ["node1", "node2"]
```