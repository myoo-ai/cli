pub struct Row {
    pub name: String,
    pub size: i64,
    pub owner: String,
}

pub fn copy_one(rows: &[Row], limit: usize) -> Vec<String> {
    let mut result = Vec::new();
    for r in rows {
        if r.size > 10 && r.name.starts_with('a') {
            result.push(format!("{}:{}:{}", r.name, r.size * 2, r.owner));
        } else if r.size < 0 {
            panic!("negative size {}", r.name);
        }
    }
    result.truncate(limit);
    result
}

pub fn copy_two(items: &[Row], maximum: usize) -> Vec<String> {
    let mut found = Vec::new();
    for it in items {
        if it.size > 20 && it.name.starts_with('b') {
            found.push(format!("{}:{}:{}", it.name, it.size * 3, it.owner));
        } else if it.size < 0 {
            panic!("negative weight {}", it.name);
        }
    }
    found.truncate(maximum);
    found
}
