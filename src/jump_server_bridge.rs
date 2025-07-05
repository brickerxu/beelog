use ssh2::{Channel, Session};
use std::net::TcpStream;
use std::path::Path;
use std::time::{Duration, Instant};
use std::io::{Write, Read};

pub struct JumpServer {
    host: String,
    port: u16,
    user: String,
    key_path: String,
}

impl JumpServer {
    pub fn new(host: String, port: u16, user: String, key_path: String) -> Self {
        JumpServer {
            host, port, user, key_path 
        }
    }
}

pub struct JumpServerBridge<'a> {
    pub jump_server: &'a JumpServer,
    pub node: String,
    pub channel: Option<Channel>,
}

impl<'a> JumpServerBridge<'a> {
    pub fn new(jump_server: &'a JumpServer, node: String) -> Self {
        JumpServerBridge {
            jump_server, node,
            channel: None
        }
    }

    pub fn create_bridge(&mut self) -> Result<(), String> {
        let server = self.jump_server;
        let addr = format!("{}:{}", server.host, server.port);
        let tcp = TcpStream::connect(&addr).map_err(|e| format!("连接失败: {}", e))?;
        tcp.set_read_timeout(Some(Duration::from_secs(3))).unwrap();
        let mut sess = Session::new().map_err(|e| format!("创建 session 失败: {}", e))?;
        sess.set_tcp_stream(tcp);
        sess.set_timeout(3000);
        sess.handshake().map_err(|e| format!("握手失败: {}", e))?;

        let key_path = Path::new(&server.key_path);
        sess.userauth_pubkey_file(&server.user, None, key_path, None)
            .map_err(|e| format!("认证失败: {}", e))?;

        if !sess.authenticated() {
            return Err("认证失败".to_string());
        }

        let mut channel = sess.channel_session().map_err(|e| format!("创建 channel 失败: {}", e))?;
        channel.request_pty("xterm", None, None).map_err(|e| format!("PTY 请求失败: {}", e))?;
        channel.shell().map_err(|e| format!("打开 shell 失败: {}", e))?; // 👈 开启 shell 模式
        
        // 等待 JumpServer 菜单出现
        Self::wait_for_prompt(&mut channel, "Opt>", 10)?;
        // 输入节点 IP 或主机名
        Self::send_line(&mut channel, &self.node)?;

        // 等待登录目标主机
        let output = Self::wait_for_prompt(&mut channel, "$", 10)?;
        self.channel = Some(channel);
        Ok(())
    }

    pub fn exec(&mut self, command: &str) -> Result<(String, String), String> {
        if let Some(channel) = self.channel.as_mut() {
            Self::send_line(channel, command)?;
            // 等待登录目标主机
            let output = Self::wait_for_prompt(channel, "$", 10)?;

            Ok((self.node.clone(), output))
        } else {
            Err("未建立 SSH 通道".to_string())
        }
    }
    
    pub fn close(&self) -> Result<(), String> {
        
        Ok(())
    }

    
    fn verify_mfa(channel: &mut Channel, sercet_code: &String) -> Result<(), String> {
        Self::wait_for_prompt(channel, "OTP Code", 10)?;
        
        Ok(())
    }

    /**
    等待输出
    */
    fn wait_for_prompt(channel: &mut Channel, prompt: &str, timeout_secs: u64) -> Result<String, String> {
        let deadline = Instant::now() + Duration::from_secs(timeout_secs);
        let mut buffer = Vec::new();
        let mut temp = [0u8; 1024];

        while Instant::now() < deadline {
            match channel.read(&mut temp) {
                Ok(n) => {
                    buffer.extend_from_slice(&temp[..n]);
                    let content = String::from_utf8_lossy(&buffer);
                    // println!("content: {}", content);
                    if content.contains(prompt) {
                        return Ok(content.to_string());
                    }
                    // println!("+++++++++++++++++++++++++++++++++++++++++++++");
                }
                Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock => continue,
                Err(e) => return Ok("fail".to_string()),
            }
        }
        Ok("".to_string())
    }

    fn send_line(channel: &mut Channel, input: &str) -> Result<(), String> {
        channel.write_all(format!("{}\r\n", input).as_bytes())
            .map_err(|e| format!("写入失败: {}", e))?;
        channel.flush().map_err(|e| format!("flush失败: {}", e))
    }
}