use std::io::{self, Write};
use beelog::jump_server_bridge::{JumpServer, JumpServerBridge};

fn main() {
    
    let host = "192.168.3.88".to_string();
    let port = 2233;
    let user = "xubo".to_string();
    let private_key = "/Users/xubo/resource/xy_key/xubo-jumpserver-test.pem".to_string();
    let jump_server = JumpServer::new(host, port, user, private_key);

    let nodes = vec!["dex-04", "dex-04"];
    let mut bridges = Vec::new();
    for node in nodes {
        let mut bridge = JumpServerBridge::new(&jump_server, node.to_string());
        bridge.create_bridge().expect(&format!("{}连接失败", &node));
        bridges.push(bridge);
    }
    loop {
        print!("> ");
        io::stdout().flush().unwrap(); // 保证提示符立即输出

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
                eprintln!("❌ 输入错误: {}", e);
                break;
            }
        }
    }
}
