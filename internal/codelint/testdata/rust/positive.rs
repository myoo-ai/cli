// positive.rs - examples of AI patterns.
// Usage: run code_lint on this directory.

extern crate serde;

use std::collections::HashMap;
use std::fmt::Write;
use std::path::PathBuf;

// ----------------------------------------

/// GetUser is a function that gets the user.
pub fn get_user(id: &str) -> String {
    // Step 1: Read the environment variable
    let v = std::env::var(id).unwrap_or_default();
    // Here we return the value to the caller
    v
}

fn steps() {
    // 1. Parse the input into its parts
    let parts: Vec<&str> = "a b".split(" ").collect();
    // 2. Print all of the parts to stdout
    println!("{:?}", parts);
}

fn todo_body() {
    // TODO
}

fn not_done() -> u32 {
    todo!()
}

fn not_implemented() -> u32 {
    panic!("not implemented")
}

fn report() {
    println!("🚀 Starting");
    println!("done ✓");
    println!("one — two");
    println!("{}", "-".repeat(40));
    println!("==========");
    let s = format!("plain text");
    println!("");
    let _ = s;
}

fn swallow(path: &str) {
    let _ = std::fs::remove_file(path);
    std::fs::remove_dir(path).ok();
    if let Err(_) = std::fs::create_dir(path) {}
    match std::fs::read(path) {
        Ok(b) => println!("{}", b.len()),
        Err(_) => {}
    }
}

#[allow(dead_code)]
fn unused() {}

#![allow(clippy::all)]

fn len_checks(v: &Vec<u32>, name: &String, b: &Box<u32>, p: &PathBuf) -> bool {
    if v.len() == 0 {
        return false;
    }
    let flag = name.len() > 0;
    if flag == true {
        return 0 != v.len();
    }
    **b > 0 && p.exists()
}

fn returns(x: u32) -> u32 {
    let y = x + 1;
    return y;
}

fn strings() -> (String, String, String) {
    let a = "".to_string();
    let b = String::from("");
    let c = "".to_owned();
    (a, b, c)
}

fn iters(v: Vec<u32>, mut w: Vec<u32>, s: &str) -> Option<u32> {
    let first = v.iter().nth(0);
    for x in v.iter() {
        let _y = x;
    }
    for x in w.iter_mut() {
        *x += 1;
    }
    let has = s.contains("x") || s.starts_with("a");
    let parts = s.split(",").count();
    let n = first.map(|x| x + 1).unwrap_or(0);
    let starts = s.chars().next() == Some('a');
    let _ = (has, parts, starts);
    for x in w.into_iter() {
        let _z = x;
    }
    Some(n)
}

fn defaults(o: Option<Vec<u32>>, p: Option<String>, m: &mut HashMap<u32, Vec<u32>>) -> usize {
    let a = o.unwrap_or(Vec::new());
    let b = p.unwrap_or_else(String::new);
    m.entry(1).or_insert(Vec::new()).push(2);
    a.len() + b.len()
}

fn question(r: Result<u32, std::io::Error>, o: Option<u32>) -> Result<u32, std::io::Error> {
    let v = match r {
        Ok(v) => v,
        Err(e) => return Err(e.into()),
    };
    if o.is_none() {
        return Ok(v);
    }
    Ok(v)
}

fn maybe(o: Option<u32>) -> Option<u32> {
    if o.is_none() {
        return None;
    }
    let x = match o {
        Some(x) => x,
        None => return None,
    };
    Some(x)
}

fn formats(name: String, n: u32) -> String {
    let mut out = String::new();
    out += &format!("{}", n);
    out.push_str(&format!("{name}"));
    let _ = write!(out, "{}", n.to_string());
    println!("{} {:?}", name.to_string(), n);
    dbg!(n);
    out
}

fn unsafe_code(p: *const u32) -> u32 {
    unsafe { *p }
}

struct Wrapper(u32);

unsafe impl Send for Wrapper {}

pub unsafe fn raw_read(p: *const u32) -> u32 {
    // SAFETY: the caller guarantees that p is valid.
    unsafe { *p }
}

fn read_config(path: &str) -> Result<String, std::io::Error> {
    let text = std::fs::read_to_string(path).unwrap();
    Ok(text)
}

