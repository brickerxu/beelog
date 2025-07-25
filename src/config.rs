use serde::Deserialize;
use std::fs;
use std::io::{Error, ErrorKind};
use std::path::PathBuf;
use super::args::Args;

const CONFIG_FILE_NAME: &str = "config.toml";
const HISTORY_FILE_NAME: &str = "history.txt";

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

#[derive(Debug, Deserialize, Clone)]
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

#[derive(Debug, Deserialize, Clone)]
pub struct NodeGroup {
    pub group: String,
    pub nodes: Vec<String>,
}


/**
 * 读取服务器信息
 *
 * args 传参
 *      server 服务器名称，如果为空则使用配置文件中的默认服务器
 *      node_group 节点组名称，如果为空则使用配置文件中的默认节点
 *
 * return 返回服务器信息和节点组列表
 *
 * Error 如果未找到服务器或节点组配置，则返回错误
 */
pub fn read_server_config(args: &Args) -> Result<(ServerInfo, NodeGroup), Box<dyn std::error::Error>> {
    let config = load_config()?;
    let server_config = config.server;
    let arg_server = args.server.as_ref().unwrap_or(&server_config.default_server);

    let mut server_info_opt = None;
    for info in server_config.servers {
        if arg_server.eq(&info.name) {
            server_info_opt = Some(info);
            break;
        }
    }
    if None == server_info_opt {
        return Err(Error::new(ErrorKind::NotFound, format!("未找到server配置: {}", arg_server)).into())
    }
    let arg_node_group = args.node_group.as_ref().unwrap_or(&server_config.default_node_group);
    let mut node_group_opt = None;
    for group in server_config.node_groups {
        if arg_node_group.eq(&group.group) {
            node_group_opt = Some(group);
        }
    }
    if node_group_opt.is_none() {
        return Err(Error::new(ErrorKind::NotFound, format!("未找到node group配置: {}", arg_node_group)).into())
    }

    Ok((server_info_opt.unwrap(), node_group_opt.unwrap()))
}

/**
 * 获取命令历史存储路径
 */
pub fn get_history_path() -> PathBuf {
    if cfg!(debug_assertions) {
        PathBuf::from(HISTORY_FILE_NAME)
    } else {
        let config_dir = get_config_dir();
        config_dir.join(HISTORY_FILE_NAME)
    }
}

/**
 * 加载配置文件
 * 读取用户主目录下的 .config/<package_name>/config.toml 文件
 * 如果文件不存在则返回错误
 * 如果文件存在则解析为 Config 结构体
 */
fn load_config() -> Result<Config, Box<dyn std::error::Error>>{
    let config_file_path = if cfg!(debug_assertions) {
        PathBuf::from(CONFIG_FILE_NAME)
    } else {
        let config_dir = get_config_dir();
        config_dir.join(CONFIG_FILE_NAME)
    };
    if !config_file_path.exists() {
        return Err(Error::new(ErrorKind::NotFound, format!("配置文件未找到 {}", config_file_path.display())).into());
    }
    let content = fs::read_to_string(config_file_path)?;
    let config: Config = toml::from_str(&content)?;
    Ok(config)
}

/**
 * 获取配置目录
 */
fn get_config_dir() -> PathBuf {
    let home_dir = dirs::home_dir().unwrap();
    // 读取 Cargo.toml 中的 package.name
    let package_name = env!("CARGO_PKG_NAME");
    home_dir.join(".config").join(package_name)
}