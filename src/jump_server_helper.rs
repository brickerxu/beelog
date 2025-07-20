use std::collections::HashMap;
use std::process::exit;
use std::sync::{Arc, Mutex};
use crate::config::ServerInfo;
use crate::ssh_bridge::*;

pub struct Helper {
    ssh_bridges: Vec<Arc<Mutex<SshBridge>>>,
}

impl Helper {
    pub async fn connect(server_info: ServerInfo, nodes: Vec<String>) -> Self {
        let mut handles = Vec::new();
        for node in nodes {
            let server_info_clone = server_info.clone();
            let handle = tokio::task::spawn_blocking(move || {
                let result = SshBridge::create_bridge(server_info_clone, node.clone());
                (node, result)
            });
            handles.push(handle);
        }
        let results = futures::future::try_join_all(handles).await.unwrap();
        let mut bridges = Vec::new();
        let mut errors = HashMap::new();
        for (node, result) in results {
            match result {
                Ok(bridge) => {
                    bridges.push(Arc::new(Mutex::new(bridge)));
                }
                Err(e) => {
                    errors.insert(node, e);
                }
            }
        }
        let mut helper = Self {
            ssh_bridges: bridges,
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
        for b in &self.ssh_bridges {
            let b = Arc::clone(b);
            let command = command.to_string();
            let task = tokio::task::spawn_blocking(move || {
                let mut bridge = b.lock().unwrap();
                bridge.exec(&command)
            });
            tasks.push(task);
        }

        let results = futures::future::join_all(tasks).await;

        for result in results {
            match result {
                Ok(Ok((node, output))) => {
                    // println!("======{}=======", node);
                    println!("{}", output);
                }
                Ok(Err(e)) => {
                    println!("执行命令错误: {}", e);
                }
                Err(e) => {
                    println!("任务失败: {}", e);
                }
            }
        }
    }
    
    pub fn close(&mut self) {
        for b in &self.ssh_bridges {
            let mut bridge = b.lock().unwrap();
            if bridge.is_ok() {
                let res = bridge.close();
                if let Err(err) = res {
                    println!("{} > 关闭失败: {}", bridge.node, err);
                }
            }
        }
    }
}