use ssh2::{Channel, Session, KeyboardInteractivePrompt};
use std::net::{Ipv4Addr, SocketAddr, SocketAddrV4, TcpStream};
use std::path::Path;
use std::time::{Duration, Instant};
use std::io::{Write, Read};
use std::string::ToString;
use anyhow::{Result, Error, anyhow};
use crate::config::ServerInfo;
use crate::mfa;


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

/// ssh连接结构体
pub struct SshBridge {
    server_info: ServerInfo,
    channel: Channel,
    success: bool,
}

/// ssh连接实现
impl SshBridge {

    /// 建立连接
    pub fn create_bridge(server_info: ServerInfo) -> Result<Self, Error> {
        println!("===开始连接: {}", server_info.host);
        let host_split: Vec<u8> = server_info.host.split(".")
            .map(|e| {e.parse().expect(&format!("Host转换错误: {} - {}", server_info.host, e))})
            .collect();
        if host_split.len() != 4 {
            return Err(anyhow!("无效的 IP 地址"));
        }
        let socket = SocketAddrV4::new(Ipv4Addr::new(host_split[0], host_split[1], host_split[2], host_split[3]), server_info.port);
        let tcp = TcpStream::connect_timeout(&SocketAddr::V4(socket), Duration::from_secs(20)).map_err(|e| anyhow!(format!("连接失败: {}", e)))?;
        let mut sess = Session::new().map_err(|e| anyhow!(format!("创建 session 失败: {}", e)))?;
        sess.set_tcp_stream(tcp);
        sess.set_timeout(1000 * 10);
        sess.handshake().map_err(|e| anyhow!(format!("握手失败: {}", e)))?;

        let pri_key_path = Path::new(&server_info.key_path);
        let auth_pubkey_res = sess.userauth_pubkey_file(&server_info.user, None, pri_key_path, None);
        if let Err(e) = auth_pubkey_res {
            if let Some(secret_code) = &server_info.secret_code {
                let mut prompt = MfaKeyboardPrompt::new(secret_code);
                let auth_keyboard_res = sess.userauth_keyboard_interactive(&server_info.user, &mut prompt);
                if let Err(e) = auth_keyboard_res {
                    return Err(anyhow!(format!("二次认证失败: {}", e)));
                }
            } else {
                return Err(anyhow!(format!("证书认证失败: {}", e)));
            }
        } 

        if !sess.authenticated() {
            return Err(anyhow!("认证失败"));
        }

        let mut channel = sess.channel_session().map_err(|e| anyhow!(format!("创建 channel 失败: {}", e)))?;
        channel.request_pty("xterm", None, None).map_err(|e| anyhow!(format!("PTY 请求失败: {}", e)))?;
        // 开启 shell 模式
        channel.shell().map_err(|e| anyhow!(format!("打开 shell 失败: {}", e)))?;

        let (m, prompt, _) = Self::wait_for_prompt(&mut channel, vec!(JUMP_SERVER_MARK.to_string()), 10)?;
        if m {
            if prompt != JUMP_SERVER_MARK  {
                return Err(anyhow!("未能正确连接"));
            }
        } else {
            return Err(anyhow!("未能正确连接"));
        }

        // // 输入节点 IP 或主机名
        // Self::send_line(&mut channel, node.as_str())?;
        // 
        // // 等待登录目标主机
        // let _ = Self::wait_for_prompt(&mut channel, vec!(PROMPT_MARK.to_string()), 10)?;
        // 读取时不会阻塞
        // sess.set_blocking(false);
        println!("===连接成功: {}", server_info.host);
        Ok(SshBridge {
            server_info,
            channel,
            success: true,
        })
    }

    /// 命令执行
    pub fn exec(&mut self, command: &str, prompts: Vec<String>) -> Result<String, Error> {
        // println!("开始执行: {}", self.node);
        Self::send_line(&mut self.channel, command)?;
        // 等待登录目标主机
        let (_, _, output) = Self::wait_for_prompt(&mut self.channel, prompts, 60 * 20)?;
        // println!("结束执行: {}", self.node);
        Ok(output)
    }

    /// 关闭连接
    pub fn close(&mut self) -> Result<(), Error> {
        let server = &mut self.server_info;
        let channel = &mut self.channel;
        channel.send_eof()?;
        channel.wait_eof()?;
        channel.close()?;
        channel.wait_close()?;
        println!("===断开连接: {}", server.host);
        Ok(())
    }

    /**
     * 等待输出
     */
    fn wait_for_prompt(channel: &mut Channel, prompts: Vec<String>, timeout_secs: u64) -> Result<(bool, String, String), Error> {
        let deadline = Instant::now() + Duration::from_secs(timeout_secs);
        let mut prompt_match = false;
        let mut match_prompt = String::new();
        let mut content = String::new();

        'out_loop: while Instant::now() < deadline {
            let mut buffer = [0u8; 1024];
            match channel.read(&mut buffer) {
                Ok(n) => {
                    let current_content = String::from_utf8_lossy(&buffer[..n]);
                    content.push_str(current_content.to_string().as_str());
                    for prompt in prompts.iter() {
                        if current_content.contains(prompt) {
                            prompt_match = true;
                            match_prompt = prompt.clone();
                            break 'out_loop;
                        }
                    }
                }
                Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(300));
                    continue
                },
                Err(e) => return Err(anyhow!(e)),
            }
        }
        Ok((prompt_match, match_prompt, content.to_string()))
    }

    fn send_line(channel: &mut Channel, input: &str) -> Result<(), Error> {
        channel.write_all(format!("{}\r", input).as_bytes())
            .map_err(|e| anyhow!(format!("写入失败: {}", e)))?;
        channel.flush().map_err(|e| anyhow!(format!("flush失败: {}", e)))
    }
    
    pub fn is_ok(&self) -> bool {
        self.success
    }
}