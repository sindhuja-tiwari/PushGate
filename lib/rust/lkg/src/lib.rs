//! PushGate's consumer-side safety net for config files, the Rust twin of
//! `lib/go/lkg`. Same behavior, same status JSON.
//!
//! * **Last known good.** A file that fails to parse or breaks a declared
//!   limit is rejected and the process keeps serving the last file that
//!   loaded. Every good file is cached at `<path>.lkg`, so a restart with a
//!   bad file on disk starts from the cache instead of crash-looping.
//! * **Declared limits.** The consumer's parse function checks the limits
//!   its code depends on and returns an error instead of failing later.
//!   Unlike Go, a Rust panic cannot be caught here when the binary is built
//!   with `panic = "abort"`, so declared limits are the only guard: they must
//!   cover every limit the serving code relies on.
//! * **Fail open or closed, per file.** `KeepLastGood` keeps serving the old
//!   file. `FailClosed` stops serving until a good file arrives.
//! * **Randomized reload.** A changed file is read after a random delay, and
//!   read errors are retried with jittered exponential backoff.
//! * **Reports.** `status_json()` says what is served, from where, and the
//!   last rejection with its reason.

use serde::{Deserialize, Serialize};
use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, RwLock};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Mode {
    KeepLastGood,
    FailClosed,
}

impl Mode {
    pub fn parse(s: &str) -> Result<Mode, String> {
        match s.to_ascii_lowercase().as_str() {
            "keep_last_good" | "keep" | "open" | "fail_open" => Ok(Mode::KeepLastGood),
            "fail_closed" | "closed" => Ok(Mode::FailClosed),
            other => Err(format!("unknown mode {other:?} (want keep_last_good or fail_closed)")),
        }
    }
}

pub struct Options {
    pub path: PathBuf,
    pub mode: Mode,
    /// How often the file is checked for changes.
    pub poll: Duration,
    /// Most a changed file waits before it is read.
    pub jitter: Duration,
    pub backoff_base: Duration,
    pub backoff_max: Duration,
    /// Declared limits, published in the status.
    pub limits: serde_json::Map<String, serde_json::Value>,
    /// Seed for the jitter; 0 picks one from the clock and pid.
    pub seed: u64,
}

impl Options {
    pub fn new(path: impl Into<PathBuf>, mode: Mode) -> Options {
        let poll = Duration::from_millis(500);
        Options {
            path: path.into(),
            mode,
            poll,
            jitter: poll,
            backoff_base: Duration::from_millis(250),
            backoff_max: Duration::from_secs(30),
            limits: serde_json::Map::new(),
            seed: 0,
        }
    }
}

#[derive(Clone, Debug, Serialize)]
pub struct Rejection {
    pub config_version: String,
    pub reason: String,
    pub at: String,
}

#[derive(Clone, Debug, Serialize)]
pub struct Status {
    pub mode: Mode,
    pub serving: bool,
    pub config_version: String,
    /// "file", "cache" or "" when nothing is served.
    pub from: String,
    pub loads: u64,
    pub rejections: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub last_rejected: Option<Rejection>,
    #[serde(skip_serializing_if = "serde_json::Map::is_empty")]
    pub limits: serde_json::Map<String, serde_json::Value>,
}

type ParseFn<T> = Box<dyn Fn(&[u8]) -> Result<T, String> + Send + Sync>;

pub struct Loader<T> {
    opts: Options,
    parse: ParseFn<T>,
    state: RwLock<(Option<Arc<T>>, Status)>,
    rng: Mutex<u64>,
    opened: Option<Stamp>,
    log: Box<dyn Fn(&str) + Send + Sync>,
}

pub fn cache_path(path: &Path) -> PathBuf {
    let mut s = path.as_os_str().to_owned();
    s.push(".lkg");
    PathBuf::from(s)
}

type Stamp = (SystemTime, u64);

fn stat(p: &Path) -> Option<Stamp> {
    let m = fs::metadata(p).ok()?;
    Some((m.modified().ok()?, m.len()))
}

