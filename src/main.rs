use beelog::args;
use beelog::config;
use beelog::jump_server_bridge::JumpServerBridge;
use rustyline::error::ReadlineError;
use rustyline::history::DefaultHistory;
use rustyline::{DefaultEditor, Editor, Result};
use std::collections::HashMap;
use std::process::exit;

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
    let (server_info, nodes) = server_res.unwrap();
    
    let mut handles = Vec::new();
    for node in nodes {
        let server_info_clone = server_info.clone();
        let handle = tokio::task::spawn_blocking(move || {
            let result = JumpServerBridge::create_bridge(server_info_clone, node.clone());
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
                bridges.push(bridge);
            }
            Err(e) => {
                errors.insert(node, e);
            }
        }
    }
    
    if !errors.is_empty() {
        for (node, error) in errors {
            println!("{} > 连接失败: {}", node, error);
        }
        // 断开已连接的资源
        close_bridges(&mut bridges);
        exit(1);
    }
    
    let mut editor = get_editor().unwrap();
    loop {
        let readline = editor.readline(">> ");
        match readline {
            Ok(line) => {
                let command = line.trim();
                if "".eq(command) {
                    continue;
                } else if QUIT.eq(command) {
                    break;
                }
                // 将命令加入历史
                add_editor_history(&mut editor, command);
                for b in &mut bridges {
                    let res = b.exec(command);
                    match res {
                        Ok((node, output)) => {
                            println!("======{}=======", node);
                            println!("{}", output);
                        },
                        Err(e) => {
                            println!("{} > 执行命令错误: {}", &b.node, e);
                        }
                    }
                }
            },
            Err(ReadlineError::Interrupted) => {
                println!("CTRL-C");
                break
            },
            Err(ReadlineError::Eof) => {
                println!("CTRL-D");
                break
            },
            Err(err) => {
                println!("输入错误: {:?}", err);
                break
            }
        }
    }
    close_bridges(&mut bridges);
    save_editor_history(&mut editor);
}

fn close_bridges(bridges: &mut Vec<JumpServerBridge>) {
    for b in bridges {
        if b.is_ok() {
            let res = b.close();
            if let Err(err) = res {
                println!("{} > 关闭失败: {}", b.node, err);
            }
        }
    }
}

fn get_editor() -> Result<Editor<(), DefaultHistory>> {
    let mut editor = DefaultEditor::new().expect("新建输入组件异常");
    let history_path = config::get_history_path();
    if history_path.exists() {
        if editor.load_history(history_path.as_os_str()).is_err() {
            println!("加载历史记录异常");
        }
    }
    Ok(editor)
}

fn add_editor_history(editor: &mut Editor<(), DefaultHistory>, command: &str) {
    let result = editor.add_history_entry(command);
    if let Err(_) = result {
        
    }
}

fn save_editor_history(editor: &mut Editor<(), DefaultHistory>) {
    let history_path = config::get_history_path();
    if !history_path.parent().unwrap().exists() {
        std::fs::create_dir_all(&history_path.parent().unwrap()).unwrap();
    }
    let result = editor.save_history(history_path.as_os_str());
    if let Err(err) = result {
        println!("保存历史失败[{}]: {}", history_path.display(), err);
    }
}