fn take(v: &mut Vec<u32>, o: &mut Option<u32>) -> (Vec<u32>, Option<u32>) {
    let a = std::mem::replace(v, Vec::new());
    let a2 = std::mem::replace(&mut *v, Default::default());
    let b = std::mem::replace(&mut *o, None);
    let _ = a2;
    (a, b)
}

struct Point {
    x: u32,
    y: u32,
}

fn point(x: u32, y: u32) -> Point {
    Point { x: x, y: y }
}

fn nested(a: bool, b: bool) -> u32 {
    if a {
        1
    } else {
        if b {
            2
        } else {
            3
        }
    }
}

fn single(o: Option<u32>) {
    match o {
        Some(x) => println!("{}", x),
        _ => {}
    }
}

fn closures(v: Vec<u32>) -> Vec<String> {
    v.iter().map(|x| describe(x)).collect()
}

fn describe(x: &u32) -> String {
    x.to_string()
}

fn deep(v: &[u32]) -> u32 {
    let mut n = 0;
    for a in v {
        if *a > 1 {
            while n < 10 {
                match a {
                    2 => {
                        if n > 3 {
                            n += 1;
                        }
                    }
                    _ => n += 2,
                }
            }
        }
    }
    n
}

fn commented() -> u32 {
    let x = 1;
    let mut counter = x;
    // Increment the counter variable here
    counter += 1;
    counter
}

fn long_function(x: u32) -> u32 {
    let mut total = 0;
    total += 0 * x;
    total += 1 * x;
    total += 2 * x;
    total += 3 * x;
    total += 4 * x;
    total += 5 * x;
    total += 6 * x;
    total += 7 * x;
    total += 8 * x;
    total += 9 * x;
    total += 10 * x;
    total += 11 * x;
    total += 12 * x;
    total += 13 * x;
    total += 14 * x;
    total += 15 * x;
    total += 16 * x;
    total += 17 * x;
    total += 18 * x;
    total += 19 * x;
    total += 20 * x;
    total += 21 * x;
    total += 22 * x;
    total += 23 * x;
    total += 24 * x;
    total += 25 * x;
    total += 26 * x;
    total += 27 * x;
    total += 28 * x;
    total += 29 * x;
    total += 30 * x;
    total += 31 * x;
    total += 32 * x;
    total += 33 * x;
    total += 34 * x;
    total += 35 * x;
    total += 36 * x;
    total += 37 * x;
    total += 38 * x;
    total += 39 * x;
    total += 40 * x;
    total += 41 * x;
    total += 42 * x;
    total += 43 * x;
    total += 44 * x;
    total += 45 * x;
    total += 46 * x;
    total += 47 * x;
    total += 48 * x;
    total += 49 * x;
    total += 50 * x;
    total += 51 * x;
    total += 52 * x;
    total += 53 * x;
    total += 54 * x;
    total += 55 * x;
    total += 56 * x;
    total += 57 * x;
    total += 58 * x;
    total += 59 * x;
    total += 60 * x;
    total += 61 * x;
    total += 62 * x;
    total += 63 * x;
    total += 64 * x;
    total += 65 * x;
    total += 66 * x;
    total += 67 * x;
    total += 68 * x;
    total += 69 * x;
    total += 70 * x;
    total += 71 * x;
    total += 72 * x;
    total += 73 * x;
    total += 74 * x;
    total += 75 * x;
    total += 76 * x;
    total += 77 * x;
    total += 78 * x;
    total += 79 * x;
    total += 80 * x;
    total += 81 * x;
    total += 82 * x;
    total += 83 * x;
    total += 84 * x;
    total
}

fn chatty(x: u32) -> u32 {
    // Start with the input value.
    let a = x;
    // Double the value.
    let b = a * 2;
    // Add one to the value.
    let c = b + 1;
    // Square the value.
    let d = c * c;
    // Subtract the input.
    let e = d - x;
    // Return the final result.
    e
}

fn parse_port(s: &str) -> anyhow::Result<u16> {
    // CHANGED: the parser no longer calls trim.
    let n: u16 = s.parse().expect("");
    let m: u16 = s.parse().expect("  ");
    if n == 0 {
        anyhow::bail!("Invalid input");
    }
    if m == 0 {
        return Err(anyhow!("unknown error"));
    }
    assert!(true);
    assert_eq!(2, 2);
    Ok(n)
}

fn parse_flag(s: &str) -> Result<bool, String> {
    if s.is_empty() {
        return Err("Operation failed.".to_string());
    }
    Ok(s == "1")
}
