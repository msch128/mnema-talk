use crate::Error;
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use serde::de::{Deserialize, Deserializer, Visitor};
use std::fmt;
use zeroize::{Zeroize, Zeroizing};
pub(crate) struct Secret([u8; 32]);
impl Secret {
    pub(crate) fn parse(text: &str) -> Result<Self, Error> {
        if text.len() != 43
            || !text
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
        {
            return Err(Error::Protocol);
        }
        let mut bytes = Zeroizing::new([0u8; 32]);
        if URL_SAFE_NO_PAD
            .decode_slice(text, bytes.as_mut())
            .map_err(|_| Error::Protocol)?
            != 32
        {
            return Err(Error::Protocol);
        }
        let secret = Self(*bytes);
        if secret.wire().as_str() != text {
            return Err(Error::Protocol);
        }
        Ok(secret)
    }
    pub(crate) fn bytes(&self) -> [u8; 32] {
        self.0
    }
    pub(crate) fn wire(&self) -> Zeroizing<String> {
        Zeroizing::new(URL_SAFE_NO_PAD.encode(self.0))
    }
    pub(crate) fn same(&self, other: &Self) -> bool {
        self.0 == other.0
    }
}
impl Drop for Secret {
    fn drop(&mut self) {
        self.0.zeroize()
    }
}
impl fmt::Debug for Secret {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Secret(REDACTED)")
    }
}
impl<'de> Deserialize<'de> for Secret {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        struct SecretVisitor;
        impl<'de> Visitor<'de> for SecretVisitor {
            type Value = Secret;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("canonical native secret")
            }
            fn visit_str<E: serde::de::Error>(self, value: &str) -> Result<Secret, E> {
                Secret::parse(value).map_err(|_| E::custom("invalid native secret"))
            }
        }
        d.deserialize_str(SecretVisitor)
    }
}
pub struct Password(Zeroizing<String>);
impl Password {
    pub fn from_native_input(value: String) -> Result<Self, Error> {
        let value = Zeroizing::new(value);
        if value.is_empty() || value.len() > 72 {
            return Err(Error::InvalidInput);
        }
        Ok(Self(value))
    }
    pub(crate) fn text(&self) -> &str {
        self.0.as_str()
    }
}
impl fmt::Debug for Password {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Password(REDACTED)")
    }
}
