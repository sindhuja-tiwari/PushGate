//! bot-proxy: the Rust test consumer for PushBench.
//!
//! It loads a "feature file" (the bot-scoring model's inputs) and serves
//! `GET /score`. Two failure shapes from public postmortems are built in on
//! purpose, and both are naive in the same way the originals were:
//!
//! * Cloudflare, Nov 2025: the feature table is preallocated with a fixed
//!   capacity. Loading a file with more features returns an error that the
//!   loader `unwrap()`s, so the process panics.
//! * CrowdStrike, Jul 2024: every rule is a template of match fields read
//!   against a fixed-size array of request inputs. A rule whose field index
//!   is past the end of that array panics, but only when a request actually
//!   reaches that field (wildcards are skipped).
//!
//! Week 4 adds the last-known-good library. Until then this consumer has no
//! safety net, which is what baselines B0 to B2 measure.

use serde::Deserialize;
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, RwLock};
use std::thread;
use std::time::{Duration, SystemTime};
use tiny_http::{Header, Method, Response, Server};

mod version;
use version::{BUILD_VERSION, INPUT_SLOTS, MAX_FEATURES};

#[derive(Debug, Deserialize)]
struct FeatureFile {
    schema: String,
    config_version: String,
    features: Vec<FeatureSpec>,
}

#[derive(Debug, Deserialize, Clone)]
struct FeatureSpec {
    /// Not used for scoring. Kept so later reports can name a feature.
    #[allow(dead_code)]
    name: String,
    weight: f64,
    /// Match template. `"*"` is a wildcard. Field `i` is compared with request
    /// input slot `i`.
    fields: Vec<String>,
}

#[derive(Debug)]
enum LoadError {
    Io(String),
    Parse(String),
    WrongSchema(String),
    TooManyFeatures { got: usize, max: usize },
}

impl std::fmt::Display for LoadError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            LoadError::Io(e) => write!(f, "io error: {e}"),
            LoadError::Parse(e) => write!(f, "parse error: {e}"),
            LoadError::WrongSchema(s) => write!(f, "unexpected schema {s:?}"),
            LoadError::TooManyFeatures { got, max } => {
                write!(f, "feature count {got} exceeds preallocated limit {max}")
            }
        }
    }
}

/// Preallocated feature table. Capacity is fixed at build time, like the
/// Cloudflare module that reserved memory for 200 features.
struct FeatureTable {
    config_version: String,
    slots: Vec<FeatureSpec>,
}

impl FeatureTable {
    fn with_capacity() -> Self {
        FeatureTable { config_version: String::from("none"), slots: Vec::with_capacity(MAX_FEATURES) }
    }

    fn load(path: &PathBuf) -> Result<FeatureTable, LoadError> {
        let raw = fs::read_to_string(path).map_err(|e| LoadError::Io(e.to_string()))?;
        let file: FeatureFile = serde_json::from_str(&raw).map_err(|e| LoadError::Parse(e.to_string()))?;
        if file.schema != "bot-features/1" {
            return Err(LoadError::WrongSchema(file.schema));
        }
        if file.features.len() > MAX_FEATURES {
            return Err(LoadError::TooManyFeatures { got: file.features.len(), max: MAX_FEATURES });
        }
        let mut table = FeatureTable::with_capacity();
        table.config_version = file.config_version;
        table.slots.extend(file.features);
        Ok(table)
    }

    /// Score one request. `inputs` has exactly INPUT_SLOTS entries.
    fn score(&self, inputs: &[String; INPUT_SLOTS]) -> (f64, usize) {
        let mut sum = 0.0;
        let mut matched = 0;
        for f in &self.slots {
            let mut all = true;
            for (i, want) in f.fields.iter().enumerate() {
                if want == "*" {
                    continue;
                }
                // Deliberately unchecked: a template with more fields than
                // INPUT_SLOTS indexes past the end and panics here.
                if inputs[i] != *want {
                    all = false;
                    break;
                }
            }
            if all {
                sum += f.weight;
                matched += 1;
            }
        }
        (1.0 / (1.0 + (-sum).exp()), matched)
    }
}

struct Stats {
    requests: AtomicU64,
    loads: AtomicU64,
    started: SystemTime,
}

struct Args {
    id: String,
    port: u16,
    config: PathBuf,
    poll_ms: u64,
    threads: usize,
}

