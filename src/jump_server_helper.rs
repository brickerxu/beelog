use std::collections::HashMap;
use std::process::exit;
use std::sync::{Arc, Mutex};
use crate::config::ServerInfo;
use crate::ssh_bridge::*;


const JUMP_SERVER_MARK : &str = "Opt>";

pub struct Helper {
    jump_server_bridges: Vec<JumpServerBridge>,
}

struct JumpServerBridge {
    ssh_bridge: Arc<Mutex<SshBridge>>,
    node: String,
}

impl Helper {
    pub async fn connect(server_info: ServerInfo, nodes: Vec<String>) -> Self {
        let mut handles = Vec::new();
        for node in nodes {
            let server_info_clone = server_info.clone();
            let handle = tokio::task::spawn_blocking(move || {
                let mut result = SshBridge::create_bridge(server_info_clone, JUMP_SERVER_MARK);
                if let Ok(ref mut ssh_bridge) = result {
                    let _ = ssh_bridge.exec(node.as_str(), vec![node.clone()]);
                }
                (node, result)
            });
            handles.push(handle);
        }
        let results = futures::future::try_join_all(handles).await.unwrap();
        let mut jump_server_bridges = Vec::new();
        let mut errors = HashMap::new();
        for (node, result) in results {
            match result {
                Ok(ssh_bridge) => {
                    let jump_server = JumpServerBridge {
                        ssh_bridge: Arc::new(Mutex::new(ssh_bridge)),
                        node,
                    };
                    jump_server_bridges.push(jump_server);
                }
                Err(e) => {
                    errors.insert(node, e);
                }
            }
        }
        let mut helper = Self {
            jump_server_bridges,
        };
        if !errors.is_empty() {
            for (node, error) in errors {
                println!("{} > 连接失败: {}", node, error);
            }
            // 断开已连接的资源
            helper.close();
            exit(1);
        }
        helper
    }

    pub async fn exec(&mut self, command: &str) {
        let mut tasks = Vec::new();
        for jsb in &self.jump_server_bridges {
            let ssh_bridge = Arc::clone(&jsb.ssh_bridge);
            let node = jsb.node.clone();
            let command = command.to_string();
            let task = tokio::task::spawn_blocking(move || {
                let mut bridge = ssh_bridge.lock().unwrap();
                bridge.exec(&command, vec![node])
            });
            tasks.push(task);
        }

        let results = futures::future::try_join_all(tasks).await.unwrap();

        for result in results {
            match result {
                Ok(output) => {
                    // println!("======{}=======", node);
                    println!("{}", output);
                }
                Err(e) => {
                    println!("执行命令错误: {}", e);
                }
            }
        }
    }

    pub fn close(&mut self) {
        for jsb in &self.jump_server_bridges {
            let mut bridge = jsb.ssh_bridge.lock().unwrap();
            println!("===关闭连接: {}", jsb.node);
            let res = bridge.close();
            if let Err(err) = res {
                println!("{} > 关闭失败: {}", jsb.node, err);
            }   
        }
    }
}