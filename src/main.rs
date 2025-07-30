use beelog::args;
use beelog::config;
use beelog::jump_server_helper;
use reedline::Signal;
use std::process::exit;

mod cli_line;

const QUIT : &str = "quit";

#[tokio::main]
async fn main() {
    let args = args::init();
    // 读取配置
    let server_res = config::read_server_config(&args);
    if let Err(err) = server_res {
        println!("读取配置异常: {}", err);
        exit(1);
    }
    let (server_info, node_group) = server_res.unwrap();
    let nodes = node_group.nodes;
    let mut helper = jump_server_helper::Helper::connect(server_info, nodes).await;

    let cli = cli_line::CliLine::new(&node_group.group);
    let mut line_editor = cli.line_editor;
    let prompt = cli.prompt;

    loop {
        let sig = line_editor.read_line(&prompt);
        match sig {
            Ok(Signal::Success(line)) => {
                let command = line.trim();
                if "".eq(command) {
                    continue;
                } else if QUIT.eq(command) {
                    break;
                } else if cli_line::is_command_blocked(command) {
                    println!("⚠️ 命令 `{}` 被禁止执行：可能导致会话阻塞", command);
                    continue;
                }
                helper.exec(command).await;
            }
            Ok(Signal::CtrlC) => {
                let _ = line_editor.clear_scrollback();
            }
            Ok(Signal::CtrlD) => {
                break;
            }
            x => {
                println!("Event: {:?}", x);
            }
        }
    }
    helper.close().await;
}