impl<T: Send + Sync + 'static> Loader<T> {
    /// Load the config file. Never fails because of the file's contents: a
    /// bad file is rejected and, in KeepLastGood mode, the cached last good
    /// file is served instead. With no good file, `current()` is None until
    /// one arrives.
    pub fn open<F>(opts: Options, parse: F, log: impl Fn(&str) + Send + Sync + 'static) -> Arc<Loader<T>>
    where
        F: Fn(&[u8]) -> Result<T, String> + Send + Sync + 'static,
    {
        let mut seed = opts.seed;
        if seed == 0 {
            let t = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos() as u64).unwrap_or(1);
            seed = t ^ ((std::process::id() as u64) << 32) | 1;
        }
        let status = Status {
            mode: opts.mode,
            serving: false,
            config_version: String::new(),
            from: String::new(),
            loads: 0,
            rejections: 0,
            last_rejected: None,
            limits: opts.limits.clone(),
        };
        let opened = stat(&opts.path);
        let l = Loader {
            opts,
            parse: Box::new(parse),
            state: RwLock::new((None, status)),
            rng: Mutex::new(seed),
            opened,
            log: Box::new(log),
        };
        match fs::read(&l.opts.path) {
            Ok(raw) => {
                if l.accept(&raw, "file") {
                    return Arc::new(l);
                }
            }
            Err(e) => l.reject(None, format!("read: {e}")),
        }
        if l.opts.mode == Mode::KeepLastGood {
            if let Ok(raw) = fs::read(cache_path(&l.opts.path)) {
                if let Ok(v) = (l.parse)(&raw) {
                    let cv = config_version(&raw);
                    let mut st = l.state.write().unwrap();
                    st.0 = Some(Arc::new(v));
                    st.1.serving = true;
                    st.1.from = "cache".into();
                    st.1.config_version = cv.clone();
                    st.1.loads += 1;
                    drop(st);
                    (l.log)(&format!("lkg: serving cached last known good {cv}"));
                }
            }
        }
        Arc::new(l)
    }

    /// The config to serve, or None when there is nothing to serve.
    pub fn current(&self) -> Option<Arc<T>> {
        let st = self.state.read().unwrap();
        if st.1.serving {
            st.0.clone()
        } else {
            None
        }
    }

    pub fn status(&self) -> Status {
        self.state.read().unwrap().1.clone()
    }

    pub fn status_json(&self) -> String {
        serde_json::to_string(&self.status()).unwrap_or_else(|_| "{}".into())
    }

    /// Poll the file and reload it on change, on a background thread.
    pub fn watch(self: &Arc<Self>) -> thread::JoinHandle<()> {
        let l = Arc::clone(self);
        thread::spawn(move || {
            let mut last = l.opened;
            loop {
                thread::sleep(l.opts.poll);
                let now = stat(&l.opts.path);
                if now.is_none() || now == last {
                    continue;
                }
                thread::sleep(l.rand_dur(l.opts.jitter));
                let mut attempt: u32 = 0;
                loop {
                    let seen = stat(&l.opts.path);
                    match fs::read(&l.opts.path) {
                        Ok(raw) => {
                            l.accept(&raw, "file");
                            last = seen;
                            break;
                        }
                        Err(e) => {
                            let mut d = l.opts.backoff_base.checked_mul(1 << attempt.min(20)).unwrap_or(l.opts.backoff_max);
                            if d > l.opts.backoff_max {
                                d = l.opts.backoff_max;
                            } else {
                                attempt += 1;
                            }
                            (l.log)(&format!("lkg: read failed, retrying: {e}"));
                            thread::sleep(l.rand_dur(d) + Duration::from_millis(1));
                        }
                    }
                }
            }
        })
    }

    fn rand_dur(&self, max: Duration) -> Duration {
        let n = max.as_nanos() as u64;
        if n == 0 {
            return Duration::ZERO;
        }
        let mut s = self.rng.lock().unwrap();
        // xorshift64
        *s ^= *s << 13;
        *s ^= *s >> 7;
        *s ^= *s << 17;
        Duration::from_nanos(*s % n)
    }

    fn accept(&self, raw: &[u8], from: &str) -> bool {
        match (self.parse)(raw) {
            Err(reason) => {
                self.reject(Some(raw), reason);
                false
            }
            Ok(v) => {
                let cv = config_version(raw);
                {
                    let mut st = self.state.write().unwrap();
                    st.0 = Some(Arc::new(v));
                    st.1.serving = true;
                    st.1.from = from.into();
                    st.1.config_version = cv.clone();
                    st.1.loads += 1;
                    st.1.last_rejected = None;
                }
                if let Err(e) = write_atomic(&cache_path(&self.opts.path), raw) {
                    (self.log)(&format!("lkg: caching last known good: {e}"));
                }
                (self.log)(&format!("lkg: loaded {cv}"));
                true
            }
        }
    }

    fn reject(&self, raw: Option<&[u8]>, reason: String) {
        let cv = raw.map(config_version).unwrap_or_else(|| "unknown".into());
        let serving;
        {
            let mut st = self.state.write().unwrap();
            st.1.rejections += 1;
            st.1.last_rejected = Some(Rejection { config_version: cv.clone(), reason: reason.clone(), at: rfc3339(SystemTime::now()) });
            if self.opts.mode == Mode::FailClosed {
                st.1.serving = false;
            }
            serving = if st.1.serving { st.1.config_version.clone() } else { String::new() };
        }
        if serving.is_empty() {
            (self.log)(&format!("lkg: rejected {cv} ({reason}); not serving"));
        } else {
            (self.log)(&format!("lkg: rejected {cv} ({reason}); still serving {serving}"));
        }
    }
}

