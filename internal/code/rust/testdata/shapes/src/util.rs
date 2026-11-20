use super::geo::point::origin;

pub(crate) fn helper(v: f64) -> f64 {
    let p = origin();
    let bytes = b"bytes";
    let c = c"cstr";
    let raw = br##"x"#y"##;
    let ch = '\u{1F600}';
    let lifetime_like: &'static str = "s";
    v + p.x + bytes.len() as f64 + c.count_bytes() as f64 + raw.len() as f64 + ch.len_utf8() as f64 + lifetime_like.len() as f64 + 1e-3_f64
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn helper_adds() {
        assert_eq!(helper(1.0) > 0.0, true);
    }
}
