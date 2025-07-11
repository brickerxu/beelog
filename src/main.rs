use std::io::{self, Write};
use std::process::exit;
use beelog::config;
use beelog::args;
use beelog::jump_server_bridge::JumpServerBridge;

const QUIT : &str = "quit";

fn main() {
    let args = args::init();
    // 读取配置
    let server_res = config::read_server_config(&args);
    if let Err(err) = server_res {
        println!("读取配置异常: {}", err);
        exit(1);
    }
    let (server_info, nodes) = server_res.unwrap();
    
    let mut bridges = Vec::new();
    for node in nodes {
        let mut bridge = JumpServerBridge::new(&server_info, node.to_string());
        bridge.create_bridge().expect(&format!("{}>连接失败", &node));
        bridges.push(bridge);
    }
    loop {
        print!(">> ");
        io::stdout().flush().unwrap(); // 保证提示符立即输出
    
        let mut input = String::new();
        match io::stdin().read_line(&mut input) {
            Ok(_) => {
                let command = input.trim();
                if "".eq(command) {
                    continue;
                } else if QUIT.eq(command) {
                    break;
                }
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
    close_bridges(&mut bridges);
}

fn close_bridges(bridges: &mut Vec<JumpServerBridge>) {
    for b in bridges {
        if b.is_ok() {
            let res = b.close();
            if let Err(err) = res {
                println!("关闭失败[{}]: {}", b.node, err);
            }
        }
    }
}
