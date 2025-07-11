use ssh2::{Channel, Session, KeyboardInteractivePrompt};
use std::net::{Ipv4Addr, SocketAddr, SocketAddrV4, TcpStream};
use std::path::Path;
use std::time::{Duration, Instant};
use std::io::{Write, Read};
use std::string::ToString;
use super::config::ServerInfo;
use super::mfa;


const JUMP_SERVER_MARK : &str = "Opt>";
const MFA_MARK : &str = "OTP Code";
const PROMPT_MARK : &str = "$";

/// MFA交互结构
struct MfaKeyboardPrompt {
    /// MFA code
    secret_code: String,
}

impl MfaKeyboardPrompt {
    fn new (secret_code: &str) -> Self {
        MfaKeyboardPrompt {
            secret_code: secret_code.to_string(),
        }
    }
}

/// MFA交互实现
impl KeyboardInteractivePrompt for MfaKeyboardPrompt {
    fn prompt(
        &mut self,
        _username: &str,
        _instructions: &str,
        prompts: &[ssh2::Prompt<'_>]
    ) -> Vec<String> {
        let mut responses = Vec::new();
        for prompt in prompts {
            if prompt.text.contains(MFA_MARK) {
                let mfa_code = mfa::get_google_code(&self.secret_code);
                responses.push(mfa_code);
            } else {
                println!("未知的认证方式：{}", prompt.text);
            }
        }
        responses
    }
}

/// jumpserver连接结构体
pub struct JumpServerBridge<'a> {
    pub jump_server: &'a ServerInfo,
    pub node: String,
    pub channel: Option<Channel>,
}

/// jumpserver连接实现
impl<'a> JumpServerBridge<'a> {
    pub fn new(jump_server: &'a ServerInfo, node: String) -> Self {
        JumpServerBridge {
            jump_server, node,
            channel: None
        }
    }

    /// 建立连接
    pub fn create_bridge(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let server = self.jump_server;
        println!("===开始连接: {} -> {}", server.host, self.node);
        let host_split: Vec<u8> = server.host.split(".")
            .map(|e| {e.parse().expect(&format!("Host转换错误: {} - {}", server.host, e))})
            .collect();
        if host_split.len() != 4 {
            return Err(Box::new(std::io::Error::new(std::io::ErrorKind::InvalidInput, "无效的 IP 地址")));
        }
        let socket = SocketAddrV4::new(Ipv4Addr::new(host_split[0], host_split[1], host_split[2], host_split[3]), server.port);
        let tcp = TcpStream::connect_timeout(&SocketAddr::V4(socket), Duration::from_secs(10)).map_err(|e| format!("连接失败: {}", e))?;
        let mut sess = Session::new().map_err(|e| format!("创建 session 失败: {}", e))?;
        sess.set_tcp_stream(tcp);
        sess.set_timeout(1000 * 60 * 5);
        sess.handshake().map_err(|e| format!("握手失败: {}", e))?;

        let pri_key_path = Path::new(&server.key_path);
        let auth_pubkey_res = sess.userauth_pubkey_file(&server.user, None, pri_key_path, None);
        if let Err(e) = auth_pubkey_res {
            if let Some(secret_code) = &server.secret_code {
                let mut prompt = MfaKeyboardPrompt::new(secret_code);
                let auth_keyboard_res = sess.userauth_keyboard_interactive(&server.user, &mut prompt);
                if let Err(e) = auth_keyboard_res {
                    return Err(Box::new(std::io::Error::new(std::io::ErrorKind::InvalidInput, format!("二次认证失败: {}", e))));
                }
            } else {
                return Err(Box::new(std::io::Error::new(std::io::ErrorKind::InvalidInput, format!("证书认证失败: {}", e))));
            }
        } 

        if !sess.authenticated() {
            return Err(Box::new(std::io::Error::new(std::io::ErrorKind::Other, "认证失败")));
        }

        let mut channel = sess.channel_session().map_err(|e| format!("创建 channel 失败: {}", e))?;
        channel.request_pty("xterm", None, None).map_err(|e| format!("PTY 请求失败: {}", e))?;
        channel.shell().map_err(|e| format!("打开 shell 失败: {}", e))?; // 👈 开启 shell 模式

        let (m, prompt, _) = Self::wait_for_prompt(&mut channel, vec!(JUMP_SERVER_MARK.to_string()), 10)?;
        if m {
            if prompt != JUMP_SERVER_MARK  {
                return Err(Box::new(std::io::Error::new(std::io::ErrorKind::Other, "未能正确连接")));
            }
        } else {
            return Err(Box::new(std::io::Error::new(std::io::ErrorKind::Other, "未能正确连接")));
        }

        // 输入节点 IP 或主机名
        Self::send_line(&mut channel, &self.node)?;

        // 等待登录目标主机
        let _ = Self::wait_for_prompt(&mut channel, vec!(PROMPT_MARK.to_string()), 10)?;
        self.channel = Some(channel);
        println!("===连接成功: {} -> {}", server.host, self.node);
        Ok(())
    }

    /// 命令执行
    pub fn exec(&mut self, command: &str) -> Result<(String, String), Box<dyn std::error::Error>> {
        if let Some(channel) = self.channel.as_mut() {
            Self::send_line(channel, command)?;
            // 等待登录目标主机
            let (_, _, output) = Self::wait_for_prompt(channel, vec!(PROMPT_MARK.to_string()), 10)?;
            Ok((self.node.clone(), output))
        } else {
            Err(Box::new(std::io::Error::new(std::io::ErrorKind::Other, "未建立 SSH 通道")))
        }
    }

    /// 关闭连接
    pub fn close(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let server = self.jump_server;
        let channel = self.channel.as_mut();
        channel.unwrap().send_eof()?;
        println!("===断开连接: {} -> {}", server.host, self.node);
        Ok(())
    }

    /**
     * 等待输出
     */
    fn wait_for_prompt(channel: &mut Channel, prompts: Vec<String>, timeout_secs: u64) -> Result<(bool, String, String), Box<dyn std::error::Error>> {
        let deadline = Instant::now() + Duration::from_secs(timeout_secs);
        let mut buffer = Vec::new();
        let mut temp = [0u8; 1024];

        while Instant::now() < deadline {
            match channel.read(&mut temp) {
                Ok(n) => {
                    buffer.extend_from_slice(&temp[..n]);
                    let content = String::from_utf8_lossy(&buffer);
                    for prompt in prompts.iter() {
                        if content.contains(prompt) {
                            return Ok((true, prompt.clone(), content.to_string()));
                        }
                    }
                }
                Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock => continue,
                Err(e) => return Err(Box::new(e)),
            }
        }
        Ok((false, String::from(""), String::from("")))
    }

    fn send_line(channel: &mut Channel, input: &str) -> Result<(), String> {
        channel.write_all(format!("{}\r\n", input).as_bytes())
            .map_err(|e| format!("写入失败: {}", e))?;
        channel.flush().map_err(|e| format!("flush失败: {}", e))
    }
}