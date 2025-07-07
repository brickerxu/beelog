use std::io::{self, Write};
use std::process::exit;
use beelog::config;
use beelog::jump_server_bridge::JumpServerBridge;

fn main() {
    // 读取配置
    let server_res = config::read_server_config(&"".to_string(), &"".to_string());
    if let Err(err) = server_res {
        println!("读取配置异常: {}", err);
        exit(1);
    }
    let (server_info, nodes) = server_res.unwrap();
    
    // 建立连接
    let mut bridges = Vec::new();
    for node in nodes {
        let mut bridge = JumpServerBridge::new(&server_info, node.to_string());
        bridge.create_bridge().expect(&format!("{}连接失败", &node));
        bridges.push(bridge);
    }
    
    // 循环等待执行命令
    loop {
        print!("> ");
        // 保证提示符立即输出
        io::stdout().flush().unwrap();
    
        let mut input = String::new();
        match io::stdin().read_line(&mut input) {
            Ok(_) => {
                let command = input.trim();
                for b in &mut bridges {
                    let (node, output) = b.exec(command).unwrap();
                    println!("======{}=======", node);
                    println!("{}", output);
                }
            }
            Err(e) => {
                eprintln!("输入错误: {}", e);
                break;
            }
        }
    }
}