fn parse_args() -> Args {
    let mut a = Args { id: "local".into(), port: 8080, config: PathBuf::from("feature.json"), poll_ms: 500, threads: 2 };
    let mut it = std::env::args().skip(1);
    while let Some(k) = it.next() {
        let mut v = || it.next().unwrap_or_else(|| die(&format!("missing value for {k}")));
        match k.as_str() {
            "--id" => a.id = v(),
            "--port" => a.port = v().parse().unwrap_or_else(|_| die("bad --port")),
            "--config" => a.config = PathBuf::from(v()),
            "--poll-ms" => a.poll_ms = v().parse().unwrap_or_else(|_| die("bad --poll-ms")),
            "--threads" => a.threads = v().parse().unwrap_or_else(|_| die("bad --threads")),
            "--version" => {
                println!("bot-proxy v{BUILD_VERSION} max_features={MAX_FEATURES} input_slots={INPUT_SLOTS}");
                std::process::exit(0);
            }
            other => die(&format!("unknown flag {other}")),
        }
    }
    a
}

fn die(msg: &str) -> ! {
    eprintln!("bot-proxy: {msg}");
    std::process::exit(2);
}

fn mtime(p: &PathBuf) -> Option<SystemTime> {
    fs::metadata(p).and_then(|m| m.modified()).ok()
}

fn parse_inputs(query: &str) -> [String; INPUT_SLOTS] {
    let mut inputs: [String; INPUT_SLOTS] = std::array::from_fn(|_| String::new());
    for pair in query.split('&') {
        let mut kv = pair.splitn(2, '=');
        let (k, v) = (kv.next().unwrap_or(""), kv.next().unwrap_or(""));
        if let Some(idx) = k.strip_prefix('f').and_then(|n| n.parse::<usize>().ok()) {
            if idx < INPUT_SLOTS {
                inputs[idx] = v.to_string();
            }
        }
    }
    inputs
}

