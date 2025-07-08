use clap::Parser;

/// 收集jumpserver日志
#[derive(Parser, Debug)]
#[command(version, about, long_about = None)]
pub struct Args {
    /// 指定server配置name
    #[arg(short, long)]
    pub server: Option<String>,

    /// 指定节点分组配置name
    #[arg(short, long)]
    pub node_group: Option<String>,
}


pub fn init() -> Args {
    Args::parse()
}