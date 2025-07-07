use std::io::{Error, ErrorKind};
use std::fs;
use std::path::PathBuf;
use serde::Deserialize;


#[derive(Debug, Deserialize)]
pub struct Config {
    pub server: ServerConfig,
}

#[derive(Debug, Deserialize)]
pub struct ServerConfig {
    #[serde(rename = "default-server")]
    pub default_server: String,

    #[serde(rename = "default-node-group")]
    pub default_node_group: String,

    pub servers: Vec<ServerInfo>,

    #[serde(rename = "node-groups")]
    pub node_groups: Vec<NodeGroup>,
}

#[derive(Debug, Deserialize)]
#[derive(PartialEq)]
pub struct ServerInfo {
    pub name: String,
    pub host: String,
    pub port: u16,
    pub user: String,
    pub key_path: String,
    #[serde(default)]
    pub secret_code: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct NodeGroup {
    pub group: String,
    pub nodes: Vec<String>,
}


/**
 * 读取服务器信息
 *
 * server 服务器名称，如果为空则使用配置文件中的默认服务器
 * node_group 节点组名称，如果为空则使用配置文件中的默认节点
 *
 * return 返回服务器信息和节点组列表
 *
 * Error 如果未找到服务器或节点组配置，则返回错误
 */
pub fn read_server_config(server: &String, node_group: &String) -> Result<(ServerInfo, Vec<String>), Box<dyn std::error::Error>> {
    let config = load_config()?;
    let server_config = config.server;
    let mut default_server = server.clone();
    if server.is_empty() {
        default_server = server_config.default_server;
    }
    let mut server_info_opt = None;
    for info in server_config.servers {
        if default_server.eq(&info.name) {
            server_info_opt = Some(info);
            break;
        }
    }
    if None.eq(&server_info_opt) {
        return Err(Error::new(ErrorKind::NotFound, format!("未找到server配置: {}", default_server)).into())
    }
    let mut default_node_group = node_group.clone();
    if node_group.is_empty() {
        default_node_group = server_config.default_node_group;
    }
    let mut node_groups_opt = None;
    for group in server_config.node_groups {
        if default_node_group.eq(&group.group) {
            node_groups_opt = Some(group.nodes);
        }
    }
    if None.eq(&node_groups_opt) {
        return Err(Error::new(ErrorKind::NotFound, format!("未找到node group配置: {}", default_node_group)).into())
    }

    Ok((server_info_opt.unwrap(), node_groups_opt.unwrap()))
}

/**
 * 加载配置文件
 * 读取用户主目录下的 .config/<package_name>/config.toml 文件
 * 如果文件不存在则返回错误
 * 如果文件存在则解析为 Config 结构体
 */
fn load_config() -> Result<Config, Box<dyn std::error::Error>>{
    let home_dir = dirs::home_dir().unwrap();
    let package_name = env!("CARGO_PKG_NAME"); // 读取 Cargo.toml 中的 package.name
    let config_path = home_dir.join(".config").join(package_name).join("config.toml");
    let config_path = PathBuf::from("config.toml");
    if !config_path.exists() {
        return Err(Error::new(ErrorKind::NotFound, format!("配置文件未找到 {}", config_path.display())).into());
    }
    let content = fs::read_to_string(config_path)?;
    let config: Config = toml::from_str(&content)?;
    Ok(config)
}