fn main() {
    let args = parse_args();

    // Initial load. Same naive unwrap as the reload path: a bad file on disk
    // means the process cannot start, which is how a crash loop forms.
    let table = FeatureTable::load(&args.config)
        .unwrap_or_else(|e| panic!("bot-proxy v{BUILD_VERSION} {}: failed to load {:?}: {e}", args.id, args.config));
    eprintln!(
        "bot-proxy v{BUILD_VERSION} {} loaded {} features (config {})",
        args.id,
        table.slots.len(),
        table.config_version
    );
    let table = Arc::new(RwLock::new(table));
    let stats = Arc::new(Stats { requests: AtomicU64::new(0), loads: AtomicU64::new(1), started: SystemTime::now() });

    // Reload loop: poll the file's mtime and swap the table on change.
    {
        let table = Arc::clone(&table);
        let stats = Arc::clone(&stats);
        let path = args.config.clone();
        let id = args.id.clone();
        let poll = Duration::from_millis(args.poll_ms);
        thread::spawn(move || {
            let mut last = mtime(&path);
            loop {
                thread::sleep(poll);
                let now = mtime(&path);
                if now.is_some() && now != last {
                    last = now;
                    // The Cloudflare bug: the error is unwrapped instead of
                    // handled, so a bad file takes the process down.
                    let fresh = FeatureTable::load(&path)
                        .unwrap_or_else(|e| panic!("bot-proxy v{BUILD_VERSION} {id}: reload failed: {e}"));
                    eprintln!("bot-proxy v{BUILD_VERSION} {id} reloaded {} features (config {})", fresh.slots.len(), fresh.config_version);
                    *table.write().unwrap() = fresh;
                    stats.loads.fetch_add(1, Ordering::Relaxed);
                }
            }
        });
    }

    let server = Arc::new(
        Server::http(("127.0.0.1", args.port)).unwrap_or_else(|e| die(&format!("cannot bind port {}: {e}", args.port))),
    );
    let json = Header::from_bytes(&b"Content-Type"[..], &b"application/json"[..]).unwrap();
    let text = Header::from_bytes(&b"Content-Type"[..], &b"text/plain; version=0.0.4"[..]).unwrap();

    let mut workers = Vec::new();
    for _ in 0..args.threads.max(1) {
        let server = Arc::clone(&server);
        let table = Arc::clone(&table);
        let stats = Arc::clone(&stats);
        let id = args.id.clone();
        let (json, text) = (json.clone(), text.clone());
        workers.push(thread::spawn(move || loop {
            let req = match server.recv() {
                Ok(r) => r,
                Err(_) => continue,
            };
            let url = req.url().to_string();
            let (path, query) = url.split_once('?').unwrap_or((url.as_str(), ""));
            let resp = match (req.method(), path) {
                (Method::Get, "/score") => {
                    stats.requests.fetch_add(1, Ordering::Relaxed);
                    let inputs = parse_inputs(query);
                    let t = table.read().unwrap();
                    let (score, matched) = t.score(&inputs);
                    let body = format!(
                        r#"{{"score":{score:.4},"bot":{},"matched":{matched},"features":{},"config_version":"{}","version":{BUILD_VERSION}}}"#,
                        score > 0.5,
                        t.slots.len(),
                        t.config_version
                    );
                    Response::from_string(body).with_header(json.clone())
                }
                (Method::Get, "/healthz") => {
                    let t = table.read().unwrap();
                    Response::from_string(format!(
                        r#"{{"ok":true,"id":"{id}","version":{BUILD_VERSION},"config_version":"{}","features":{}}}"#,
                        t.config_version,
                        t.slots.len()
                    ))
                    .with_header(json.clone())
                }
                (Method::Get, "/metrics") => {
                    let t = table.read().unwrap();
                    let up = stats.started.elapsed().map(|d| d.as_secs_f64()).unwrap_or(0.0);
                    let body = format!(
                        "# TYPE pg_requests_total counter\npg_requests_total{{consumer=\"bot-proxy\",id=\"{id}\",version=\"{v}\"}} {r}\n\
                         # TYPE pg_config_loads_total counter\npg_config_loads_total{{consumer=\"bot-proxy\",id=\"{id}\",version=\"{v}\"}} {l}\n\
                         # TYPE pg_features_loaded gauge\npg_features_loaded{{consumer=\"bot-proxy\",id=\"{id}\",version=\"{v}\"}} {n}\n\
                         # TYPE pg_feature_capacity gauge\npg_feature_capacity{{consumer=\"bot-proxy\",id=\"{id}\",version=\"{v}\"}} {MAX_FEATURES}\n\
                         # TYPE pg_config_info gauge\npg_config_info{{consumer=\"bot-proxy\",id=\"{id}\",version=\"{v}\",config_version=\"{cv}\"}} 1\n\
                         # TYPE pg_uptime_seconds gauge\npg_uptime_seconds{{consumer=\"bot-proxy\",id=\"{id}\",version=\"{v}\"}} {up:.1}\n",
                        v = BUILD_VERSION,
                        r = stats.requests.load(Ordering::Relaxed),
                        l = stats.loads.load(Ordering::Relaxed),
                        n = t.slots.len(),
                        cv = t.config_version,
                    );
                    Response::from_string(body).with_header(text.clone())
                }
                _ => Response::from_string("not found").with_status_code(404),
            };
            let _ = req.respond(resp);
        }));
    }
    for w in workers {
        let _ = w.join();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    fn write_file(name: &str, n: usize, arity: usize) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("pg-test-{}-{name}", std::process::id()));
        fs::create_dir_all(&dir).unwrap();
        let p = dir.join("f.json");
        let feats: Vec<String> = (0..n)
            .map(|i| {
                let fields: Vec<String> = (0..arity).map(|j| if j == 0 { format!("\"ua{i}\"") } else { "\"*\"".into() }).collect();
                format!(r#"{{"name":"f{i}","weight":1.0,"fields":[{}]}}"#, fields.join(","))
            })
            .collect();
        let mut f = fs::File::create(&p).unwrap();
        write!(f, r#"{{"schema":"bot-features/1","config_version":"t","features":[{}]}}"#, feats.join(",")).unwrap();
        p
    }

    #[test]
    fn loads_up_to_capacity() {
        let t = FeatureTable::load(&write_file("cap", MAX_FEATURES, INPUT_SLOTS)).unwrap();
        assert_eq!(t.slots.len(), MAX_FEATURES);
    }

    #[test]
    fn rejects_over_capacity() {
        match FeatureTable::load(&write_file("over", MAX_FEATURES + 1, INPUT_SLOTS)) {
            Err(LoadError::TooManyFeatures { got, max }) => assert_eq!((got, max), (MAX_FEATURES + 1, MAX_FEATURES)),
            other => panic!("expected TooManyFeatures, got {:?}", other.map(|t| t.slots.len())),
        }
    }

    #[test]
    fn scores_matching_rule() {
        let t = FeatureTable::load(&write_file("score", 3, INPUT_SLOTS)).unwrap();
        let mut inputs: [String; INPUT_SLOTS] = std::array::from_fn(|_| String::new());
        inputs[0] = "ua1".into();
        let (s, m) = t.score(&inputs);
        assert_eq!(m, 1);
        assert!(s > 0.5);
    }
}
