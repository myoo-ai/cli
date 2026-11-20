//! Fixture holding Rust code that code_lint accepts. The crate docs may
//! span several lines in lib.rs and main.rs, and shorter ones elsewhere.
#![allow(unused, reason = "the fixture defines unused helpers")]

use std::fmt::Write as _;
use std::mem;

/// Reads the configured value, falling back to `def` when the variable is
/// unset or not valid unicode.
pub fn lookup(id: &str, def: &str) -> String {
    std::env::var(id).unwrap_or_else(|_| def.to_string())
}

fn empty_checks(v: &[u32], s: &str) -> bool {
    v.is_empty() || !s.is_empty() || v.len() == 1
}

fn early(x: u32) -> u32 {
    if x > 3 {
        return x;
    }
    x + 1
}

fn patterns(s: &str, set: &std::collections::HashSet<&str>) -> bool {
    s.split(',').count() > 1 || s.contains("ab") || set.contains("x") || s.starts_with('a')
}

fn loops(v: Vec<u32>, m: std::collections::HashMap<u32, u32>) -> u32 {
    let mut n = 0;
    for x in &v {
        n += x;
    }
    for x in v.iter().rev() {
        n += x;
    }
    for (k, _) in &m {
        n += k;
    }
    n
}

fn mapping(o: Option<u32>) -> u32 {
    o.map_or(0, |x| x + 1) + o.unwrap_or(7)
}

fn defaults(o: Option<Vec<u32>>) -> Vec<u32> {
    o.unwrap_or_default()
}

fn question(r: Result<u32, std::io::Error>) -> Result<u32, std::io::Error> {
    let v = match r {
        Ok(v) => v,
        Err(e) => {
            eprintln!("failed: {e}");
            return Err(e);
        }
    };
    Ok(r? + v)
}

fn formats(name: &str, n: f64) -> String {
    let mut out = String::new();
    // Writing to a String cannot fail.
    let _ = write!(out, "{name}: {:>8.2} {{literal}}", n);
    println!();
    println!("{:>5}", n.to_string());
    format!("{n}")
}

fn unsafe_code(p: *const u32) -> u32 {
    // SAFETY: callers pass a pointer to a live u32.
    unsafe { *p }
}

/// Reads through a raw pointer.
///
/// # Safety
///
/// `p` must point to a live, aligned `u32`.
pub unsafe fn raw_read(p: *const u32) -> u32 {
    // SAFETY: the caller upholds the contract above.
    unsafe { *p }
}

fn take(v: &mut Vec<u32>) -> Vec<u32> {
    mem::take(v)
}

struct Point {
    x: u32,
    y: u32,
}

fn point(x: u32, y: u32) -> Point {
    Point { x, y: y + 1 }
}

fn chained(a: bool, b: bool) -> u32 {
    if a {
        1
    } else if b {
        2
    } else {
        3
    }
}

fn arms(o: Option<u32>) -> u32 {
    match o {
        Some(x) if x > 2 => x,
        _ => 0,
    }
}

fn closures(v: Vec<u32>) -> Vec<String> {
    v.iter().map(|x| x.to_string()).collect()
}

fn mutex(m: &std::sync::Mutex<u32>) -> Result<u32, String> {
    let g = m.lock().unwrap();
    Ok(*g)
}

fn ignore_send(tx: &std::sync::mpsc::Sender<u32>) {
    // The receiver may be gone during shutdown, which is fine here.
    let _ = tx.send(1);
}

fn literals() -> (char, &'static str, &'static [u8]) {
    let raw = r#"a "quoted" {} string"#;
    ('\'', raw, b"bytes")
}

fn parse_port(s: &str) -> anyhow::Result<u16> {
    let n: u16 = s.parse().expect("ports are validated by the CLI parser");
    if n == 0 {
        anyhow::bail!("port must be 1-65535, got {n}");
    }
    assert!(n > 0);
    assert_eq!(n, 1);
    Ok(n)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads() -> Result<(), String> {
        let v = "1".parse::<u32>().unwrap();
        dbg!(v);
        Ok(())
    }

    #[test]
    fn fails() {
        let e: Result<(), String> = Err("failed".into());
        assert!(e.is_err(), "Oops! expected an error");
    }
}