fn config_version(raw: &[u8]) -> String {
    #[derive(Deserialize)]
    struct Head {
        #[serde(default)]
        config_version: String,
    }
    match serde_json::from_slice::<Head>(raw) {
        Ok(h) if !h.config_version.is_empty() => h.config_version,
        _ => "unknown".into(),
    }
}

fn write_atomic(path: &Path, data: &[u8]) -> std::io::Result<()> {
    let dir = path.parent().unwrap_or(Path::new("."));
    let tmp = dir.join(format!(".lkg-tmp-{}-{:?}", std::process::id(), thread::current().id()).replace(['(', ')'], ""));
    {
        let mut f = fs::File::create(&tmp)?;
        f.write_all(data)?;
    }
    fs::rename(&tmp, path)
}

/// UTC time as RFC 3339 with milliseconds, without a date crate.
fn rfc3339(t: SystemTime) -> String {
    let d = t.duration_since(UNIX_EPOCH).unwrap_or_default();
    let secs = d.as_secs() as i64;
    let (days, rem) = (secs.div_euclid(86400), secs.rem_euclid(86400));
    // Howard Hinnant's civil_from_days.
    let z = days + 719468;
    let era = z.div_euclid(146097);
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = if month <= 2 { y + 1 } else { y };
    format!(
        "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}.{:03}Z",
        rem / 3600,
        rem % 3600 / 60,
        rem % 60,
        d.subsec_millis()
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(raw: &[u8]) -> Result<Vec<i64>, String> {
        #[derive(Deserialize)]
        struct C {
            items: Vec<i64>,
        }
        let c: C = serde_json::from_slice(raw).map_err(|e| e.to_string())?;
        if c.items.len() > 3 {
            return Err(format!("{} items exceeds declared limit 3", c.items.len()));
        }
        Ok(c.items)
    }

    fn dir(name: &str) -> PathBuf {
        let d = std::env::temp_dir().join(format!("lkg-test-{}-{name}", std::process::id()));
        let _ = fs::remove_dir_all(&d);
        fs::create_dir_all(&d).unwrap();
        d.join("c.json")
    }

    fn write(p: &Path, body: &str) {
        fs::write(p, body).unwrap();
    }

    fn opts(p: &Path, mode: Mode) -> Options {
        let mut o = Options::new(p, mode);
        o.poll = Duration::from_millis(5);
        o.jitter = Duration::from_millis(5);
        o.backoff_base = Duration::from_millis(1);
        o.seed = 1;
        o
    }

    fn wait(what: &str, cond: impl Fn() -> bool) {
        for _ in 0..1000 {
            if cond() {
                return;
            }
            thread::sleep(Duration::from_millis(2));
        }
        panic!("timed out waiting for {what}");
    }

    const GOOD: &str = r#"{"config_version":"g1","items":[1,2]}"#;
    const GOOD2: &str = r#"{"config_version":"g2","items":[1,2,3]}"#;
    const BAD: &str = r#"{"config_version":"bad","items":[1,2,3,4]}"#;

    #[test]
    fn keeps_last_good_on_bad_reload() {
        let p = dir("keep");
        write(&p, GOOD);
        let l = Loader::open(opts(&p, Mode::KeepLastGood), parse, |_| {});
        assert_eq!(l.current().unwrap().len(), 2);
        assert_eq!(fs::read_to_string(cache_path(&p)).unwrap(), GOOD);
        l.watch();
        write(&p, BAD);
        wait("rejection", || l.status().rejections == 1);
        assert_eq!(l.current().unwrap().len(), 2);
        let r = l.status().last_rejected.unwrap();
        assert_eq!(r.config_version, "bad");
        assert!(r.reason.contains("declared limit"));
        write(&p, GOOD2);
        wait("good reload", || l.status().config_version == "g2");
        assert!(l.status().last_rejected.is_none());
    }

    #[test]
    fn restart_with_bad_file_uses_cache() {
        let p = dir("cache");
        write(&p, GOOD);
        drop(Loader::open(opts(&p, Mode::KeepLastGood), parse, |_| {}));
        write(&p, BAD);
        let l = Loader::open(opts(&p, Mode::KeepLastGood), parse, |_| {});
        assert_eq!(l.current().unwrap().len(), 2);
        assert_eq!(l.status().from, "cache");
        assert_eq!(l.status().last_rejected.unwrap().config_version, "bad");
    }

    #[test]
    fn fail_closed_stops_serving() {
        let p = dir("closed");
        write(&p, GOOD);
        let l = Loader::open(opts(&p, Mode::FailClosed), parse, |_| {});
        l.watch();
        write(&p, BAD);
        wait("stop serving", || l.current().is_none());
        write(&p, GOOD2);
        wait("serve again", || l.current().map(|v| v.len()) == Some(3));
        write(&p, BAD);
        let l2 = Loader::open(opts(&p, Mode::FailClosed), parse, |_| {});
        assert!(l2.current().is_none(), "fail closed must not serve the cache");
    }

    #[test]
    fn missing_file_serves_nothing() {
        let p = dir("missing");
        let l = Loader::open(opts(&p, Mode::KeepLastGood), parse, |_| {});
        assert!(l.current().is_none());
        assert!(l.status().last_rejected.unwrap().reason.starts_with("read:"));
    }

    #[test]
    fn status_json_matches_go_shape() {
        let p = dir("json");
        write(&p, GOOD);
        let l = Loader::open(opts(&p, Mode::KeepLastGood), parse, |_| {});
        let v: serde_json::Value = serde_json::from_str(&l.status_json()).unwrap();
        for k in ["mode", "serving", "config_version", "from", "loads", "rejections"] {
            assert!(v.get(k).is_some(), "missing {k}");
        }
        assert_eq!(v["mode"], "keep_last_good");
    }

    #[test]
    fn rfc3339_known_date() {
        let t = UNIX_EPOCH + Duration::from_millis(1_762_000_000_123);
        assert_eq!(rfc3339(t), "2025-11-01T12:26:40.123Z");
    }
}
