use base32::Alphabet::Rfc4648;
use base32::decode;
use hmac::{Hmac, Mac};
use sha1::Sha1;
use std::time::{SystemTime, UNIX_EPOCH};

type HmacSha1 = Hmac<Sha1>;

pub fn get_google_code(secret: &str) -> String {
    format!("{:06}", get_totp_token(secret, 3))
}

fn get_hotp_token(secret: &str, intervals_no: u64) -> u32 {
    // Decode base32 secret
    let key = decode(Rfc4648 { padding: false }, secret).expect("Invalid base32 secret");

    // Convert interval to 8-byte array (big-endian)
    let msg = intervals_no.to_be_bytes();

    // HMAC-SHA1
    let mut mac = HmacSha1::new_from_slice(&key).expect("HMAC can take key of any size");
    mac.update(&msg);
    let hmac_result = mac.finalize().into_bytes();

    // Dynamic truncation
    let offset = (hmac_result[19] & 0x0f) as usize;
    let four_bytes = &hmac_result[offset..offset + 4];
    let code = ((u32::from_be_bytes(four_bytes.try_into().unwrap())) & 0x7fffffff) % 1_000_000;

    code
}

fn get_totp_token(secret: &str, bias: i64) -> u32 {
    let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs() as i64;
    let time_step = (now + bias) / 30;
    get_hotp_token(secret, time_step as u64)
}



#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_get_hotp_token() {
        let secret = "xxxx"; // 替换为 Google MFA 的 base32 秘钥
        let code = get_google_code(secret);
        println!("{}", code);
    }

}