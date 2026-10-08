//! Validated native fixture options; no persistence, renderer key capture or OS
//! shortcut registration is performed here. Binding sampler remains bounded.
use crate::gaming_fixture::Error;
use crate::native_input::{
    Config,
    shortcuts::{Binding, Modifiers},
};

pub fn fixture_config<'a>(args: impl IntoIterator<Item = &'a str>) -> Result<Config, Error> {
    let mut config = Config {
        overlay: binding("ALT+M")?,
        ptt: Some(binding("ALT+V")?),
    };
    let mut overlay_seen = false;
    let mut ptt_seen = false;
    for arg in args {
        if let Some(value) = arg.strip_prefix("--overlay-key=") {
            if overlay_seen {
                return Err(Error::Owner);
            }
            overlay_seen = true;
            config.overlay = binding(value)?;
        }
        if let Some(value) = arg.strip_prefix("--ptt-key=") {
            if ptt_seen {
                return Err(Error::Owner);
            }
            ptt_seen = true;
            config.ptt = if value.eq_ignore_ascii_case("NONE") {
                None
            } else {
                Some(binding(value)?)
            };
        }
    }
    config.bindings().map_err(|_| Error::Owner)?;
    Ok(config)
}
fn binding(value: &str) -> Result<Binding, Error> {
    if value.is_empty() || value.len() > 32 || !value.is_ascii() {
        return Err(Error::Owner);
    }
    let upper = value.to_ascii_uppercase();
    let mut modifiers = Modifiers::default();
    let mut key = None;
    for part in upper.split('+') {
        let slot = match part {
            "ALT" => Some(&mut modifiers.alt),
            "CTRL" => Some(&mut modifiers.control),
            "SHIFT" => Some(&mut modifiers.shift),
            "WIN" => Some(&mut modifiers.windows),
            _ => None,
        };
        if let Some(slot) = slot {
            if *slot {
                return Err(Error::Owner);
            }
            *slot = true;
            continue;
        }
        if key.is_some() {
            return Err(Error::Owner);
        }
        key = Some(match part {
            "SPACE" => 0x20,
            "MOUSE4" => 0x05,
            "MOUSE5" => 0x06,
            letter if letter.len() == 1 && letter.as_bytes()[0].is_ascii_alphanumeric() => {
                u16::from(letter.as_bytes()[0])
            }
            function if function.starts_with('F') => {
                let number = function[1..].parse::<u16>().map_err(|_| Error::Owner)?;
                if !(1..=24).contains(&number) || (8..=11).contains(&number) {
                    return Err(Error::Owner);
                }
                0x6f + number
            }
            _ => return Err(Error::Owner),
        });
    }
    Binding::new(key.ok_or(Error::Owner)?, modifiers).map_err(|_| Error::Owner)
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn configurable_mouse_ptt_and_disabled_ptt() {
        let config = fixture_config(["--overlay-key=CTRL+SHIFT+X", "--ptt-key=MOUSE4"]).unwrap();
        assert_eq!(config.overlay.key(), b'X' as u16);
        assert!(config.overlay.modifiers().control && config.overlay.modifiers().shift);
        assert_eq!(config.ptt.unwrap().key(), 5);
        assert!(fixture_config(["--ptt-key=NONE"]).unwrap().ptt.is_none());
    }
    #[test]
    fn reserved_conflicting_and_ambiguous_bindings_reject() {
        for args in [
            vec!["--overlay-key=ALT+F4"],
            vec!["--overlay-key=WIN+X"],
            vec!["--overlay-key=ALT+ALT+M"],
            vec!["--overlay-key=F8"],
            vec!["--overlay-key=X+Y"],
            vec!["--overlay-key=ALT+M", "--ptt-key=ALT+M"],
            vec!["--overlay-key=M", "--overlay-key=X"],
        ] {
            assert!(fixture_config(args).is_err());
        }
    }
}
